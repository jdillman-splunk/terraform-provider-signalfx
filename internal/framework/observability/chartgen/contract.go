// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// contractNode is chartgen's build-time representation of the JSON Schema
// vocabulary used by the pinned Olly contracts. It is emitted as plain Go
// data; provider runtime never reads or compiles JSON Schema.
type contractNode struct {
	Kinds           []string
	Enum            []string
	Properties      map[string]*contractNode
	Required        []string
	Additional      *contractNode
	AllowAdditional bool
	Items           *contractNode
	AnyOf           []*contractNode
	Deprecated      bool
}

var supportedContractKeywords = map[string]bool{
	"$ref": true, "type": true, "enum": true, "properties": true,
	"required": true, "additionalProperties": true, "items": true,
	"anyOf": true,
	// Annotation/identity vocabulary does not affect validation.
	"$schema": true, "$id": true, "$defs": true, "title": true,
	"description": true, "deprecated": true,
}

type contractDocument struct {
	path string
	root map[string]any
	defs map[string]any
}

func loadContracts(directory string, charts []chartInfo) (map[string]*contractNode, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "O11y*.schema.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	wanted := make(map[string]bool, len(charts))
	for _, chart := range charts {
		wanted[chart.ElementTag] = true
	}
	contracts := make(map[string]*contractNode, len(paths))
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("reading Olly contract %s: %w", path, readErr)
		}
		var root map[string]any
		decoderErr := json.Unmarshal(contents, &root)
		if decoderErr != nil {
			return nil, fmt.Errorf("parsing Olly contract %s: %w", path, decoderErr)
		}
		title, ok := root["title"].(string)
		if !ok || title == "" {
			return nil, fmt.Errorf("olly contract %s has no string title", path)
		}
		tag := "<" + title + ">"
		if !wanted[tag] {
			return nil, fmt.Errorf("olly contract %s describes unsupported element %q", path, tag)
		}
		if _, duplicate := contracts[tag]; duplicate {
			return nil, fmt.Errorf("duplicate Olly contract for %s", tag)
		}
		defs, _ := root["$defs"].(map[string]any)
		document := contractDocument{path: path, root: root, defs: defs}
		for _, name := range sortedAnyKeys(defs) {
			definition, definitionOK := defs[name].(map[string]any)
			if !definitionOK {
				return nil, fmt.Errorf("%s definition %s is not an object", path, name)
			}
			if _, compileErr := document.compile(definition, "#/$defs/"+name, map[string]bool{name: true}); compileErr != nil {
				return nil, compileErr
			}
		}
		node, compileErr := document.compile(root, "#", map[string]bool{})
		if compileErr != nil {
			return nil, compileErr
		}
		contracts[tag] = node
	}
	if len(contracts) != len(wanted) {
		var missing []string
		for tag := range wanted {
			if contracts[tag] == nil {
				missing = append(missing, tag)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("missing Olly contracts for %s", strings.Join(missing, ", "))
	}
	return contracts, nil
}

func (d contractDocument) compile(raw map[string]any, location string, resolving map[string]bool) (*contractNode, error) {
	for keyword := range raw {
		if !supportedContractKeywords[keyword] {
			return nil, fmt.Errorf("%s %s uses unsupported JSON Schema keyword %q", d.path, location, keyword)
		}
	}
	if rawReference, present := raw["$ref"]; present {
		reference, ok := rawReference.(string)
		if !ok || reference == "" {
			return nil, fmt.Errorf("%s %s has non-string or empty $ref", d.path, location)
		}
		for keyword := range raw {
			switch keyword {
			case "$ref", "title", "description", "deprecated":
				// Annotation siblings do not change the referenced validation
				// semantics and can safely be overlaid below. Draft 2020-12 also
				// permits applicative siblings, but silently ignoring one would
				// make our compact runtime contract weaker than Olly's schema.
			default:
				return nil, fmt.Errorf("%s %s has unsupported structural $ref sibling %q", d.path, location, keyword)
			}
		}
		const prefix = "#/$defs/"
		if !strings.HasPrefix(reference, prefix) || strings.Contains(strings.TrimPrefix(reference, prefix), "/") {
			return nil, fmt.Errorf("%s %s has non-local or nested $ref %q", d.path, location, reference)
		}
		name := strings.TrimPrefix(reference, prefix)
		if resolving[name] {
			return nil, fmt.Errorf("%s has cyclic JSON Schema reference at %s", d.path, reference)
		}
		target, ok := d.defs[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s %s references missing definition %q", d.path, location, name)
		}
		next := cloneBools(resolving)
		next[name] = true
		compiled, err := d.compile(target, reference, next)
		if err != nil {
			return nil, err
		}
		copy := *compiled
		if deprecated, present := raw["deprecated"]; present {
			value, valid := deprecated.(bool)
			if !valid {
				return nil, fmt.Errorf("%s %s has non-boolean deprecated annotation", d.path, location)
			}
			copy.Deprecated = value
		}
		return &copy, nil
	}
	if _, present := raw["anyOf"]; present {
		for keyword := range raw {
			switch keyword {
			case "anyOf", "title", "description", "deprecated":
				// The pinned contracts use anyOf as a leaf union with annotation
				// siblings only. Restricting that shape keeps mapping traversal and
				// the emitted compact validator exactly aligned with Draft 2020-12;
				// a future applicative sibling must be modeled explicitly first.
			default:
				return nil, fmt.Errorf("%s %s has unsupported structural anyOf sibling %q", d.path, location, keyword)
			}
		}
	}

	node := &contractNode{AllowAdditional: true}
	if deprecated, present := raw["deprecated"]; present {
		value, valid := deprecated.(bool)
		if !valid {
			return nil, fmt.Errorf("%s %s has non-boolean deprecated annotation", d.path, location)
		}
		node.Deprecated = value
	}
	if rawType, ok := raw["type"]; ok {
		kinds, err := contractKinds(rawType)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", d.path, location, err)
		}
		node.Kinds = kinds
	}
	if rawEnumValue, present := raw["enum"]; present {
		rawEnum, ok := rawEnumValue.([]any)
		if !ok || len(rawEnum) == 0 {
			return nil, fmt.Errorf("%s %s has non-array or empty enum", d.path, location)
		}
		for _, value := range rawEnum {
			stringValue, stringOK := value.(string)
			if !stringOK {
				return nil, fmt.Errorf("%s %s has non-string enum value %v", d.path, location, value)
			}
			node.Enum = append(node.Enum, stringValue)
		}
	}
	if rawPropertiesValue, present := raw["properties"]; present {
		rawProperties, ok := rawPropertiesValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s %s has non-object properties", d.path, location)
		}
		node.Properties = make(map[string]*contractNode, len(rawProperties))
		for _, name := range sortedAnyKeys(rawProperties) {
			child, childOK := rawProperties[name].(map[string]any)
			if !childOK {
				return nil, fmt.Errorf("%s %s/properties/%s is not a schema object", d.path, location, name)
			}
			compiled, err := d.compile(child, location+"/properties/"+name, resolving)
			if err != nil {
				return nil, err
			}
			node.Properties[name] = compiled
		}
	}
	if rawRequiredValue, present := raw["required"]; present {
		rawRequired, ok := rawRequiredValue.([]any)
		if !ok {
			return nil, fmt.Errorf("%s %s has non-array required", d.path, location)
		}
		seenRequired := map[string]bool{}
		for _, value := range rawRequired {
			name, nameOK := value.(string)
			if !nameOK || name == "" || seenRequired[name] {
				return nil, fmt.Errorf("%s %s has non-string, empty, or duplicate required member", d.path, location)
			}
			seenRequired[name] = true
			node.Required = append(node.Required, name)
		}
		sort.Strings(node.Required)
	}
	if rawAdditional, ok := raw["additionalProperties"]; ok {
		switch value := rawAdditional.(type) {
		case bool:
			node.AllowAdditional = value
		case map[string]any:
			compiled, err := d.compile(value, location+"/additionalProperties", resolving)
			if err != nil {
				return nil, err
			}
			node.Additional = compiled
			node.AllowAdditional = true
		default:
			return nil, fmt.Errorf("%s %s has unsupported additionalProperties %T", d.path, location, rawAdditional)
		}
	}
	if rawItemsValue, present := raw["items"]; present {
		rawItems, ok := rawItemsValue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s %s has non-object items", d.path, location)
		}
		compiled, err := d.compile(rawItems, location+"/items", resolving)
		if err != nil {
			return nil, err
		}
		node.Items = compiled
	}
	if rawAnyOfValue, present := raw["anyOf"]; present {
		rawAnyOf, ok := rawAnyOfValue.([]any)
		if !ok || len(rawAnyOf) == 0 {
			return nil, fmt.Errorf("%s %s has non-array or empty anyOf", d.path, location)
		}
		for index, rawAlternative := range rawAnyOf {
			alternative, alternativeOK := rawAlternative.(map[string]any)
			if !alternativeOK {
				return nil, fmt.Errorf("%s %s/anyOf/%d is not a schema object", d.path, location, index)
			}
			compiled, err := d.compile(alternative, fmt.Sprintf("%s/anyOf/%d", location, index), resolving)
			if err != nil {
				return nil, err
			}
			node.AnyOf = append(node.AnyOf, compiled)
		}
	}
	return node, nil
}

func contractKinds(raw any) ([]string, error) {
	allowed := map[string]bool{
		"null": true, "object": true, "array": true, "string": true,
		"boolean": true, "number": true,
	}
	validate := func(values []string) ([]string, error) {
		if len(values) == 0 {
			return nil, fmt.Errorf("type union is empty")
		}
		seen := map[string]bool{}
		for _, kind := range values {
			if !allowed[kind] {
				return nil, fmt.Errorf("type contains unsupported kind %q", kind)
			}
			if seen[kind] {
				return nil, fmt.Errorf("type contains duplicate kind %q", kind)
			}
			seen[kind] = true
		}
		return values, nil
	}
	switch value := raw.(type) {
	case string:
		return validate([]string{value})
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			kind, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("type union contains %T", item)
			}
			result = append(result, kind)
		}
		return validate(result)
	default:
		return nil, fmt.Errorf("type is %T, want string or string array", raw)
	}
}

func cloneBools(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sortedAnyKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func generateContractRegistry(charts []chartInfo, contracts map[string]*contractNode, packageName string) (string, error) {
	var b strings.Builder
	b.WriteString("// Copyright Splunk, Inc.\n// SPDX-License-Identifier: MPL-2.0\n\n")
	fmt.Fprintf(&b, "// %s from %s/elements/*.schema.json; DO NOT EDIT.\n\n", generatedCodeMarker, chartSchemasPath)
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("func contractForElement(tag string) *contractNode {\n\tswitch tag {\n")
	for _, chart := range charts {
		fmt.Fprintf(&b, "\tcase %q:\n\t\treturn ", chart.ElementTag)
		writeContractNode(&b, contracts[chart.ElementTag])
		b.WriteString("\n")
	}
	b.WriteString("\tdefault:\n\t\treturn nil\n\t}\n}\n")
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("generated contract registry does not compile: %w\n\n%s", err, b.String())
	}
	return string(formatted), nil
}

func writeContractNode(b *strings.Builder, node *contractNode) {
	b.WriteString("&contractNode{")
	if len(node.Kinds) > 0 {
		fmt.Fprintf(b, "Kinds: []string{%s},", quotedStrings(node.Kinds))
	}
	if len(node.Enum) > 0 {
		fmt.Fprintf(b, "Enum: []string{%s},", quotedStrings(node.Enum))
	}
	if len(node.Properties) > 0 {
		b.WriteString("Properties: map[string]*contractNode{")
		for _, name := range sortedContractKeys(node.Properties) {
			fmt.Fprintf(b, "%q:", name)
			writeContractNode(b, node.Properties[name])
			b.WriteString(",")
		}
		b.WriteString("},")
	}
	if len(node.Required) > 0 {
		fmt.Fprintf(b, "Required: []string{%s},", quotedStrings(node.Required))
	}
	if node.Additional != nil {
		b.WriteString("Additional:")
		writeContractNode(b, node.Additional)
		b.WriteString(",")
	}
	if node.AllowAdditional {
		b.WriteString("AllowAdditional:true,")
	}
	if node.Items != nil {
		b.WriteString("Items:")
		writeContractNode(b, node.Items)
		b.WriteString(",")
	}
	if len(node.AnyOf) > 0 {
		b.WriteString("AnyOf:[]*contractNode{")
		for _, alternative := range node.AnyOf {
			writeContractNode(b, alternative)
			b.WriteString(",")
		}
		b.WriteString("},")
	}
	if node.Deprecated {
		b.WriteString("Deprecated:true,")
	}
	b.WriteString("}")
}

func sortedContractKeys(values map[string]*contractNode) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func quotedStrings(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ",")
}

// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const scaffoldHeader = "# Copyright Splunk, Inc.\n# SPDX-License-Identifier: MPL-2.0\n\n"

type scaffoldOptions struct {
	ContractsDir string
	Contract     string
	Pointer      string
	Type         string
}

type scaffoldYAMLSchema struct {
	Type        string                          `yaml:"type,omitempty"`
	Version     int                             `yaml:"version"`
	Description string                          `yaml:"description,omitempty"`
	Dashify     *scaffoldYAMLDashify            `yaml:"dashify,omitempty"`
	Properties  map[string]scaffoldYAMLProperty `yaml:"properties"`
}

type scaffoldYAMLDashify struct {
	Element scaffoldYAMLElement `yaml:"element"`
}

type scaffoldYAMLElement struct {
	Name string `yaml:"name"`
	Args []any  `yaml:"args"`
}

type scaffoldYAMLProperty struct {
	Type        string                          `yaml:"type"`
	Optional    bool                            `yaml:"optional,omitempty"`
	Required    bool                            `yaml:"required,omitempty"`
	Values      []string                        `yaml:"values,omitempty"`
	Description string                          `yaml:"description"`
	Mapping     *scaffoldYAMLMapping            `yaml:"mapping,omitempty"`
	Item        *scaffoldYAMLItem               `yaml:"item,omitempty"`
	Properties  map[string]scaffoldYAMLProperty `yaml:"properties,omitempty"`
	Variants    map[string]scaffoldYAMLProperty `yaml:"variants,omitempty"`
}

type scaffoldYAMLMapping struct {
	Path []string `yaml:"path"`
}

type scaffoldYAMLItem struct {
	Type       string                          `yaml:"type,omitempty"`
	Properties map[string]scaffoldYAMLProperty `yaml:"properties,omitempty"`
}

type scaffoldBuilder struct {
	warnings []string
	seen     map[string]bool
}

// scaffoldContract converts one vendored Olly JSON Schema, or one of its
// top-level $defs, into a deterministic starter document for the hand-curated
// chart YAML DSL. It never writes files.
func scaffoldContract(options scaffoldOptions) ([]byte, []string, error) {
	if options.ContractsDir == "" {
		return nil, nil, fmt.Errorf("contracts directory is required")
	}
	if options.Contract == "" {
		return nil, nil, fmt.Errorf("contract is required")
	}
	if filepath.Base(options.Contract) != options.Contract || strings.ContainsAny(options.Contract, `/\\`) {
		return nil, nil, fmt.Errorf("contract %q must be a basename within the elements directory", options.Contract)
	}
	if !strings.HasSuffix(options.Contract, ".schema.json") {
		return nil, nil, fmt.Errorf("contract %q must end in .schema.json", options.Contract)
	}

	pointer := options.Pointer
	if pointer == "" {
		pointer = "#"
	}
	document, title, err := loadContractDocument(filepath.Join(options.ContractsDir, options.Contract))
	if err != nil {
		return nil, nil, err
	}

	raw := document.root
	location := "#"
	fragment := false
	if pointer != "#" {
		const prefix = "#/$defs/"
		name := strings.TrimPrefix(pointer, prefix)
		if !strings.HasPrefix(pointer, prefix) || name == "" || strings.Contains(name, "/") {
			return nil, nil, fmt.Errorf("pointer %q must be # or #/$defs/<name>", pointer)
		}
		definition, ok := document.defs[name].(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("contract %s has no object definition at %s", options.Contract, pointer)
		}
		raw = definition
		location = pointer
		fragment = true
	}
	if fragment && options.Type != "" {
		return nil, nil, fmt.Errorf("type must be omitted when scaffolding a $defs fragment")
	}
	if !fragment && options.Type == "" {
		return nil, nil, fmt.Errorf("type is required when scaffolding a complete contract")
	}

	node, err := document.compile(raw, location, map[string]bool{})
	if err != nil {
		return nil, nil, err
	}
	if !contractHasKind(node, "object") || len(node.Properties) == 0 {
		return nil, nil, fmt.Errorf("%s is not an object schema with properties", location)
	}

	builder := &scaffoldBuilder{}
	properties, err := builder.objectProperties(node, location)
	if err != nil {
		return nil, nil, err
	}
	if len(properties) == 0 {
		return nil, nil, fmt.Errorf("%s has no non-deprecated properties to scaffold", location)
	}
	output := scaffoldYAMLSchema{Version: supportSchemaVersion, Properties: properties}
	if !fragment {
		output.Type = options.Type
		output.Description = node.Description
		if output.Description == "" {
			output.Description = fmt.Sprintf("Terraform-facing schema for Dashify's %s element.", title)
			builder.warn(location, "has no description; generated a starter description")
		}
		output.Dashify = &scaffoldYAMLDashify{Element: scaffoldYAMLElement{Name: title, Args: []any{}}}
	}

	encoded, err := yaml.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding scaffold YAML: %w", err)
	}
	sort.Strings(builder.warnings)
	return append([]byte(scaffoldHeader), encoded...), builder.warnings, nil
}

func (b *scaffoldBuilder) objectProperties(node *contractNode, location string) (map[string]scaffoldYAMLProperty, error) {
	result := make(map[string]scaffoldYAMLProperty, len(node.Properties))
	for _, jsonName := range sortedContractKeys(node.Properties) {
		child := node.Properties[jsonName]
		childLocation := location + "/properties/" + jsonName
		if child.Deprecated {
			b.warn(childLocation, "is deprecated and was omitted")
			continue
		}
		terraformName := scaffoldTerraformName(jsonName)
		if terraformName == "" {
			return nil, fmt.Errorf("%s cannot be converted to a Terraform name", childLocation)
		}
		if _, duplicate := result[terraformName]; duplicate {
			return nil, fmt.Errorf("%s collides as Terraform name %q", childLocation, terraformName)
		}
		property, err := b.property(child, childLocation, true, containsPropertyName(node.Required, jsonName))
		if err != nil {
			return nil, err
		}
		property.Mapping = &scaffoldYAMLMapping{Path: []string{jsonName}}
		result[terraformName] = property
	}
	return result, nil
}

func (b *scaffoldBuilder) property(node *contractNode, location string, includePresence, required bool) (scaffoldYAMLProperty, error) {
	alternatives := scaffoldAlternatives(node)
	description := node.Description
	if description == "" && len(alternatives) == 1 {
		description = alternatives[0].Description
	}
	property := scaffoldYAMLProperty{Description: description}
	if property.Description == "" {
		property.Description = fmt.Sprintf("Value of the Olly property at %s.", location)
		b.warn(location, "has no description; generated a starter description")
	}
	if includePresence {
		property.Required = required
		property.Optional = !required
	}

	if len(alternatives) > 1 {
		property.Type = "oneof"
		property.Variants = make(map[string]scaffoldYAMLProperty, len(alternatives))
		for index, alternative := range alternatives {
			name := scaffoldVariantName(alternative, index)
			if _, duplicate := property.Variants[name]; duplicate {
				return scaffoldYAMLProperty{}, fmt.Errorf("%s has alternatives that collide as variant %q", location, name)
			}
			variant, err := b.property(alternative, location, false, false)
			if err != nil {
				return scaffoldYAMLProperty{}, err
			}
			if variant.Type != "string" && variant.Type != "bool" && variant.Type != "float" && variant.Type != "enum" && variant.Type != "block" {
				return scaffoldYAMLProperty{}, fmt.Errorf("%s alternative %q becomes unsupported oneof variant type %q", location, name, variant.Type)
			}
			for childName, child := range variant.Properties {
				if child.Required {
					return scaffoldYAMLProperty{}, fmt.Errorf("%s alternative %q contains required child %q, which the chart DSL cannot represent in a oneof", location, name, childName)
				}
			}
			variant.Optional = false
			variant.Required = false
			variant.Mapping = nil
			property.Variants[name] = variant
		}
		return property, nil
	}
	if len(alternatives) == 1 {
		node = alternatives[0]
	}

	if len(node.Enum) > 0 {
		property.Type = "enum"
		property.Values = append([]string(nil), node.Enum...)
		return property, nil
	}
	kinds := scaffoldNonNullKinds(node.Kinds)
	if len(kinds) != 1 {
		return scaffoldYAMLProperty{}, fmt.Errorf("%s has unsupported JSON types %v", location, node.Kinds)
	}
	switch kinds[0] {
	case "string":
		property.Type = "string"
	case "boolean":
		property.Type = "bool"
	case "number":
		property.Type = "float"
		b.warn(location, "is a JSON number scaffolded as Terraform float; review whether number (int64) is intended")
	case "object":
		if node.Additional != nil && len(node.Properties) > 0 {
			return scaffoldYAMLProperty{}, fmt.Errorf("%s mixes named properties with typed additionalProperties", location)
		}
		if node.Additional != nil {
			item, err := b.objectItem(node.Additional, location+"/additionalProperties")
			if err != nil {
				return scaffoldYAMLProperty{}, err
			}
			property.Type = "map"
			property.Item = item
			break
		}
		if len(node.Properties) == 0 {
			return scaffoldYAMLProperty{}, fmt.Errorf("%s is an untyped object, which the chart DSL cannot scaffold safely", location)
		}
		children, err := b.objectProperties(node, location)
		if err != nil {
			return scaffoldYAMLProperty{}, err
		}
		if len(children) == 0 {
			return scaffoldYAMLProperty{}, fmt.Errorf("%s has no non-deprecated properties to scaffold", location)
		}
		property.Type = "block"
		property.Properties = children
	case "array":
		if node.Items == nil {
			return scaffoldYAMLProperty{}, fmt.Errorf("%s is an array without an item schema", location)
		}
		item, err := b.arrayItem(node.Items, location+"/items")
		if err != nil {
			return scaffoldYAMLProperty{}, err
		}
		property.Type = "list"
		property.Item = item
	default:
		return scaffoldYAMLProperty{}, fmt.Errorf("%s has unsupported JSON type %q", location, kinds[0])
	}
	return property, nil
}

func (b *scaffoldBuilder) objectItem(node *contractNode, location string) (*scaffoldYAMLItem, error) {
	alternatives := scaffoldAlternatives(node)
	if len(alternatives) == 1 {
		node = alternatives[0]
	} else if len(alternatives) > 1 {
		return nil, fmt.Errorf("%s is a union-valued map, which the chart DSL cannot represent", location)
	}
	if !contractHasKind(node, "object") || len(node.Properties) == 0 {
		return nil, fmt.Errorf("%s is not a fixed-shape object map value", location)
	}
	properties, err := b.objectProperties(node, location)
	if err != nil {
		return nil, err
	}
	if len(properties) == 0 {
		return nil, fmt.Errorf("%s has no non-deprecated properties to scaffold", location)
	}
	return &scaffoldYAMLItem{Properties: properties}, nil
}

func (b *scaffoldBuilder) arrayItem(node *contractNode, location string) (*scaffoldYAMLItem, error) {
	alternatives := scaffoldAlternatives(node)
	if len(alternatives) == 1 {
		node = alternatives[0]
	} else if len(alternatives) > 1 {
		return nil, fmt.Errorf("%s is a union-valued array item, which the chart DSL cannot represent", location)
	}
	if len(node.Enum) > 0 {
		return nil, fmt.Errorf("%s is an enum array item, whose allowed values the chart DSL cannot preserve", location)
	}
	kinds := scaffoldNonNullKinds(node.Kinds)
	if len(kinds) != 1 {
		return nil, fmt.Errorf("%s has unsupported array item types %v", location, node.Kinds)
	}
	switch kinds[0] {
	case "string":
		return &scaffoldYAMLItem{Type: "string"}, nil
	case "object":
		if len(node.Properties) == 0 {
			return nil, fmt.Errorf("%s is not a fixed-shape object array item", location)
		}
		properties, err := b.objectProperties(node, location)
		if err != nil {
			return nil, err
		}
		if len(properties) == 0 {
			return nil, fmt.Errorf("%s has no non-deprecated properties to scaffold", location)
		}
		return &scaffoldYAMLItem{Properties: properties}, nil
	default:
		return nil, fmt.Errorf("%s has array item type %q, but DSL v1 supports only string or fixed-shape object list items", location, kinds[0])
	}
}

func scaffoldAlternatives(node *contractNode) []*contractNode {
	if len(node.AnyOf) > 0 {
		result := make([]*contractNode, 0, len(node.AnyOf))
		for _, alternative := range node.AnyOf {
			if scaffoldNullOnly(alternative) {
				continue
			}
			result = append(result, alternative)
		}
		return result
	}
	kinds := scaffoldNonNullKinds(node.Kinds)
	if len(kinds) <= 1 {
		return []*contractNode{node}
	}
	result := make([]*contractNode, 0, len(kinds))
	for _, kind := range kinds {
		copy := *node
		copy.Kinds = []string{kind}
		result = append(result, &copy)
	}
	return result
}

func scaffoldNonNullKinds(kinds []string) []string {
	result := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if kind != "null" {
			result = append(result, kind)
		}
	}
	return result
}

func scaffoldNullOnly(node *contractNode) bool {
	return len(node.AnyOf) == 0 && len(scaffoldNonNullKinds(node.Kinds)) == 0 && contractHasKind(node, "null")
}

func contractHasKind(node *contractNode, wanted string) bool {
	for _, kind := range node.Kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func scaffoldVariantName(node *contractNode, index int) string {
	if node.ReferenceName != "" {
		name := strings.TrimPrefix(node.ReferenceName, "O11y")
		if converted := scaffoldTerraformName(name); converted != "" {
			return converted
		}
	}
	if len(node.Enum) > 0 {
		return "enum"
	}
	kinds := scaffoldNonNullKinds(node.Kinds)
	if len(kinds) == 1 {
		switch kinds[0] {
		case "boolean":
			return "bool"
		case "number":
			return "float"
		case "array":
			return "list"
		case "object":
			return "object"
		default:
			return kinds[0]
		}
	}
	return fmt.Sprintf("variant_%d", index+1)
}

func scaffoldTerraformName(name string) string {
	var result []rune
	runes := []rune(name)
	for index, current := range runes {
		if current == '-' || current == ' ' || current == '.' {
			if len(result) > 0 && result[len(result)-1] != '_' {
				result = append(result, '_')
			}
			continue
		}
		if unicode.IsUpper(current) {
			previousIsLowerOrDigit := index > 0 && (unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1]))
			nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if len(result) > 0 && result[len(result)-1] != '_' && (previousIsLowerOrDigit || nextIsLower) {
				result = append(result, '_')
			}
			current = unicode.ToLower(current)
		}
		if unicode.IsLetter(current) || unicode.IsDigit(current) || current == '_' {
			result = append(result, unicode.ToLower(current))
		}
	}
	return strings.Trim(string(result), "_")
}

func (b *scaffoldBuilder) warn(location, message string) {
	warning := location + " " + message
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	if b.seen[warning] {
		return
	}
	b.seen[warning] = true
	b.warnings = append(b.warnings, warning)
}

// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type mappingWitness struct {
	value  any
	covers []string
}

func validateMappingWitnesses(schemaPaths []string, schemasDirectory string, contracts map[string]*contractNode) error {
	for _, schemaPath := range schemaPaths {
		resolved, err := loadSchema(schemasDirectory, schemaPath)
		if err != nil {
			return err
		}
		tag := "<" + resolved.Dashify.Element.Name + ">"
		contract := contracts[tag]
		if contract == nil {
			return fmt.Errorf("%s has no compiled Olly contract", schemaPath)
		}
		if err := validateNormalizations(schemaPath, resolved.Normalizations, contract); err != nil {
			return err
		}
		if err := validatePropertyMappings(schemaPath, resolved.Properties, contract); err != nil {
			return err
		}
		expected, covered := map[string]bool{}, map[string]bool{}
		for _, name := range sortedKeys(resolved.Properties) {
			prop := resolved.Properties[name]
			if !isGeneratedProperty(prop) {
				return fmt.Errorf("%s: property %q has no generated witness", schemaPath, name)
			}
			collectExpectedCoverage(prop, name, expected)
			for _, witness := range propertyWitnesses(prop, name) {
				properties := minimalContractProperties(tag)
				value := witness.value
				if prop.Mapping.Wrap == "array" {
					value = []any{value}
				}
				setWitnessPath(properties, prop.Mapping.Path, value)
				if failures := validateCompiledContract("", properties, contract); len(failures) > 0 {
					return fmt.Errorf("%s: Olly rejected generated witness for %s: %s", schemaPath, strings.Join(witness.covers, ", "), strings.Join(failures, "; "))
				}
				for _, label := range witness.covers {
					covered[label] = true
				}
			}
		}
		for _, label := range sortedBoolKeys(expected) {
			if !covered[label] {
				return fmt.Errorf("%s: no Olly-validated generated witness covers %s", schemaPath, label)
			}
		}
	}
	return nil
}

func validatePropertyMappings(schemaPath string, properties map[string]property, contract *contractNode) error {
	for _, name := range sortedKeys(properties) {
		if err := validatePropertyMapping(schemaPath, name, properties[name], []*contractNode{contract}); err != nil {
			return err
		}
	}
	return nil
}

func validatePropertyMapping(schemaPath, logicalPath string, prop property, parents []*contractNode) error {
	targets := contractNodesAtPath(parents, prop.Mapping.Path)
	if len(targets) == 0 {
		return fmt.Errorf("%s: property %q mapping path %s is absent from Olly", schemaPath, logicalPath, strings.Join(prop.Mapping.Path, "."))
	}
	for _, target := range targets {
		if target.Deprecated {
			return fmt.Errorf("%s: property %q maps to deprecated Olly path %s", schemaPath, logicalPath, strings.Join(prop.Mapping.Path, "."))
		}
	}
	for _, witness := range propertyWitnesses(prop, logicalPath) {
		value := witness.value
		if prop.Mapping.Wrap == "array" {
			value = []any{value}
		}
		accepted := false
		for _, target := range targets {
			if len(validateCompiledContract("", value, target)) == 0 {
				accepted = true
				break
			}
		}
		if !accepted {
			return fmt.Errorf("%s: property %q emits %T rejected by its Olly mapping target", schemaPath, logicalPath, value)
		}
	}

	switch prop.Type {
	case "block":
		return validateChildMappings(schemaPath, logicalPath, prop.Properties, targets)
	case "list":
		if prop.Item != nil && prop.Item.Type == "" {
			items := contractCollectionChildren(targets, true)
			if len(items) == 0 {
				return fmt.Errorf("%s: property %q maps object-list children to a non-array Olly target", schemaPath, logicalPath)
			}
			return validateChildMappings(schemaPath, logicalPath+"[]", prop.Item.Properties, items)
		}
	case "map":
		items := contractCollectionChildren(targets, false)
		if len(items) == 0 {
			return fmt.Errorf("%s: property %q maps object-map children to an Olly target without typed additionalProperties", schemaPath, logicalPath)
		}
		return validateChildMappings(schemaPath, logicalPath+".*", prop.Item.Properties, items)
	case "oneof":
		for _, variantName := range sortedKeys(prop.Variants) {
			variant := prop.Variants[variantName]
			if variant.Type != "block" {
				continue
			}
			if err := validateChildMappings(schemaPath, logicalPath+"."+variantName, variant.Properties, targets); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateChildMappings(schemaPath, logicalPrefix string, children map[string]property, parents []*contractNode) error {
	for _, name := range sortedKeys(children) {
		if err := validatePropertyMapping(schemaPath, logicalPrefix+"."+name, children[name], parents); err != nil {
			return err
		}
	}
	return nil
}

func contractNodesAtPath(nodes []*contractNode, path []string) []*contractNode {
	current := expandContractAlternatives(nodes)
	for index, segment := range path {
		var next []*contractNode
		for _, node := range current {
			if segment == "*" {
				if node.Additional != nil {
					next = append(next, node.Additional)
				}
				if node.Items != nil {
					next = append(next, node.Items)
				}
				continue
			}
			if node.Properties != nil && node.Properties[segment] != nil {
				next = append(next, node.Properties[segment])
			}
		}
		current = uniqueContractNodes(next)
		if index < len(path)-1 {
			current = expandContractAlternatives(current)
		}
	}
	return uniqueContractNodes(current)
}

func expandContractAlternatives(nodes []*contractNode) []*contractNode {
	var expanded []*contractNode
	var visit func(*contractNode)
	visit = func(node *contractNode) {
		if node == nil {
			return
		}
		if len(node.AnyOf) == 0 {
			expanded = append(expanded, node)
			return
		}
		for _, alternative := range node.AnyOf {
			visit(alternative)
		}
	}
	for _, node := range nodes {
		visit(node)
	}
	return uniqueContractNodes(expanded)
}

func uniqueContractNodes(nodes []*contractNode) []*contractNode {
	seen := map[*contractNode]bool{}
	result := make([]*contractNode, 0, len(nodes))
	for _, node := range nodes {
		if node != nil && !seen[node] {
			seen[node] = true
			result = append(result, node)
		}
	}
	return result
}

func contractCollectionChildren(nodes []*contractNode, array bool) []*contractNode {
	var result []*contractNode
	for _, node := range expandContractAlternatives(nodes) {
		if array && node.Items != nil {
			result = append(result, node.Items)
		}
		if !array && node.Additional != nil {
			result = append(result, node.Additional)
		}
	}
	return expandContractAlternatives(result)
}

func validateNormalizations(schemaPath string, normalizations []normalization, contract *contractNode) error {
	for index, rule := range normalizations {
		label := fmt.Sprintf("%s normalization %d (%s)", schemaPath, index, rule.Kind)
		if rule.Kind != "fallback" && rule.Kind != "wrap_scalar_array" && rule.Kind != "display_unit" && rule.Kind != "single_value_publish_label_options" {
			return fmt.Errorf("%s has unsupported kind", label)
		}
		if len(rule.Target) == 0 {
			return fmt.Errorf("%s has no target", label)
		}
		paths := map[string][]string{"target": rule.Target, "source": rule.Source, "unit": rule.Unit, "prefix": rule.Prefix, "suffix": rule.Suffix}
		pathNames := []string{"target", "source", "unit", "prefix", "suffix"}
		for _, pathName := range pathNames {
			path := paths[pathName]
			for _, segment := range path {
				if segment == "" {
					return fmt.Errorf("%s %s path has an empty segment", label, pathName)
				}
			}
			if len(path) > 0 && path[len(path)-1] == "*" {
				return fmt.Errorf("%s %s path cannot end in a wildcard", label, pathName)
			}
		}
		requirePaths := func(required, forbidden []string) error {
			for _, name := range required {
				if len(paths[name]) == 0 {
					return fmt.Errorf("%s requires %s", label, name)
				}
			}
			for _, name := range forbidden {
				if len(paths[name]) != 0 {
					return fmt.Errorf("%s does not allow %s", label, name)
				}
			}
			return nil
		}
		switch rule.Kind {
		case "fallback":
			if err := requirePaths([]string{"target", "source"}, []string{"unit", "prefix", "suffix"}); err != nil {
				return err
			}
		case "wrap_scalar_array":
			if err := requirePaths([]string{"target"}, []string{"source", "unit", "prefix", "suffix"}); err != nil {
				return err
			}
		case "display_unit":
			if err := requirePaths([]string{"target", "unit", "prefix", "suffix"}, []string{"source"}); err != nil {
				return err
			}
		case "single_value_publish_label_options":
			if err := requirePaths([]string{"target", "source"}, []string{"unit", "prefix", "suffix"}); err != nil {
				return err
			}
		}
		targetWildcards := wildcardCount(rule.Target)
		targetBindings, bindingErr := normalizationBindingKinds(contract, rule.Target)
		if bindingErr != nil {
			return fmt.Errorf("%s target: %w", label, bindingErr)
		}
		for _, pathName := range pathNames {
			path := paths[pathName]
			if len(path) > 0 && wildcardCount(path) != targetWildcards {
				return fmt.Errorf("%s %s wildcard count does not match target", label, pathName)
			}
			if len(path) == 0 {
				continue
			}
			bindings, err := normalizationBindingKinds(contract, path)
			if err != nil {
				return fmt.Errorf("%s %s: %w", label, pathName, err)
			}
			if !sameBindingKinds(targetBindings, bindings) {
				return fmt.Errorf("%s %s wildcard bindings %v do not match target bindings %v", label, pathName, bindings, targetBindings)
			}
		}
		target, ok := contractNodeAtPath(contract, rule.Target)
		if !ok {
			return fmt.Errorf("%s target %s is absent from Olly", label, strings.Join(rule.Target, "."))
		}
		if target.Deprecated {
			return fmt.Errorf("%s target %s is deprecated", label, strings.Join(rule.Target, "."))
		}
		var legacyPaths [][]string
		switch rule.Kind {
		case "fallback", "single_value_publish_label_options":
			legacyPaths = append(legacyPaths, rule.Source)
		case "display_unit":
			legacyPaths = append(legacyPaths, rule.Unit, rule.Prefix, rule.Suffix)
		case "wrap_scalar_array":
			if len(validateCompiledContract("target", "value", target)) != 0 || len(validateCompiledContract("target", []any{"value"}, target)) != 0 {
				return fmt.Errorf("%s target must accept both scalar string and string array", label)
			}
			continue
		}
		for _, path := range legacyPaths {
			legacy, exists := contractNodeAtPath(contract, path)
			if len(path) == 0 || !exists {
				return fmt.Errorf("%s legacy path %s is absent from Olly", label, strings.Join(path, "."))
			}
			if !legacy.Deprecated {
				return fmt.Errorf("%s legacy path %s is not marked deprecated by Olly", label, strings.Join(path, "."))
			}
		}
		switch rule.Kind {
		case "fallback":
			source, _ := contractNodeAtPath(contract, rule.Source)
			if !allNonNullContractWitnessesAccepted(source, target) {
				return fmt.Errorf("%s source and target types are incompatible", label)
			}
		case "display_unit":
			unit, _ := contractNodeAtPath(contract, rule.Unit)
			prefix, _ := contractNodeAtPath(contract, rule.Prefix)
			suffix, _ := contractNodeAtPath(contract, rule.Suffix)
			if !allNonNullContractWitnessesAccepted(unit, target) ||
				len(validateCompiledContract("prefix", "prefix", prefix)) != 0 ||
				len(validateCompiledContract("suffix", "suffix", suffix)) != 0 ||
				len(validateCompiledContract("target", map[string]any{"prefix": "prefix"}, target)) != 0 ||
				len(validateCompiledContract("target", map[string]any{"suffix": "suffix"}, target)) != 0 {
				return fmt.Errorf("%s display-unit sources/target have incompatible shapes", label)
			}
		case "single_value_publish_label_options":
			source, _ := contractNodeAtPath(contract, rule.Source)
			if !singleValueNormalizationShapesMatch(source, target) {
				return fmt.Errorf("%s publishLabelOptions source/target have incompatible shapes", label)
			}
		}
	}
	return nil
}

func allNonNullContractWitnessesAccepted(source, target *contractNode) bool {
	found := false
	for _, value := range simpleContractWitnesses(source) {
		if value == nil {
			continue
		}
		found = true
		if len(validateCompiledContract("", value, target)) != 0 {
			return false
		}
	}
	return found
}

func singleValueNormalizationShapesMatch(source, target *contractNode) bool {
	var arrays []*contractNode
	for _, candidate := range append([]*contractNode{source}, expandContractAlternatives([]*contractNode{source})...) {
		if candidate != nil && candidate.Items != nil {
			arrays = append(arrays, candidate)
		}
	}
	for _, array := range uniqueContractNodes(arrays) {
		itemCandidates := expandContractAlternatives([]*contractNode{array.Items})
		for _, item := range itemCandidates {
			if item.Properties == nil {
				continue
			}
			unit, prefix, suffix := item.Properties["valueUnit"], item.Properties["valuePrefix"], item.Properties["valueSuffix"]
			if unit == nil || prefix == nil || suffix == nil ||
				!allNonNullContractWitnessesAccepted(unit, target) ||
				len(validateCompiledContract("", "prefix", prefix)) != 0 ||
				len(validateCompiledContract("", "suffix", suffix)) != 0 ||
				len(validateCompiledContract("", map[string]any{"prefix": "prefix"}, target)) != 0 ||
				len(validateCompiledContract("", map[string]any{"suffix": "suffix"}, target)) != 0 {
				continue
			}
			return true
		}
	}
	return false
}

func simpleContractWitnesses(node *contractNode) []any {
	if node == nil {
		return nil
	}
	if len(node.AnyOf) > 0 {
		var values []any
		for _, alternative := range node.AnyOf {
			values = append(values, simpleContractWitnesses(alternative)...)
		}
		return values
	}
	if len(node.Enum) > 0 {
		values := make([]any, len(node.Enum))
		for index, value := range node.Enum {
			values[index] = value
		}
		return values
	}
	var values []any
	for _, kind := range node.Kinds {
		switch kind {
		case "null":
			values = append(values, nil)
		case "string":
			values = append(values, "value")
		case "boolean":
			values = append(values, true)
		case "number":
			values = append(values, float64(1))
		case "array":
			values = append(values, []any{})
		case "object":
			values = append(values, map[string]any{})
		}
	}
	return values
}

func wildcardCount(path []string) int {
	count := 0
	for _, segment := range path {
		if segment == "*" {
			count++
		}
	}
	return count
}

func normalizationBindingKinds(node *contractNode, path []string) ([]string, error) {
	current := node
	var kinds []string
	for index, segment := range path {
		if current == nil {
			return nil, fmt.Errorf("path %s has no contract node at segment %d", strings.Join(path, "."), index)
		}
		if len(current.AnyOf) > 0 {
			return nil, fmt.Errorf("path %s traverses an anyOf union before segment %q", strings.Join(path, "."), segment)
		}
		if segment == "*" {
			switch {
			case current.Additional != nil && current.Items == nil:
				kinds = append(kinds, "map")
				current = current.Additional
			case current.Items != nil && current.Additional == nil:
				kinds = append(kinds, "list")
				current = current.Items
			default:
				return nil, fmt.Errorf("path %s has ambiguous or untyped wildcard container at segment %d", strings.Join(path, "."), index)
			}
			continue
		}
		if current.Properties == nil || current.Properties[segment] == nil {
			return nil, fmt.Errorf("path %s is absent at segment %q", strings.Join(path, "."), segment)
		}
		current = current.Properties[segment]
	}
	return kinds, nil
}

func sameBindingKinds(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func contractNodeAtPath(node *contractNode, path []string) (*contractNode, bool) {
	current := node
	for _, segment := range path {
		if segment == "*" {
			switch {
			case current.Additional != nil:
				current = current.Additional
			case current.Items != nil:
				current = current.Items
			default:
				return nil, false
			}
			continue
		}
		if current.Properties == nil || current.Properties[segment] == nil {
			return nil, false
		}
		current = current.Properties[segment]
	}
	return current, true
}

func validateCompiledContract(path string, value any, node *contractNode) []string {
	if len(node.AnyOf) > 0 {
		matched := false
		for _, alternative := range node.AnyOf {
			if len(validateCompiledContract(path, value, alternative)) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			return []string{path + " does not match anyOf"}
		}
	}
	if len(node.Kinds) > 0 && !generatorKindMatches(value, node.Kinds) {
		return []string{fmt.Sprintf("%s is %T, want %s", path, value, strings.Join(node.Kinds, " or "))}
	}
	if len(node.Enum) > 0 {
		text, ok := value.(string)
		if !ok || !containsGeneratorString(node.Enum, text) {
			return []string{path + " is outside enum"}
		}
	}
	var failures []string
	if object, ok := value.(map[string]any); ok {
		for _, required := range node.Required {
			if _, exists := object[required]; !exists {
				failures = append(failures, witnessPath(path, required)+" is required")
			}
		}
		for _, name := range sortedAnyKeys(object) {
			childPath := witnessPath(path, name)
			if child := node.Properties[name]; child != nil {
				failures = append(failures, validateCompiledContract(childPath, object[name], child)...)
			} else if node.Additional != nil {
				failures = append(failures, validateCompiledContract(childPath, object[name], node.Additional)...)
			} else if !node.AllowAdditional {
				failures = append(failures, childPath+" is closed")
			}
		}
	}
	if array, ok := value.([]any); ok && node.Items != nil {
		for index, item := range array {
			failures = append(failures, validateCompiledContract(witnessPath(path, strconv.Itoa(index)), item, node.Items)...)
		}
	}
	return failures
}

func generatorKindMatches(value any, kinds []string) bool {
	for _, kind := range kinds {
		switch kind {
		case "null":
			if value == nil {
				return true
			}
		case "object":
			_, ok := value.(map[string]any)
			if ok {
				return true
			}
		case "array":
			_, ok := value.([]any)
			if ok {
				return true
			}
		case "string":
			_, ok := value.(string)
			if ok {
				return true
			}
		case "boolean":
			_, ok := value.(bool)
			if ok {
				return true
			}
		case "number":
			valueType := reflect.TypeOf(value)
			if valueType == nil {
				continue
			}
			switch valueType.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
				reflect.Float32, reflect.Float64:
				return true
			}
		}
	}
	return false
}

func containsGeneratorString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func witnessPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

func minimalContractProperties(tag string) map[string]any {
	properties := map[string]any{
		"chart":  map[string]any{},
		"widget": map[string]any{},
	}
	if tag != "<o11y:Text>" {
		properties["datasource"] = map[string]any{"program": "data('witness').publish()"}
	}
	return properties
}

func propertyWitnesses(prop property, logicalPath string) []mappingWitness {
	switch prop.Type {
	case "string":
		value := any("value")
		if prop.Mapping.Transform == "relative_duration" {
			value = map[string]any{"type": "relative", "range": float64(3600000), "rangeEnd": float64(0)}
		}
		return []mappingWitness{{value: value, covers: []string{logicalPath}}}
	case "enum":
		witnesses := make([]mappingWitness, 0, len(prop.Values))
		for _, value := range prop.Values {
			witnesses = append(witnesses, mappingWitness{value: value, covers: []string{logicalPath, logicalPath + "=" + value}})
		}
		return witnesses
	case "bool":
		value := true
		if prop.Mapping.Transform == "negate" {
			value = false
		}
		return []mappingWitness{{value: value, covers: []string{logicalPath}}}
	case "number":
		return []mappingWitness{{value: float64(witnessInteger(prop)), covers: []string{logicalPath}}}
	case "float":
		return []mappingWitness{{value: float64(1.5), covers: []string{logicalPath}}}
	case "block":
		return objectWitnesses(prop.Properties, logicalPath, func(value map[string]any) any { return value })
	case "oneof":
		var witnesses []mappingWitness
		for _, variantName := range sortedKeys(prop.Variants) {
			variant := prop.Variants[variantName]
			variantPath := logicalPath + "." + variantName
			if variant.Type == "block" {
				for _, witness := range objectWitnesses(variant.Properties, variantPath, func(value map[string]any) any { return value }) {
					witness.covers = append(witness.covers, logicalPath)
					witnesses = append(witnesses, witness)
				}
				continue
			}
			for _, witness := range propertyWitnesses(variant, variantPath) {
				witness.covers = append(witness.covers, logicalPath)
				witnesses = append(witnesses, witness)
			}
		}
		return witnesses
	case "list":
		if prop.Item.Type != "" {
			item := property{Type: prop.Item.Type}
			var witnesses []mappingWitness
			for _, witness := range propertyWitnesses(item, logicalPath+"[]") {
				witness.value = []any{witness.value}
				witness.covers = append(witness.covers, logicalPath)
				witnesses = append(witnesses, witness)
			}
			return witnesses
		}
		witnesses := objectWitnesses(prop.Item.Properties, logicalPath+"[]", func(value map[string]any) any { return []any{value} })
		for index := range witnesses {
			witnesses[index].covers = append(witnesses[index].covers, logicalPath)
		}
		return witnesses
	case "map":
		witnesses := objectWitnesses(prop.Item.Properties, logicalPath+".*", func(value map[string]any) any { return map[string]any{"A": value} })
		for index := range witnesses {
			witnesses[index].covers = append(witnesses[index].covers, logicalPath)
		}
		return witnesses
	default:
		panic("unsupported witness type " + prop.Type)
	}
}

func objectWitnesses(properties map[string]property, logicalPath string, wrap func(map[string]any) any) []mappingWitness {
	base := map[string]any{}
	var baseCoverage []string
	for _, name := range sortedKeys(properties) {
		child := properties[name]
		if !child.Required {
			continue
		}
		witnesses := propertyWitnesses(child, logicalPath+"."+name)
		if len(witnesses) == 0 {
			continue
		}
		setWitnessPath(base, child.Mapping.Path, witnesses[0].value)
		baseCoverage = append(baseCoverage, logicalPath+"."+name)
	}

	var result []mappingWitness
	for _, name := range sortedKeys(properties) {
		child := properties[name]
		for _, childWitness := range propertyWitnesses(child, logicalPath+"."+name) {
			object := cloneWitnessMap(base)
			setWitnessPath(object, child.Mapping.Path, childWitness.value)
			covers := append([]string{logicalPath}, baseCoverage...)
			covers = append(covers, childWitness.covers...)
			result = append(result, mappingWitness{value: wrap(object), covers: uniqueStrings(covers)})
		}
	}
	if len(result) == 0 {
		result = append(result, mappingWitness{value: wrap(base), covers: []string{logicalPath}})
	}
	return result
}

func witnessInteger(prop property) int {
	if prop.Min != nil {
		return *prop.Min
	}
	return 1
}

func setWitnessPath(root map[string]any, path []string, value any) {
	current := root
	for _, segment := range path[:len(path)-1] {
		next, ok := current[segment].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[segment] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

func cloneWitnessMap(source map[string]any) map[string]any {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(err)
	}
	return result
}

func collectExpectedCoverage(prop property, logicalPath string, expected map[string]bool) {
	expected[logicalPath] = true
	if prop.Type == "enum" {
		for _, value := range prop.Values {
			expected[logicalPath+"="+value] = true
		}
	}
	switch prop.Type {
	case "block":
		for _, name := range sortedKeys(prop.Properties) {
			collectExpectedCoverage(prop.Properties[name], logicalPath+"."+name, expected)
		}
	case "oneof":
		for _, name := range sortedKeys(prop.Variants) {
			collectExpectedCoverage(prop.Variants[name], logicalPath+"."+name, expected)
		}
	case "list", "map":
		if prop.Item != nil && prop.Item.Type == "" {
			suffix := "[]"
			if prop.Type == "map" {
				suffix = ".*"
			}
			for _, name := range sortedKeys(prop.Item.Properties) {
				collectExpectedCoverage(prop.Item.Properties[name], logicalPath+suffix+"."+name, expected)
			}
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

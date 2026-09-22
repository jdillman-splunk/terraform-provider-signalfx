// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:generate go run ../chartgen -mode=write -repo-root=../../../..

// Hand-written, not generated - see the *_generated.go files in this same
// package for what dashify/chartgen actually produces. This file contains
// fixed parsing, normalization, validation, and document helpers shared by
// every generated chart.
package charts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type normalizationRule struct {
	Kind   string
	Target []string
	Source []string
	Unit   []string
	Prefix []string
	Suffix []string
}

func float32IsInvalid(value float32) bool {
	converted := float64(value)
	return math.IsNaN(converted) || math.IsInf(converted, 0)
}

func cloneSpecMap(source map[string]any) map[string]any {
	return cloneSpecValue(source).(map[string]any)
}

func cloneSpecValue(source any) any {
	switch value := source.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			result[key] = cloneSpecValue(child)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, child := range value {
			result[index] = cloneSpecValue(child)
		}
		return result
	default:
		return value
	}
}

func semanticallyEqualSpec(left, right map[string]any) bool {
	return semanticallyEqualValue(left, right)
}

func semanticallyEqualValue(left, right any) bool {
	leftNumber, leftIsNumber, leftValid := exactNumber(left)
	rightNumber, rightIsNumber, rightValid := exactNumber(right)
	if leftIsNumber || rightIsNumber {
		return leftIsNumber && rightIsNumber && leftValid && rightValid && leftNumber.Cmp(rightNumber) == 0
	}
	switch leftValue := left.(type) {
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, child := range leftValue {
			rightChild, exists := rightValue[key]
			if !exists || !semanticallyEqualValue(child, rightChild) {
				return false
			}
		}
		return true
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index, child := range leftValue {
			if !semanticallyEqualValue(child, rightValue[index]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left, right)
	}
}

// exactNumber converts all Go representations accepted for JSON numbers to
// an arbitrary-precision rational. This makes semantic equality independent
// of object ordering, decimal spelling, exponent notation, negative zero,
// and the lossy float64 conversion a plain json.Unmarshal would perform.
func exactNumber(raw any) (*big.Rat, bool, bool) {
	var text string
	switch value := raw.(type) {
	case json.Number:
		text = value.String()
		if !json.Valid([]byte(text)) {
			return nil, true, false
		}
	case int:
		text = strconv.FormatInt(int64(value), 10)
	case int8:
		text = strconv.FormatInt(int64(value), 10)
	case int16:
		text = strconv.FormatInt(int64(value), 10)
	case int32:
		text = strconv.FormatInt(int64(value), 10)
	case int64:
		text = strconv.FormatInt(value, 10)
	case uint:
		text = strconv.FormatUint(uint64(value), 10)
	case uint8:
		text = strconv.FormatUint(uint64(value), 10)
	case uint16:
		text = strconv.FormatUint(uint64(value), 10)
	case uint32:
		text = strconv.FormatUint(uint64(value), 10)
	case uint64:
		text = strconv.FormatUint(value, 10)
	case float32:
		if float32IsInvalid(value) {
			return nil, true, false
		}
		text = strconv.FormatFloat(float64(value), 'g', -1, 32)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, true, false
		}
		text = strconv.FormatFloat(value, 'g', -1, 64)
	default:
		return nil, false, false
	}
	number, ok := new(big.Rat).SetString(text)
	return number, true, ok
}

func applyNormalizations(root map[string]any, rules []normalizationRule) ([]string, error) {
	var applied []string
	for _, rule := range rules {
		var ruleErr error
		visitPatternParents(root, rule.Target, func(parent map[string]any, key string, bindings []string) {
			if ruleErr != nil {
				return
			}
			switch rule.Kind {
			case "fallback":
				sourceParent, sourceKey, ok := resolvePatternParent(root, rule.Source, bindings)
				if !ok {
					return
				}
				sourceValue, sourceSet := sourceParent[sourceKey]
				if !sourceSet {
					return
				}
				if sourceValue == nil {
					return
				}
				if _, targetSet := parent[key]; !targetSet {
					parent[key] = sourceValue
				}
				delete(sourceParent, sourceKey)
				applied = append(applied, strings.Join(rule.Source, ".")+" -> "+strings.Join(rule.Target, "."))
			case "wrap_scalar_array":
				value, ok := parent[key]
				if !ok || value == nil {
					return
				}
				if _, alreadyArray := value.([]any); alreadyArray {
					return
				}
				parent[key] = []any{value}
				applied = append(applied, strings.Join(rule.Target, ".")+" scalar -> one-item array")
			case "display_unit":
				changed, err := normalizeDisplayUnit(root, parent, key, bindings, rule)
				if err != nil {
					ruleErr = err
					return
				}
				if changed {
					applied = append(applied, "deprecated display unit -> "+strings.Join(rule.Target, "."))
				}
			case "single_value_publish_label_options":
				changed, err := normalizeSingleValueDisplayUnit(root, parent, key, bindings, rule)
				if err != nil {
					ruleErr = err
					return
				}
				if changed {
					applied = append(applied, "publishLabelOptions[0] -> "+strings.Join(rule.Target, "."))
				}
			default:
				ruleErr = fmt.Errorf("unsupported generated normalization kind %q", rule.Kind)
			}
		})
		if ruleErr != nil {
			return nil, ruleErr
		}
	}
	sort.Strings(applied)
	return applied, nil
}

func visitPatternParents(root map[string]any, pattern []string, visit func(map[string]any, string, []string)) {
	if len(pattern) == 0 {
		return
	}
	var walk func(any, int, []string)
	walk = func(current any, index int, bindings []string) {
		if index == len(pattern)-1 {
			if object, ok := current.(map[string]any); ok {
				visit(object, pattern[index], append([]string(nil), bindings...))
			}
			return
		}
		segment := pattern[index]
		if segment == "*" {
			switch object := current.(type) {
			case map[string]any:
				for _, key := range sortedMapKeys(object) {
					walk(object[key], index+1, append(bindings, key))
				}
			case []any:
				for itemIndex, child := range object {
					walk(child, index+1, append(bindings, strconv.Itoa(itemIndex)))
				}
			}
			return
		}
		object, ok := current.(map[string]any)
		if !ok {
			return
		}
		child, ok := object[segment]
		if !ok {
			return
		}
		walk(child, index+1, bindings)
	}
	walk(root, 0, nil)
}

func resolvePatternParent(root map[string]any, pattern, bindings []string) (map[string]any, string, bool) {
	if len(pattern) == 0 {
		return nil, "", false
	}
	var current any = root
	bindingIndex := 0
	for _, rawSegment := range pattern[:len(pattern)-1] {
		segment := rawSegment
		if segment == "*" {
			if bindingIndex >= len(bindings) {
				return nil, "", false
			}
			segment = bindings[bindingIndex]
			bindingIndex++
		}
		switch container := current.(type) {
		case map[string]any:
			child, ok := container[segment]
			if !ok {
				return nil, "", false
			}
			current = child
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(container) {
				return nil, "", false
			}
			current = container[index]
		default:
			return nil, "", false
		}
	}
	parent, ok := current.(map[string]any)
	return parent, pattern[len(pattern)-1], ok
}

func normalizeDisplayUnit(root map[string]any, targetParent map[string]any, targetKey string, bindings []string, rule normalizationRule) (bool, error) {
	type legacyValue struct {
		parent map[string]any
		key    string
		value  any
		set    bool
	}
	lookup := func(pattern []string) legacyValue {
		parent, key, ok := resolvePatternParent(root, pattern, bindings)
		if !ok {
			return legacyValue{}
		}
		value, set := parent[key]
		return legacyValue{parent: parent, key: key, value: value, set: set}
	}
	unit, prefix, suffix := lookup(rule.Unit), lookup(rule.Prefix), lookup(rule.Suffix)
	if !unit.set && !prefix.set && !suffix.set {
		return false, nil
	}
	// Modern fields have explicit precedence. Once displayUnit is present the
	// deprecated values are semantically ignored, so even malformed legacy
	// values can be removed without interpreting them.
	if _, modernSet := targetParent[targetKey]; modernSet {
		for _, legacy := range []legacyValue{unit, prefix, suffix} {
			if legacy.set {
				delete(legacy.parent, legacy.key)
			}
		}
		return true, nil
	}
	for name, legacy := range map[string]legacyValue{"unit": unit, "prefix": prefix, "suffix": suffix} {
		if legacy.set && legacy.value != nil {
			if _, ok := legacy.value.(string); !ok {
				return false, fmt.Errorf("deprecated display unit %s is %T, want string or null", name, legacy.value)
			}
		}
	}
	unitValue, unitIsString := unit.value.(string)
	prefixValue, prefixIsString := prefix.value.(string)
	suffixValue, suffixIsString := suffix.value.(string)
	if unit.set && unitIsString && unitValue != "" &&
		((prefix.set && prefixIsString && prefixValue != "") || (suffix.set && suffixIsString && suffixValue != "")) {
		// A named unit and custom affixes are different variants of modern
		// displayUnit. There is no lossless precedence rule for legacy input
		// that sets both, so leave every key untouched for raw preservation.
		return false, nil
	}
	if unit.set && unitIsString && unitValue != "" {
		targetParent[targetKey] = unitValue
	} else {
		custom := map[string]any{}
		if prefix.set && prefixIsString && prefix.value != nil {
			custom["prefix"] = prefixValue
		}
		if suffix.set && suffixIsString && suffix.value != nil {
			custom["suffix"] = suffixValue
		}
		if prefixValue != "" || suffixValue != "" {
			targetParent[targetKey] = custom
		}
	}
	for _, legacy := range []legacyValue{unit, prefix, suffix} {
		if legacy.set {
			delete(legacy.parent, legacy.key)
		}
	}
	return true, nil
}

func normalizeSingleValueDisplayUnit(root map[string]any, targetParent map[string]any, targetKey string, bindings []string, rule normalizationRule) (bool, error) {
	sourceParent, sourceKey, ok := resolvePatternParent(root, rule.Source, bindings)
	if !ok {
		return false, nil
	}
	raw, sourceSet := sourceParent[sourceKey]
	if !sourceSet {
		return false, nil
	}
	if _, modernSet := targetParent[targetKey]; modernSet {
		delete(sourceParent, sourceKey)
		return true, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return false, fmt.Errorf("publishLabelOptions is %T, want an array", raw)
	}
	if len(items) == 0 {
		delete(sourceParent, sourceKey)
		return true, nil
	}
	if len(items) != 1 {
		return false, nil
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		return false, fmt.Errorf("publishLabelOptions[0] is %T, want an object", items[0])
	}
	for key := range item {
		if key != "valueUnit" && key != "valuePrefix" && key != "valueSuffix" {
			return false, nil
		}
	}
	temporary := map[string]any{"legacy": cloneSpecMap(item)}
	displayRule := normalizationRule{Target: []string{"displayUnit"}, Unit: []string{"legacy", "valueUnit"}, Prefix: []string{"legacy", "valuePrefix"}, Suffix: []string{"legacy", "valueSuffix"}}
	changed, err := normalizeDisplayUnit(temporary, targetParent, targetKey, nil, displayRule)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	delete(sourceParent, sourceKey)
	return true, nil
}

func validationPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	if field == "" {
		return prefix
	}
	return prefix + "." + field
}

func validateString(path string, value types.String, required bool, minLength int, allowed ...string) []ValidationError {
	if value.IsUnknown() {
		return nil
	}
	if value.IsNull() {
		if required {
			return []ValidationError{{Path: path, Message: "must be set"}}
		}
		return nil
	}
	if len(value.ValueString()) < minLength {
		return []ValidationError{{Path: path, Message: fmt.Sprintf("must contain at least %d character(s)", minLength)}}
	}
	if len(allowed) > 0 {
		for _, candidate := range allowed {
			if value.ValueString() == candidate {
				return nil
			}
		}
		return []ValidationError{{Path: path, Message: fmt.Sprintf("must be one of %s", strings.Join(allowed, ", "))}}
	}
	return nil
}

func validateRelativeDuration(path string, value types.String) []ValidationError {
	if value.IsUnknown() {
		return nil
	}
	if value.IsNull() {
		return nil
	}
	if _, ok := relativeDurationMillis(value.ValueString()); !ok {
		return []ValidationError{{Path: path, Message: "must be a positive whole-number duration such as 15m or 1h that fits in milliseconds"}}
	}
	return nil
}

func validateInt64(path string, value types.Int64, minimum, maximum *int64) []ValidationError {
	if value.IsUnknown() {
		return nil
	}
	if value.IsNull() {
		return nil
	}
	if minimum != nil && value.ValueInt64() < *minimum {
		return []ValidationError{{Path: path, Message: fmt.Sprintf("must be at least %d", *minimum)}}
	}
	if maximum != nil && value.ValueInt64() > *maximum {
		return []ValidationError{{Path: path, Message: fmt.Sprintf("must be at most %d", *maximum)}}
	}
	return nil
}

func validateBool(path string, value types.Bool, required bool) []ValidationError {
	if !value.IsUnknown() && value.IsNull() && required {
		return []ValidationError{{Path: path, Message: "must be set"}}
	}
	return nil
}

func validateFloat64(string, types.Float64) []ValidationError {
	return nil
}

func int64Pointer(value int64) *int64 { return &value }

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Content is implemented by every generated chart-content model
// (SingleValueModel, TimeSeriesModel, ...) - see observability/chartgen. Dropping
// a new charts/schemas/*.yml file and regenerating is enough to make its
// model satisfy this automatically; nothing here needs to change.
type Content interface {
	BuildSpec() map[string]any
	MissingRequiredFields() []string
	ValidationErrors() []ValidationError
}

// Entry pairs a set chart-content block with the tfsdk name used to build
// diagnostic paths against it.
type Entry struct {
	Name    string
	Content Content
}

// ValidationError is a chart-relative validation failure.
type ValidationError struct {
	Path    string
	Message string
}

// ParseMetadata explains whether a recognized stored chart can be represented
// by typed Terraform state without changing its meaning.
type ParseMetadata struct {
	ElementTag            string
	Leftovers             []string
	MissingRequiredFields []string
	ValidationErrors      []ValidationError
	Normalizations        []string
	Lossless              bool
}

// TypedSafe is the import/read gate for selecting a generated chart model.
func (m ParseMetadata) TypedSafe() bool { return m.Lossless }

// setPath is called by every generated chart's BuildSpec method - the
// runtime counterpart to a charts/schemas/*.yml property's `mapping.path`.
// It walks/creates nested maps along path and sets the leaf value.
func setPath(root map[string]any, path []string, value any) {
	m := root
	for _, seg := range path[:len(path)-1] {
		next, ok := m[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[seg] = next
		}
		m = next
	}
	m[path[len(path)-1]] = value
}

// --- inverse: stored spec back into a model --------------------------------
//
// TakeString/TakeBool/TakeInt64 are called by every generated chart's
// ParseSpec method, the inverse of BuildSpec. Each *removes* the value at
// mapping.path rather than just reading it, which is what makes unrepresented
// content detectable: whatever is still in the document once every parser has
// run is content the typed schema does not model, and Leftovers reports it.
// A path that isn't present becomes a null Terraform value, so an optional
// field left out of the document round-trips back to "unset" rather than to a
// zero value that would diff forever.
//
// They're exported, unlike setPath, because the parent package needs them for
// the parts of a dashboard that aren't schema-driven - layout in particular.

// ValueShape says how a property sits in the document: as the value itself, or
// wrapped in a single-element array (a charts/schemas/*.yml property with
// `mapping.wrap: array`). The generated parsers pass the shape their schema
// declares, so a list arriving where the schema says scalar is reported rather
// than quietly unwrapped and rewritten in the other form on the next apply.
type ValueShape bool

const (
	Scalar         ValueShape = false
	WrappedInArray ValueShape = true
)

// takeLeaf removes and returns the value at path. A path that's present but
// null decodes as an unset Terraform value. ParseContent still compares the
// rebuilt document with the untouched normalized input, so explicit null is
// not declared lossless or admitted to typed state unless a normalization
// explicitly authorizes it.
func takeLeaf(root map[string]any, path []string) (any, bool) {
	m := root
	for _, seg := range path[:len(path)-1] {
		next, ok := m[seg].(map[string]any)
		if !ok {
			return nil, false
		}
		m = next
	}

	leaf := path[len(path)-1]
	value, ok := m[leaf]
	if !ok {
		return nil, false
	}
	delete(m, leaf)

	return value, value != nil
}

// unwrap collapses the single-element array a WrappedInArray property
// serializes to. A multi-element array has no single-value Terraform
// representation, so it's reported instead of silently truncated to its first
// element. A wrapped property carrying a bare value is accepted and written
// back in its declared array form - the two forms cannot both round-trip, and
// following the schema is the more predictable of the two.
func unwrap(path []string, shape ValueShape, value any) (any, error) {
	array, ok := value.([]any)
	if !ok {
		return value, nil
	}
	if shape != WrappedInArray {
		return nil, fmt.Errorf("%s holds a list, which this single-value field cannot represent", strings.Join(path, "."))
	}
	if len(array) != 1 {
		return nil, fmt.Errorf("%s holds %d values; this field has a single-value Terraform representation only", strings.Join(path, "."), len(array))
	}

	return array[0], nil
}

// take is TakeString/TakeBool/TakeInt64's shared engine: remove the value at
// path, unwrap it per shape, then hand the raw wire value to convert for the
// one part that's actually type-specific. null is the zero value each
// wrapper returns for "absent" or "wrong type", since a generic function
// can't call e.g. types.StringNull() without knowing T is types.String.
func take[T any](root map[string]any, path []string, shape ValueShape, null T, convert func(raw any) (T, error)) (T, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return null, nil
	}

	raw, err := unwrap(path, shape, raw)
	if err != nil {
		return null, err
	}

	return convert(raw)
}

func TakeString(root map[string]any, path []string, shape ValueShape) (types.String, error) {
	return take(root, path, shape, types.StringNull(), func(raw any) (types.String, error) {
		value, ok := raw.(string)
		if !ok {
			return types.StringNull(), typeError(path, "a string", raw)
		}
		return types.StringValue(value), nil
	})
}

func TakeBool(root map[string]any, path []string, shape ValueShape) (types.Bool, error) {
	return take(root, path, shape, types.BoolNull(), func(raw any) (types.Bool, error) {
		value, ok := raw.(bool)
		if !ok {
			return types.BoolNull(), typeError(path, "true or false", raw)
		}
		return types.BoolValue(value), nil
	})
}

// TakeInt64 accepts both wire and in-process numbers: a document decoded from
// JSON holds every number as a float64, while one just built by BuildSpec
// holds the int64 it was given. A fractional value is reported rather than
// truncated - charts/schemas' single `number` type maps to Int64 here, so a
// genuinely fractional field (a threshold bound, an axis min) needs a schema
// change, not a lossy read.
func TakeInt64(root map[string]any, path []string, shape ValueShape) (types.Int64, error) {
	return take(root, path, shape, types.Int64Null(), func(raw any) (types.Int64, error) {
		value, err := jsonInt64(raw)
		if err != nil {
			return types.Int64Null(), fmt.Errorf("%s: %w", strings.Join(path, "."), err)
		}
		return types.Int64Value(value), nil
	})
}

// TakeFloat64 accepts both wire and in-process numbers, the same as
// TakeInt64, but keeps any fractional part instead of rejecting it -
// charts/schemas' `float` type maps to Float64 here, for fields (a
// threshold bound, a palette range) that are genuinely fractional in real
// documents.
func TakeFloat64(root map[string]any, path []string, shape ValueShape) (types.Float64, error) {
	return take(root, path, shape, types.Float64Null(), func(raw any) (types.Float64, error) {
		value, err := jsonFloat64(raw)
		if err != nil {
			return types.Float64Null(), fmt.Errorf("%s: %w", strings.Join(path, "."), err)
		}
		return types.Float64Value(value), nil
	})
}

func TakeStringList(root map[string]any, path []string) ([]types.String, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, typeError(path, "a list of strings", raw)
	}
	out := make([]types.String, len(values))
	for i, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok {
			return nil, typeError(append(path, strconv.Itoa(i)), "a string", rawValue)
		}
		out[i] = types.StringValue(value)
	}
	return out, nil
}

func TakeObjectList[T any](root map[string]any, path []string, parse func(map[string]any) (T, error)) ([]T, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, typeError(path, "a list of objects", raw)
	}
	out := make([]T, len(values))
	for i, rawValue := range values {
		value, ok := rawValue.(map[string]any)
		if !ok {
			return nil, typeError(append(path, strconv.Itoa(i)), "an object", rawValue)
		}
		parsed, err := parse(value)
		if err != nil {
			return nil, fmt.Errorf("%s.%d: %w", strings.Join(path, "."), i, err)
		}
		if leftovers := Leftovers("", value); len(leftovers) > 0 {
			return nil, fmt.Errorf("%s.%d contains unsupported fields: %s", strings.Join(path, "."), i, strings.Join(leftovers, ", "))
		}
		out[i] = parsed
	}
	return out, nil
}

func TakeObjectMap[T any](root map[string]any, path []string, parse func(map[string]any) (T, error)) (map[string]T, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, typeError(path, "an object map", raw)
	}
	out := make(map[string]T, len(values))
	for key, rawValue := range values {
		value, ok := rawValue.(map[string]any)
		if !ok {
			return nil, typeError(append(path, key), "an object", rawValue)
		}
		parsed, err := parse(value)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", strings.Join(path, "."), key, err)
		}
		if leftovers := Leftovers("", value); len(leftovers) > 0 {
			return nil, fmt.Errorf("%s.%s contains unsupported fields: %s", strings.Join(path, "."), key, strings.Join(leftovers, ", "))
		}
		out[key] = parsed
	}
	return out, nil
}

func TakeObject[T any](root map[string]any, path []string, parse func(map[string]any) (T, error)) (*T, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return nil, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return nil, typeError(path, "an object", raw)
	}
	parsed, err := parse(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(path, "."), err)
	}
	if leftovers := Leftovers("", value); len(leftovers) > 0 {
		return nil, fmt.Errorf("%s contains unsupported fields: %s", strings.Join(path, "."), strings.Join(leftovers, ", "))
	}
	return &parsed, nil
}

// TakeOneOf removes the value at path and hands its raw wire shape - a bare
// scalar or an object, whichever the active variant serialized - to parse,
// which recovers the variant itself (see observability/chartgen's generated
// oneof parse functions). Unlike TakeObject, a missing path is simply
// absent rather than a type error, since either shape is valid; when the
// raw value is an object, parse is expected to remove every key it
// recognizes so the Leftovers check below reports only what no variant
// claimed.
func TakeOneOf[T any](root map[string]any, path []string, parse func(raw any) (T, error)) (*T, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return nil, nil
	}

	parsed, err := parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(path, "."), err)
	}

	if obj, ok := raw.(map[string]any); ok {
		if leftovers := Leftovers("", obj); len(leftovers) > 0 {
			return nil, fmt.Errorf("%s contains unsupported fields: %s", strings.Join(path, "."), strings.Join(leftovers, ", "))
		}
	}

	return &parsed, nil
}

func RelativeDurationSpec(value string) (map[string]any, bool) {
	milliseconds, ok := relativeDurationMillis(value)
	if !ok {
		return nil, false
	}
	return map[string]any{"type": "relative", "range": milliseconds, "rangeEnd": int64(0)}, true
}

// RelativeDurationSemanticEqualityModifier preserves the configured spelling
// of a duration when the API returns an equivalent canonical value. Dashify
// stores milliseconds, so values such as 60m and 1h otherwise oscillate
// between configuration and state forever.
type RelativeDurationSemanticEqualityModifier struct{}

func (m RelativeDurationSemanticEqualityModifier) Description(_ context.Context) string {
	return "Treats durations with the same millisecond value as equal."
}

func (m RelativeDurationSemanticEqualityModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m RelativeDurationSemanticEqualityModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	stateMillis, stateOK := relativeDurationMillis(req.StateValue.ValueString())
	configMillis, configOK := relativeDurationMillis(req.ConfigValue.ValueString())
	if stateOK && configOK && stateMillis == configMillis {
		resp.PlanValue = req.StateValue
	}
}

func relativeDurationMillis(value string) (int64, bool) {
	if len(value) < 2 {
		return 0, false
	}
	number, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	if err != nil || number <= 0 {
		return 0, false
	}

	var multiplier int64
	switch value[len(value)-1:] {
	case "s":
		multiplier = 1000
	case "m":
		multiplier = 60 * 1000
	case "h":
		multiplier = 60 * 60 * 1000
	case "d":
		multiplier = 24 * 60 * 60 * 1000
	case "w":
		multiplier = 7 * 24 * 60 * 60 * 1000
	default:
		return 0, false
	}
	if number > math.MaxInt64/multiplier {
		return 0, false
	}
	return number * multiplier, true
}

func TakeRelativeDuration(root map[string]any, path []string) (types.String, error) {
	raw, ok := takeLeaf(root, path)
	if !ok {
		return types.StringNull(), nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return types.StringNull(), typeError(path, "a relative time object", raw)
	}
	typeValue, ok := value["type"].(string)
	if !ok || typeValue != "relative" {
		return types.StringNull(), fmt.Errorf("%s.type is %v, want relative", strings.Join(path, "."), value["type"])
	}
	rangeValue, err := jsonInt64(value["range"])
	if err != nil {
		return types.StringNull(), fmt.Errorf("%s.range: %w", strings.Join(path, "."), err)
	}
	rangeEnd, err := jsonInt64(value["rangeEnd"])
	if err != nil || rangeEnd != 0 {
		return types.StringNull(), fmt.Errorf("%s.rangeEnd is %v, want 0", strings.Join(path, "."), value["rangeEnd"])
	}
	if rangeValue <= 0 {
		return types.StringNull(), fmt.Errorf("%s.range is %dms, want a positive duration", strings.Join(path, "."), rangeValue)
	}
	delete(value, "type")
	delete(value, "range")
	delete(value, "rangeEnd")
	units := []struct {
		suffix string
		millis int64
	}{{"w", 7 * 24 * 60 * 60 * 1000}, {"d", 24 * 60 * 60 * 1000}, {"h", 60 * 60 * 1000}, {"m", 60 * 1000}, {"s", 1000}}
	for _, unit := range units {
		if rangeValue%unit.millis == 0 {
			return types.StringValue(strconv.FormatInt(rangeValue/unit.millis, 10) + unit.suffix), nil
		}
	}
	return types.StringNull(), fmt.Errorf("%s.range is %dms, which cannot be represented as whole seconds", strings.Join(path, "."), rangeValue)
}

func jsonInt64(raw any) (int64, error) {
	number, numeric, valid := exactNumber(raw)
	if !numeric || !valid {
		return 0, fmt.Errorf("%v (%T) is not a number", raw, raw)
	}
	if !number.IsInt() {
		return 0, fmt.Errorf("%v is not a whole number", raw)
	}
	integer := number.Num()
	if !integer.IsInt64() {
		return 0, fmt.Errorf("%v is outside the int64 range", raw)
	}
	return integer.Int64(), nil
}

func jsonFloat64(raw any) (float64, error) {
	if value, ok := raw.(float64); ok {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("%v is not a finite number", raw)
		}
		return value, nil
	}
	number, numeric, valid := exactNumber(raw)
	if !numeric || !valid {
		return 0, fmt.Errorf("%v (%T) is not a number", raw, raw)
	}
	value, err := strconv.ParseFloat(number.FloatString(400), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%v cannot be represented as a finite float64", raw)
	}
	roundTrip, _, roundTripValid := exactNumber(value)
	if !roundTripValid || roundTrip.Cmp(number) != 0 {
		return 0, fmt.Errorf("%v cannot round-trip through float64 without changing value", raw)
	}
	return value, nil
}

func typeError(path []string, want string, got any) error {
	return fmt.Errorf("%s holds %v (%T), want %s", strings.Join(path, "."), got, got, want)
}

// TakeElement removes an element tag whose argument list is empty, which is
// the only form BuildSpec produces. A tag carrying arguments is deliberately
// left in place: nothing in the typed schema represents element arguments, so
// Leftovers should report it rather than have it vanish.
func TakeElement(node map[string]any, tag string) {
	if args, ok := node[tag].([]any); ok && len(args) == 0 {
		delete(node, tag)
	}
}

// Leftovers lists the dotted paths under node that still hold a value once
// every parser has removed what it claims, each prefixed with prefix. Empty
// maps and arrays, and explicit nulls, are not reported: the standard
// chart/datasource/widget containers are always present and are empty once
// their leaves are taken.
// A non-empty array is reported as one path rather than per element, since an
// array Terraform doesn't model is unrepresented as a whole.
func Leftovers(prefix string, node map[string]any) []string {
	var out []string
	collectLeftovers(prefix, node, &out)
	sort.Strings(out)

	return out
}

func collectLeftovers(prefix string, node map[string]any, out *[]string) {
	for key, value := range node {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		switch typed := value.(type) {
		case nil:
			// An explicit null holds nothing, and the write path omits it
			// rather than writing null - the same reading takeLeaf gives it.
		case map[string]any:
			collectLeftovers(path, typed, out)
		case []any:
			if len(typed) > 0 {
				*out = append(*out, path)
			}
		default:
			*out = append(*out, path)
		}
	}
}

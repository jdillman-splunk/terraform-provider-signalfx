// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	generatedcharts "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/observability/charts"
	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/observability/charts/schemas/elements"
)

// TestEveryGeneratedMappingHasAnOllyValidatedWitness independently checks the
// generation-time descriptor gate against a complete Draft 2020-12 compiler.
func TestEveryGeneratedMappingHasAnOllyValidatedWitness(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	schemasDirectory := filepath.Join(repository, "internal", "framework", "observability", "charts", "schemas")
	compiled := compileOllyContracts(t)

	seenTransforms := map[string]bool{}
	seenTypes := map[string]bool{}
	for _, filename := range expectedChartFiles {
		path := filepath.Join(schemasDirectory, filename)
		resolved, err := loadSchema(schemasDirectory, path)
		if err != nil {
			t.Fatal(err)
		}
		tag := "<" + resolved.Dashify.Element.Name + ">"
		contract := compiled[tag]
		if contract == nil {
			t.Fatalf("no Olly contract for %s", tag)
		}

		expected := map[string]bool{}
		covered := map[string]bool{}
		for _, name := range sortedKeys(resolved.Properties) {
			prop := resolved.Properties[name]
			if !isGeneratedProperty(prop) {
				continue
			}
			collectExpectedCoverage(prop, name, expected)
			collectWitnessFeatures(prop, seenTypes, seenTransforms)
			for _, witness := range propertyWitnesses(prop, name) {
				properties := minimalContractProperties(tag)
				addWitnessDependencies(resolved.Properties, name, prop, witness, properties)
				value := witness.value
				if prop.Mapping.Wrap == "array" {
					value = []any{value}
				}
				setWitnessPath(properties, prop.Mapping.Path, value)
				if err := contract.Validate(properties); err != nil {
					t.Errorf("%s %s witness rejected by Olly: %v\nproperties: %#v", filename, strings.Join(witness.covers, ", "), err, properties)
					continue
				}
				spec := cloneWitnessMap(properties)
				spec[tag] = []any{}
				entry, metadata, parseErr := generatedcharts.ParseContent(tag, spec)
				if parseErr != nil {
					t.Errorf("%s %s witness did not parse through generated code: %v", filename, strings.Join(witness.covers, ", "), parseErr)
					continue
				}
				if !metadata.TypedSafe() {
					t.Errorf("%s %s witness was not a lossless generated BuildSpec/Parse round trip: %+v", filename, strings.Join(witness.covers, ", "), metadata)
					continue
				}
				if entry.Content == nil || entry.Content.BuildSpec() == nil {
					t.Errorf("%s %s witness produced no generated content", filename, strings.Join(witness.covers, ", "))
					continue
				}
				for _, label := range witness.covers {
					covered[label] = true
				}
			}
		}
		for _, label := range sortedBoolKeys(expected) {
			if !covered[label] {
				t.Errorf("%s has no validated witness for %s", filename, label)
			}
		}
	}

	for _, propertyType := range []string{"string", "bool", "number", "float", "enum", "list", "map", "block", "oneof"} {
		if !seenTypes[propertyType] {
			t.Errorf("witness suite never exercised %s", propertyType)
		}
	}
	for _, transform := range []string{"negate", "relative_duration", "wrap:array"} {
		if !seenTransforms[transform] {
			t.Errorf("witness suite never exercised %s", transform)
		}
	}
}

func TestEveryDeclaredLegacyNormalizationUsesGeneratedParseAndBuild(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	schemasDirectory := filepath.Join(repository, "internal", "framework", "observability", "charts", "schemas")
	var infos []chartInfo
	resolvedSchemas := map[string]*chartSchema{}
	for _, filename := range expectedChartFiles {
		resolved, err := loadSchema(schemasDirectory, filepath.Join(schemasDirectory, filename))
		if err != nil {
			t.Fatal(err)
		}
		tag := "<" + resolved.Dashify.Element.Name + ">"
		resolvedSchemas[tag] = resolved
		infos = append(infos, chartInfo{Type: resolved.Type, ElementTag: tag})
	}
	contracts, err := loadContracts(filepath.Join(schemasDirectory, "elements"), infos)
	if err != nil {
		t.Fatal(err)
	}

	cases := 0
	for _, info := range infos {
		resolved := resolvedSchemas[info.ElementTag]
		contract := contracts[info.ElementTag]
		for ruleIndex, rule := range resolved.Normalizations {
			legacyCases := normalizationWitnessCases(rule, contract)
			if len(legacyCases) == 0 {
				t.Errorf("%s normalization %d (%s) produced no witness cases", resolved.Type, ruleIndex, rule.Kind)
				continue
			}
			for _, legacyCase := range legacyCases {
				cases++
				name := fmt.Sprintf("%s/%02d_%s/%s", resolved.Type, ruleIndex, rule.Kind, legacyCase.name)
				t.Run(name, func(t *testing.T) {
					properties := minimalContractProperties(info.ElementTag)
					fragment, ok := contractPathFragment(contract, legacyCase.path, legacyCase.value)
					if !ok {
						t.Fatalf("cannot construct source path %s", strings.Join(legacyCase.path, "."))
					}
					mergeWitnessDocument(properties, fragment.(map[string]any))
					properties[info.ElementTag] = []any{}
					entry, metadata, parseErr := generatedcharts.ParseContent(info.ElementTag, properties)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					if !metadata.TypedSafe() || len(metadata.Normalizations) == 0 {
						t.Fatalf("normalization was not typed-safe: %+v", metadata)
					}
					if !contractPatternPresent(entry.Content.BuildSpec(), rule.Target) {
						t.Fatalf("canonical target %s is absent from %#v", strings.Join(rule.Target, "."), entry.Content.BuildSpec())
					}

					malformed := minimalContractProperties(info.ElementTag)
					malformedFragment, ok := contractPathFragment(contract, legacyCase.path, true)
					if !ok {
						t.Fatalf("cannot construct malformed source path %s", strings.Join(legacyCase.path, "."))
					}
					mergeWitnessDocument(malformed, malformedFragment.(map[string]any))
					malformed[info.ElementTag] = []any{}
					_, malformedMetadata, malformedErr := generatedcharts.ParseContent(info.ElementTag, malformed)
					if malformedErr == nil && malformedMetadata.TypedSafe() {
						t.Fatalf("malformed legacy source became typed: %+v", malformedMetadata)
					}
				})
			}
		}
	}
	if cases < 18 {
		t.Fatalf("only %d normalization source witnesses ran; expected every rule and wildcard unit/prefix/suffix source", cases)
	}
}

type normalizationWitnessCase struct {
	name  string
	path  []string
	value any
}

func normalizationWitnessCases(rule normalization, contract *contractNode) []normalizationWitnessCase {
	valueFor := func(path []string) any {
		node, _ := contractNodeAtPath(contract, path)
		for _, value := range simpleContractWitnesses(node) {
			if value != nil {
				return value
			}
		}
		return "value"
	}
	switch rule.Kind {
	case "fallback":
		return []normalizationWitnessCase{{"source", rule.Source, valueFor(rule.Source)}}
	case "wrap_scalar_array":
		return []normalizationWitnessCase{{"scalar", rule.Target, "service.name"}}
	case "display_unit":
		return []normalizationWitnessCase{
			{"unit", rule.Unit, valueFor(rule.Unit)},
			{"prefix", rule.Prefix, "prefix"},
			{"suffix", rule.Suffix, "suffix"},
		}
	case "single_value_publish_label_options":
		return []normalizationWitnessCase{{"source", rule.Source, []any{map[string]any{"valueSuffix": "%"}}}}
	default:
		return nil
	}
}

func contractPathFragment(node *contractNode, path []string, value any) (any, bool) {
	if len(path) == 0 {
		return value, true
	}
	if len(node.AnyOf) > 0 {
		for _, alternative := range node.AnyOf {
			if fragment, ok := contractPathFragment(alternative, path, value); ok {
				return fragment, true
			}
		}
		return nil, false
	}
	segment := path[0]
	if segment == "*" {
		if node.Additional != nil {
			child, ok := contractPathFragment(node.Additional, path[1:], value)
			if childObject, objectOK := child.(map[string]any); objectOK {
				fillRequiredContractProperties(childObject, node.Additional)
			}
			return map[string]any{"A": child}, ok
		}
		if node.Items != nil {
			child, ok := contractPathFragment(node.Items, path[1:], value)
			if childObject, objectOK := child.(map[string]any); objectOK {
				fillRequiredContractProperties(childObject, node.Items)
			}
			return []any{child}, ok
		}
		return nil, false
	}
	if node.Properties == nil || node.Properties[segment] == nil {
		return nil, false
	}
	child, ok := contractPathFragment(node.Properties[segment], path[1:], value)
	if !ok {
		return nil, false
	}
	return map[string]any{segment: child}, true
}

func fillRequiredContractProperties(object map[string]any, node *contractNode) {
	for _, name := range node.Required {
		if _, exists := object[name]; exists || node.Properties[name] == nil {
			continue
		}
		for _, witness := range simpleContractWitnesses(node.Properties[name]) {
			if witness != nil {
				object[name] = witness
				break
			}
		}
	}
}

func mergeWitnessDocument(target, source map[string]any) {
	for key, value := range source {
		if sourceObject, ok := value.(map[string]any); ok {
			if targetObject, exists := target[key].(map[string]any); exists {
				mergeWitnessDocument(targetObject, sourceObject)
				continue
			}
		}
		target[key] = value
	}
}

func contractPatternPresent(root map[string]any, path []string) bool {
	var current any = root
	for _, segment := range path {
		switch container := current.(type) {
		case map[string]any:
			if segment == "*" {
				keys := sortedAnyKeys(container)
				if len(keys) == 0 {
					return false
				}
				current = container[keys[0]]
				continue
			}
			var ok bool
			current, ok = container[segment]
			if !ok {
				return false
			}
		case []any:
			if segment != "*" || len(container) == 0 {
				return false
			}
			current = container[0]
		default:
			return false
		}
	}
	return true
}

func addWitnessDependencies(properties map[string]property, name string, prop property, witness mappingWitness, document map[string]any) {
	if prop.RequiresValue != nil {
		if target, ok := properties[prop.RequiresValue.Field]; ok {
			setWitnessPath(document, target.Mapping.Path, prop.RequiresValue.Value)
		}
	}
	for _, candidateName := range sortedKeys(properties) {
		candidate := properties[candidateName]
		if candidate.RequiredWhen == nil || candidate.RequiredWhen.Field != name {
			continue
		}
		matches := false
		for _, valueWitness := range propertyWitnesses(prop, name) {
			if strings.Contains(strings.Join(valueWitness.covers, ","), name+"="+candidate.RequiredWhen.Value) && strings.Join(valueWitness.covers, ",") == strings.Join(witness.covers, ",") {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		candidateWitnesses := propertyWitnesses(candidate, candidateName)
		if len(candidateWitnesses) > 0 {
			setWitnessPath(document, candidate.Mapping.Path, candidateWitnesses[0].value)
		}
	}
}

func compileOllyContracts(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	result := make(map[string]*jsonschema.Schema, len(elements.Catalog))
	for _, definition := range elements.Catalog {
		contents, err := elements.Read(definition)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if unmarshalErr := json.Unmarshal(contents, &document); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		if resourceErr := compiler.AddResource(definition.SchemaID, document); resourceErr != nil {
			t.Fatal(resourceErr)
		}
		contract, err := compiler.Compile(definition.SchemaID)
		if err != nil {
			t.Fatal(err)
		}
		result[definition.Element] = contract
	}
	return result
}

func collectWitnessFeatures(prop property, propertyTypes, transforms map[string]bool) {
	propertyTypes[prop.Type] = true
	if prop.Mapping.Transform != "" {
		transforms[prop.Mapping.Transform] = true
	}
	if prop.Mapping.Wrap != "" {
		transforms["wrap:"+prop.Mapping.Wrap] = true
	}
	switch prop.Type {
	case "block":
		for _, child := range prop.Properties {
			collectWitnessFeatures(child, propertyTypes, transforms)
		}
	case "oneof":
		for _, child := range prop.Variants {
			collectWitnessFeatures(child, propertyTypes, transforms)
		}
	case "list", "map":
		if prop.Item != nil {
			for _, child := range prop.Item.Properties {
				collectWitnessFeatures(child, propertyTypes, transforms)
			}
		}
	}
}

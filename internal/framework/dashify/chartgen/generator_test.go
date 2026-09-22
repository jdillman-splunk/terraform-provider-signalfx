// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestRepositoryGenerationIsCurrentAndDeterministic(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	options := repositoryOptions(repository, true)
	if err := runGeneration(options); err != nil {
		t.Fatal(err)
	}

	before := generatedDigests(t, options)
	if err := runGeneration(options); err != nil {
		t.Fatal(err)
	}
	after := generatedDigests(t, options)
	if !equalDigests(before, after) {
		t.Fatal("check mode mutated generated output")
	}

	for path := range before {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(contents), "// Copyright Splunk, Inc.\n// SPDX-License-Identifier: MPL-2.0\n") {
			t.Errorf("%s has no MPL header", path)
		}
		if strings.Contains(string(contents), repository) {
			t.Errorf("%s embeds absolute repository path %q", path, repository)
		}
	}
}

func TestWriteAndCheckModesDetectEveryStaleStateWithoutMutation(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	newFixture := func(t *testing.T) generationOptions {
		t.Helper()
		root := t.TempDir()
		copyTree(t, filepath.Join(repository, "internal", "framework", "dashify", "charts", "schemas"), filepath.Join(root, "internal", "framework", "dashify", "charts", "schemas"))
		return repositoryOptions(root, false)
	}

	t.Run("current", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(options.OutDir, "metrics_list_generated.go")
		if err := os.Remove(missing); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		assertCheckErrorWithoutMutation(t, options, "missing: "+missing)
	})

	t.Run("changed", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		changed := filepath.Join(options.OutDir, "metrics_list_generated.go")
		if err := os.WriteFile(changed, []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		assertCheckErrorWithoutMutation(t, options, "changed: "+changed)
	})

	t.Run("obsolete", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		obsolete := filepath.Join(options.OutDir, "removed_generated.go")
		if err := os.WriteFile(obsolete, []byte("// "+generatedCodeMarker+"; DO NOT EDIT.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		assertCheckErrorWithoutMutation(t, options, "obsolete: "+obsolete)
	})

	t.Run("obsolete dashboard bridge output", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		obsolete := filepath.Join(filepath.Dir(options.BridgePath), "old_chart_bridge_generated.go")
		if err := os.WriteFile(obsolete, []byte("// "+generatedCodeMarker+"; DO NOT EDIT.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		options.Check = true
		assertCheckErrorWithoutMutation(t, options, "obsolete: "+obsolete)
	})

	t.Run("legacy dashboard bridge output", func(t *testing.T) {
		options := newFixture(t)
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		legacy := options.LegacyGeneratedPaths[0]
		writeTestFile(t, legacy, "// "+legacyGeneratedCodeMarker+"; DO NOT EDIT.\n")
		options.Check = true
		assertCheckErrorWithoutMutation(t, options, "obsolete: "+legacy)
		options.Check = false
		if err := runGeneration(options); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("legacy generated bridge still exists after write mode: %v", err)
		}
	})
}

func TestGeneratorRequiresExactSixSchemasAndMatchingTypes(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	root := t.TempDir()
	copyTree(t, filepath.Join(repository, "internal", "framework", "dashify", "charts", "schemas"), filepath.Join(root, "internal", "framework", "dashify", "charts", "schemas"))
	options := repositoryOptions(root, true)

	extra := filepath.Join(options.SchemasDir, "seventh.yml")
	writeTestFile(t, extra, minimalChart("seventh", "1"))
	if err := runGeneration(options); err == nil || !strings.Contains(err.Error(), "want exactly") {
		t.Fatalf("extra schema error = %v", err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}

	listPath := filepath.Join(options.SchemasDir, "metrics_list.yml")
	contents, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "type: metrics_list", "type: wrong_name", 1))
	schemaRoot, err := os.OpenRoot(options.SchemasDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := schemaRoot.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := schemaRoot.WriteFile("metrics_list.yml", contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runGeneration(options); err == nil || !strings.Contains(err.Error(), "must exactly match filename") {
		t.Fatalf("type mismatch error = %v", err)
	}
}

func TestDSLVersionMustBeExactlySupported(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		valid   bool
	}{{"missing", "", false}, {"zero", "0", false}, {"negative", "-1", false}, {"supported", "1", true}, {"future", "2", false}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "chart.yml")
			writeTestFile(t, path, minimalChart("chart", test.version))
			_, _, err := generateChart(path, "charts")
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "unsupported version")) {
				t.Fatalf("error = %v, want unsupported version", err)
			}
		})
	}
}

func TestEveryExtendedAndReferencedDocumentRequiresSupportedVersion(t *testing.T) {
	for _, relationship := range []string{"extends", "ref"} {
		for _, test := range []struct {
			name, version string
		}{{"missing", ""}, {"future", "version: 2\n"}} {
			t.Run(relationship+"_"+test.name, func(t *testing.T) {
				root := t.TempDir()
				fragment := test.version + "properties:\n  value:\n    type: string\n    optional: true\n    description: value\n    mapping: {path: [chart, value]}\n"
				writeTestFile(t, filepath.Join(root, "fragment.yml"), fragment)
				chart := "version: 1\nproperties:\n  value:\n    ref: fragment.yml#value\n"
				if relationship == "extends" {
					chart = "version: 1\nextends: [fragment.yml]\nproperties: {}\n"
				}
				path := filepath.Join(root, "chart.yml")
				writeTestFile(t, path, chart)
				_, err := loadSchema(root, path)
				if err == nil || !strings.Contains(err.Error(), "unsupported version") {
					t.Fatalf("error = %v, want unsupported fragment version", err)
				}
			})
		}
	}
}

func TestSchemaLoaderRejectsCyclesAndRootEscapes(t *testing.T) {
	t.Run("extends cycle", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "a.yml"), "version: 1\nextends: [b.yml]\nproperties: {}\n")
		writeTestFile(t, filepath.Join(root, "b.yml"), "version: 1\nextends: [a.yml]\nproperties: {}\n")
		_, err := loadSchema(root, filepath.Join(root, "a.yml"))
		if err == nil || !strings.Contains(err.Error(), "schema reference cycle: a.yml -> b.yml -> a.yml") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("ref cycle", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "a.yml"), "version: 1\nproperties:\n  value:\n    ref: b.yml#value\n")
		writeTestFile(t, filepath.Join(root, "b.yml"), "version: 1\nproperties:\n  value:\n    ref: a.yml#value\n")
		_, err := loadSchema(root, filepath.Join(root, "a.yml"))
		if err == nil || !strings.Contains(err.Error(), "schema reference cycle") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("lexical escape", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "root")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(parent, "outside.yml"), "version: 1\nproperties: {}\n")
		writeTestFile(t, filepath.Join(root, "chart.yml"), "version: 1\nextends: [../outside.yml]\nproperties: {}\n")
		_, err := loadSchema(root, filepath.Join(root, "chart.yml"))
		if err == nil || !strings.Contains(err.Error(), "escapes schema root") {
			t.Fatalf("error = %v", err)
		}
	})

	if runtime.GOOS != "windows" {
		t.Run("symlink escape", func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "root")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(parent, "outside.yml")
			writeTestFile(t, outside, "version: 1\nproperties: {}\n")
			if err := os.Symlink(outside, filepath.Join(root, "linked.yml")); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(root, "chart.yml"), "version: 1\nextends: [linked.yml]\nproperties: {}\n")
			_, err := loadSchema(root, filepath.Join(root, "chart.yml"))
			if err == nil || !strings.Contains(err.Error(), "escapes schema root") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRefOverlayUsesExplicitPresence(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "base.yml"), `properties:
  shared:
    type: enum
    optional: true
    values: [one, two]
    default: one
    description: base
    mapping:
      path: [chart, value]
  transformed:
    type: bool
    optional: true
    description: transformed base
    mapping:
      path: [chart, transformed]
      transform: negate
  required_shared:
    type: string
    required: true
    description: required base
    mapping:
      path: [chart, required]
`)
	writeTestFile(t, filepath.Join(root, "chart.yml"), `properties:
  value:
    ref: base.yml#shared
    type: string
    optional: false
    required: false
    values: []
    default: null
  transformed_value:
    ref: base.yml#transformed
    mapping:
      transform: ""
  required_value:
    ref: base.yml#required_shared
    required: false
`)
	var base, local chartSchema
	baseRaw, err := os.ReadFile(filepath.Join(root, "base.yml"))
	if err != nil {
		t.Fatal(err)
	}
	localRaw, err := os.ReadFile(filepath.Join(root, "chart.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeStrictYAML(baseRaw, &base); err != nil {
		t.Fatal(err)
	}
	if err := decodeStrictYAML(localRaw, &local); err != nil {
		t.Fatal(err)
	}
	mergedProperty := overlay(base.Properties["shared"], local.Properties["value"])
	if mergedProperty.Type != "string" || mergedProperty.Optional || mergedProperty.Required || mergedProperty.Default != nil || len(mergedProperty.Values) != 0 || mergedProperty.Description != "base" {
		t.Fatalf("explicit zero-value overlay was not preserved: %#v", mergedProperty)
	}
	if !slices.Equal(mergedProperty.Mapping.Path, []string{"chart", "value"}) {
		t.Fatalf("unmentioned mapping path = %v", mergedProperty.Mapping.Path)
	}
	if overlay(base.Properties["required_shared"], local.Properties["required_value"]).Required {
		t.Fatal("explicit required:false did not override required:true")
	}
	transformed := overlay(base.Properties["transformed"], local.Properties["transformed_value"])
	if transformed.Mapping.Transform != "" || !slices.Equal(transformed.Mapping.Path, []string{"chart", "transformed"}) {
		t.Fatalf("explicit transform clear did not preserve path: %#v", transformed.Mapping)
	}
}

func TestStrictNestedYAMLAndContractVocabulary(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "chart.yml"), `properties:
  values:
    type: list
    description: values
    item:
      typo: string
`)
	if _, err := loadSchema(root, filepath.Join(root, "chart.yml")); err == nil || !strings.Contains(err.Error(), `field "typo"`) {
		t.Fatalf("nested unknown-key error = %v", err)
	}

	document := contractDocument{path: "future.json"}
	if _, err := document.compile(map[string]any{"type": "string", "pattern": "x"}, "#", nil); err == nil || !strings.Contains(err.Error(), `unsupported JSON Schema keyword "pattern"`) {
		t.Fatalf("unsupported vocabulary error = %v", err)
	}

	if err := decodeStrictYAML([]byte("type: first\n---\ntype: second\n"), &chartSchema{}); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("second-document error = %v", err)
	}
}

func TestRelativeInitialSchemaPathIsNotJoinedToRootTwice(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "chart.yml")
	writeTestFile(t, path, minimalChart("chart", "1"))
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	canonicalWorkingDirectory, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(canonicalWorkingDirectory, canonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	relativePath := filepath.Join(relativeRoot, "chart.yml")
	if _, err := loadSchema(relativeRoot, relativePath); err != nil {
		t.Fatalf("relative root/path load failed: %v", err)
	}
}

func TestGeneratorSupportsEveryDSLTypeAndTransform(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "all.yml")
	writeTestFile(t, path, `type: all
version: 1
description: every supported DSL shape
dashify:
  element: {name: o11y:Test, args: []}
properties:
  string_value: {type: string, optional: true, description: string, mapping: {path: [chart, string]}}
  negated: {type: bool, optional: true, description: bool, mapping: {path: [chart, negated], transform: negate}}
  integer: {type: number, optional: true, min: 0, max: 10, description: number, mapping: {path: [chart, integer]}}
  required_integer: {type: number, required: true, description: required number, mapping: {path: [chart, requiredInteger]}}
  decimal: {type: float, optional: true, description: float, mapping: {path: [chart, decimal]}}
  required_decimal: {type: float, required: true, description: required float, mapping: {path: [chart, requiredDecimal]}}
  choice: {type: enum, optional: true, values: [one, two], description: enum, mapping: {path: [chart, choice]}}
  strings: {type: list, optional: true, description: list, mapping: {path: [chart, strings]}, item: {type: string}}
  objects:
    type: map
    optional: true
    description: map
    mapping: {path: [chart, objects]}
    item:
      properties:
        enabled: {type: bool, optional: true, description: enabled, mapping: {path: [enabled]}}
  object:
    type: block
    optional: true
    description: block
    mapping: {path: [chart, object]}
    properties:
      name: {type: string, required: true, description: name, mapping: {path: [name]}}
  union:
    type: oneof
    optional: true
    description: union
    mapping: {path: [chart, union]}
    variants:
      numeric: {type: number, description: numeric}
      custom:
        type: block
        description: custom
        properties:
          prefix: {type: string, optional: true, description: prefix, mapping: {path: [prefix]}}
  duration: {type: string, optional: true, description: duration, mapping: {path: [datasource, time], transform: relative_duration}}
  required_duration: {type: string, required: true, description: required duration, mapping: {path: [datasource, requiredTime], transform: relative_duration}}
  wrapped: {type: string, optional: true, description: wrapped, mapping: {path: [chart, wrapped], wrap: array}}
`)
	source, _, err := generateChart(path, "charts")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"TakeStringList", "TakeObjectMap", "TakeFloat64", "TakeInt64", "TakeRelativeDuration", "WrappedInArray", "parseAllUnionModel", "json.Number", "jsonInt64", "stringvalidator.OneOf"} {
		if !strings.Contains(source, fragment) {
			t.Errorf("generated source does not exercise %q", fragment)
		}
	}
	for _, field := range []string{"RequiredInteger", "RequiredDecimal", "RequiredDuration"} {
		fragment := "if !p." + field + ".IsUnknown() && p." + field + ".IsNull()"
		if !strings.Contains(source, fragment) {
			t.Errorf("generated source does not enforce required scalar %s", field)
		}
	}
}

func TestGeneratorRejectsSemanticallyUnsupportedDSL(t *testing.T) {
	base := func(propertyText string) string {
		return `type: chart
version: 1
description: chart
dashify:
  element: {name: o11y:Test, args: []}
properties:
` + propertyText
	}
	tests := []struct {
		name, property, want string
	}{
		{"unknown type", "  value: {type: mystery, optional: true, description: value, mapping: {path: [chart, value]}}\n", "unsupported type"},
		{"required and optional", "  value: {type: string, required: true, optional: true, description: value, mapping: {path: [chart, value]}}\n", "both optional and required"},
		{"unsupported transform", "  value: {type: string, optional: true, description: value, mapping: {path: [chart, value], transform: magic}}\n", "unsupported mapping.transform"},
		{"invalid mapping", "  value: {type: string, optional: true, description: value, mapping: {path: [chart, '']}}\n", "invalid mapping.path"},
		{"irrelevant enum values", "  value: {type: string, optional: true, values: [x], description: value, mapping: {path: [chart, value]}}\n", "is ignored"},
		{"invalid string default", "  value: {type: string, optional: true, default: false, description: value, mapping: {path: [chart, value]}}\n", "default"},
		{"invalid enum default", "  value: {type: enum, optional: true, values: [one], default: typo, description: value, mapping: {path: [chart, value]}}\n", "default"},
		{"out of range default", "  value: {type: number, optional: true, min: 1, max: 2, default: 3, description: value, mapping: {path: [chart, value]}}\n", "default"},
		{"relative wrapped", "  value: {type: string, optional: true, description: value, mapping: {path: [chart, value], transform: relative_duration, wrap: array}}\n", "cannot combine"},
		{"bool scalar list", "  value: {type: list, optional: true, description: value, mapping: {path: [chart, value]}, item: {type: bool}}\n", "only string scalar lists"},
		{"number scalar list", "  value: {type: list, optional: true, description: value, mapping: {path: [chart, value]}, item: {type: number}}\n", "only string scalar lists"},
		{"float scalar list", "  value: {type: list, optional: true, description: value, mapping: {path: [chart, value]}, item: {type: float}}\n", "only string scalar lists"},
		{"enum scalar list", "  value: {type: list, optional: true, description: value, mapping: {path: [chart, value]}, item: {type: enum}}\n", "only string scalar lists"},
		{"oneof scalar mapping ignored", `  value:
    type: oneof
    optional: true
    description: value
    mapping: {path: [chart, value]}
    variants:
      named: {type: string, description: named, mapping: {path: [ignored]}}
`, "unsupported or ambiguous variants"},
		{"missing presence", "  value: {type: string, description: value, mapping: {path: [chart, value]}}\n", "exactly one of optional or required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "chart.yml")
			writeTestFile(t, path, base(test.property))
			if _, _, err := generateChart(path, "charts"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestGeneratorRejectsMappingAndConstraintAmbiguity(t *testing.T) {
	tests := []struct {
		name, properties, want string
	}{
		{"duplicate path", `
  first: {type: string, optional: true, description: first, mapping: {path: [chart, value]}}
  second: {type: string, optional: true, description: second, mapping: {path: [chart, value]}}
`, "duplicate or prefix-colliding"},
		{"prefix path", `
  first: {type: string, optional: true, description: first, mapping: {path: [chart, value]}}
  second: {type: string, optional: true, description: second, mapping: {path: [chart, value, child]}}
`, "duplicate or prefix-colliding"},
		{"Go field collision", `
  foo_bar: {type: string, optional: true, description: first, mapping: {path: [chart, first]}}
  foo__bar: {type: string, optional: true, description: second, mapping: {path: [chart, second]}}
`, "collide as generated Go field"},
		{"one-sided conflict", `
  alpha: {type: string, optional: true, description: alpha, mapping: {path: [chart, alpha]}}
  zulu: {type: string, optional: true, conflicts_with: [alpha], description: zulu, mapping: {path: [chart, zulu]}}
`, "not reciprocal"},
		{"condition outside enum", `
  mode: {type: enum, optional: true, values: [Range, Scale], description: mode, mapping: {path: [chart, mode]}}
  scale: {type: string, optional: true, description: scale, requires_value: {field: mode, value: Typo}, mapping: {path: [chart, scale]}}
`, "outside \"mode\"'s enum"},
		{"flattened oneof collision", `
  union:
    type: oneof
    optional: true
    description: union
    mapping: {path: [chart, union]}
    variants:
      first:
        type: block
        description: first
        properties:
          same: {type: string, optional: true, description: same, mapping: {path: [first]}}
      second:
        type: block
        description: second
        properties:
          same: {type: string, optional: true, description: same, mapping: {path: [second]}}
`, "unsupported or ambiguous variants"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "chart.yml")
			writeTestFile(t, path, `type: chart
version: 1
description: chart
dashify:
  element: {name: o11y:Test, args: []}
properties:
`+test.properties)
			if _, _, err := generateChart(path, "charts"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCompactContractRejectsInvalidCompositionAndKinds(t *testing.T) {
	document := contractDocument{path: "contract.json", defs: map[string]any{"String": map[string]any{"type": "string"}}}
	for _, test := range []struct {
		name string
		raw  map[string]any
		want string
	}{
		{"ref structural sibling", map[string]any{"$ref": "#/$defs/String", "type": "string"}, "structural $ref sibling"},
		{"anyOf structural sibling", map[string]any{"anyOf": []any{map[string]any{"type": "string"}}, "type": "string"}, "structural anyOf sibling"},
		{"unknown kind", map[string]any{"type": "integer"}, "unsupported kind"},
		{"duplicate kind", map[string]any{"type": []any{"string", "string"}}, "duplicate kind"},
		{"empty union", map[string]any{"type": []any{}}, "empty"},
		{"empty anyOf", map[string]any{"anyOf": []any{}}, "empty anyOf"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := document.compile(test.raw, "#", map[string]bool{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	if !generatorKindMatches(nil, []string{"number", "null"}) {
		t.Error("nullable number did not accept null")
	}
}

func TestGeneratorRequiresExplicitEmptyElementArgs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "chart.yml")
	writeTestFile(t, path, strings.Replace(minimalChart("chart", "1"), "    args: []\n", "", 1))
	if _, _, err := generateChart(path, "charts"); err == nil || !strings.Contains(err.Error(), "missing explicit dashify.element.args") {
		t.Fatalf("missing args error = %v", err)
	}
}

func TestMappingValidatorRejectsDeprecatedAnyOfWrapper(t *testing.T) {
	contract := &contractNode{Kinds: []string{"object"}, Properties: map[string]*contractNode{
		"legacy": {Deprecated: true, AnyOf: []*contractNode{{Kinds: []string{"string"}}, {Kinds: []string{"null"}}}},
	}}
	properties := map[string]property{"value": {
		Type: "string", Description: "value", Optional: true,
		Mapping: propertyMapping{Path: []string{"legacy"}},
	}}
	if err := validatePropertyMappings("chart.yml", properties, contract); err == nil || !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("deprecated mapping error = %v", err)
	}
}

func TestNormalizationDescriptorsFailClosed(t *testing.T) {
	stringNode := func(deprecated bool) *contractNode {
		return &contractNode{Kinds: []string{"string"}, Deprecated: deprecated}
	}
	numberNode := func(deprecated bool) *contractNode {
		return &contractNode{Kinds: []string{"number"}, Deprecated: deprecated}
	}
	base := func() *contractNode {
		return &contractNode{Kinds: []string{"object"}, Properties: map[string]*contractNode{
			"current": stringNode(false),
			"legacy":  stringNode(true),
			"unit":    stringNode(true),
			"prefix":  stringNode(true),
			"suffix":  stringNode(true),
		}}
	}
	tests := []struct {
		name string
		rule normalization
		edit func(*contractNode)
		want string
	}{
		{"unknown kind", normalization{Kind: "magic", Target: []string{"current"}}, nil, "unsupported kind"},
		{"missing fallback source", normalization{Kind: "fallback", Target: []string{"current"}}, nil, "requires source"},
		{"final wildcard", normalization{Kind: "fallback", Target: []string{"current"}, Source: []string{"legacy", "*"}}, nil, "cannot end in a wildcard"},
		{"wildcard mismatch", normalization{Kind: "fallback", Target: []string{"current"}, Source: []string{"legacy", "*", "value"}}, nil, "wildcard count"},
		{"absent target", normalization{Kind: "fallback", Target: []string{"missing"}, Source: []string{"legacy"}}, nil, "absent at segment"},
		{"deprecated target", normalization{Kind: "fallback", Target: []string{"current"}, Source: []string{"legacy"}}, func(root *contractNode) { root.Properties["current"].Deprecated = true }, "target current is deprecated"},
		{"nondeprecated source", normalization{Kind: "fallback", Target: []string{"current"}, Source: []string{"legacy"}}, func(root *contractNode) { root.Properties["legacy"].Deprecated = false }, "not marked deprecated"},
		{"fallback union incompatibility", normalization{Kind: "fallback", Target: []string{"current"}, Source: []string{"legacy"}}, func(root *contractNode) {
			root.Properties["legacy"] = &contractNode{Deprecated: true, AnyOf: []*contractNode{stringNode(false), numberNode(false)}}
		}, "incompatible"},
		{"wildcard container mismatch", normalization{Kind: "fallback", Target: []string{"mapTarget", "*", "value"}, Source: []string{"listSource", "*", "value"}}, func(root *contractNode) {
			root.Properties["mapTarget"] = &contractNode{Kinds: []string{"object"}, Additional: &contractNode{Kinds: []string{"object"}, Properties: map[string]*contractNode{"value": stringNode(false)}}}
			root.Properties["listSource"] = &contractNode{Kinds: []string{"array"}, Items: &contractNode{Kinds: []string{"object"}, Properties: map[string]*contractNode{"value": stringNode(true)}}}
		}, "wildcard bindings [list] do not match target bindings [map]"},
		{"display target lacks object variant", normalization{Kind: "display_unit", Target: []string{"current"}, Unit: []string{"unit"}, Prefix: []string{"prefix"}, Suffix: []string{"suffix"}}, nil, "incompatible shapes"},
		{"single source wrong shape", normalization{Kind: "single_value_publish_label_options", Target: []string{"current"}, Source: []string{"legacy"}}, nil, "incompatible shapes"},
		{"wrap target wrong shape", normalization{Kind: "wrap_scalar_array", Target: []string{"current"}}, nil, "accept both scalar string and string array"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := base()
			if test.edit != nil {
				test.edit(contract)
			}
			if err := validateNormalizations("chart.yml", []normalization{test.rule}, contract); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func repositoryOptions(root string, check bool) generationOptions {
	return generationOptions{
		SchemasDir:           filepath.Join(root, "internal", "framework", "dashify", "charts", "schemas"),
		OutDir:               filepath.Join(root, "internal", "framework", "dashify", "charts"),
		PackageName:          "charts",
		BridgePath:           filepath.Join(root, "internal", "framework", "dashify", "dashboard", "dashboard_charts_generated.go"),
		LegacyGeneratedPaths: []string{filepath.Join(root, "internal", "framework", "dashify", "dashboard_charts_generated.go")},
		BridgePackage:        "dashboard",
		ChartsImport:         "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts",
		Check:                check,
	}
}

func minimalChart(chartType, version string) string {
	versionLine := ""
	if version != "" {
		versionLine = "version: " + version + "\n"
	}
	return "type: " + chartType + "\n" + versionLine + `description: test chart
dashify:
  element:
    name: o11y:Test
    args: []
properties:
  title:
    type: string
    optional: true
    description: title
    mapping:
      path: [widget, title]
`
}

func assertCheckErrorWithoutMutation(t *testing.T, options generationOptions, wanted string) {
	t.Helper()
	before := generatedDigests(t, options)
	err := runGeneration(options)
	if err == nil || !strings.Contains(err.Error(), wanted) {
		t.Fatalf("check error = %v, want %q", err, wanted)
	}
	after := generatedDigests(t, options)
	if !equalDigests(before, after) {
		t.Fatal("check mode mutated files while reporting stale output")
	}
}

func generatedDigests(t *testing.T, options generationOptions) map[string][sha256.Size]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(options.OutDir, "*_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, options.BridgePath)
	paths = append(paths, options.LegacyGeneratedPaths...)
	result := make(map[string][sha256.Size]byte, len(paths))
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		result[path] = sha256.Sum256(contents)
	}
	return result
}

func equalDigests(left, right map[string][sha256.Size]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for path, digest := range left {
		if right[path] != digest {
			return false
		}
	}
	return true
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

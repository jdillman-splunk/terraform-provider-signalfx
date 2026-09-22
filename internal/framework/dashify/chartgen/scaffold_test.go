// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestScaffoldContractGolden(t *testing.T) {
	contracts := filepath.Join("testdata", "scaffold")
	tests := []struct {
		name       string
		options    scaffoldOptions
		golden     string
		warnings   []string
		compilable bool
	}{
		{
			name: "complete contract",
			options: scaffoldOptions{
				ContractsDir: contracts,
				Contract:     "O11yExample.schema.json",
				Type:         "metrics_example",
			},
			golden: "full.golden.yml",
			warnings: []string{
				"#/properties/size is a JSON number scaffolded as Terraform float; review whether number (int64) is intended",
				"#/properties/widget/properties/legacyColor is deprecated and was omitted",
				"#/properties/widget/properties/threshold is a JSON number scaffolded as Terraform float; review whether number (int64) is intended",
			},
			compilable: true,
		},
		{
			name: "definition fragment",
			options: scaffoldOptions{
				ContractsDir: contracts,
				Contract:     "O11yExample.schema.json",
				Pointer:      "#/$defs/Widget",
			},
			golden: "fragment.golden.yml",
			warnings: []string{
				"#/$defs/Widget/properties/legacyColor is deprecated and was omitted",
				"#/$defs/Widget/properties/threshold is a JSON number scaffolded as Terraform float; review whether number (int64) is intended",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, warnings, err := scaffoldContract(test.options)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join(contracts, test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != string(expected) {
				t.Fatalf("scaffold output differs from %s\n--- actual ---\n%s\n--- expected ---\n%s", test.golden, actual, expected)
			}
			if !slices.Equal(warnings, test.warnings) {
				t.Fatalf("warnings = %#v, want %#v", warnings, test.warnings)
			}
			if test.compilable {
				path := filepath.Join(t.TempDir(), "metrics_example.yml")
				if err := os.WriteFile(path, actual, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, _, err := generateChart(path, "charts"); err != nil {
					t.Fatalf("generated scaffold is not accepted by chartgen: %v", err)
				}
			}
		})
	}
}

func TestRepositoryTimeSeriesContractScaffoldsToCompilableDSL(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	contracts := filepath.Join(repository, "internal", "framework", "dashify", "charts", "schemas", "elements")
	output, _, err := scaffoldContract(scaffoldOptions{
		ContractsDir: contracts,
		Contract:     "O11yTimeSeriesChart.schema.json",
		Type:         "metrics_time_series_scaffold",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "metrics_time_series_scaffold.yml")
	if err := os.WriteFile(path, output, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := generateChart(path, "charts"); err != nil {
		t.Fatalf("repository TimeSeries scaffold is not accepted by chartgen: %v", err)
	}
}

func TestScaffoldContractRejectsUnsafeInputs(t *testing.T) {
	contracts := filepath.Join("testdata", "scaffold")
	tests := []struct {
		name    string
		options scaffoldOptions
		want    string
	}{
		{
			name:    "path traversal",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "../O11yExample.schema.json", Type: "example"},
			want:    "must be a basename",
		},
		{
			name:    "missing contract",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "Missing.schema.json", Type: "example"},
			want:    "reading Olly contract",
		},
		{
			name:    "invalid pointer",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "O11yExample.schema.json", Pointer: "#/properties/widget"},
			want:    "must be # or #/$defs/<name>",
		},
		{
			name:    "missing definition",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "O11yExample.schema.json", Pointer: "#/$defs/Missing"},
			want:    "has no object definition",
		},
		{
			name:    "missing complete type",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "O11yExample.schema.json"},
			want:    "type is required",
		},
		{
			name:    "fragment with type",
			options: scaffoldOptions{ContractsDir: contracts, Contract: "O11yExample.schema.json", Pointer: "#/$defs/Widget", Type: "example"},
			want:    "type must be omitted",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := scaffoldContract(test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestScaffoldContractRejectsBrokenOrUnsupportedSchemas(t *testing.T) {
	tests := []struct {
		name     string
		property string
		want     string
	}{
		{
			name:     "unresolved reference",
			property: `{"$ref":"#/$defs/Missing"}`,
			want:     `references missing definition "Missing"`,
		},
		{
			name:     "unknown keyword",
			property: `{"type":"string","format":"hostname"}`,
			want:     `uses unsupported JSON Schema keyword "format"`,
		},
		{
			name:     "unsupported union",
			property: `{"description":"value","anyOf":[{"type":"string"},{"type":"array","items":{"type":"string"}}]}`,
			want:     `alternative "list" becomes unsupported oneof variant type "list"`,
		},
		{
			name:     "unsupported scalar list",
			property: `{"description":"value","type":"array","items":{"type":"number"}}`,
			want:     `DSL v1 supports only string or fixed-shape object list items`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			contract := `{"title":"o11y:Broken","description":"broken","type":"object","properties":{"value":` + test.property + `}}`
			if err := os.WriteFile(filepath.Join(root, "Broken.schema.json"), []byte(contract), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := scaffoldContract(scaffoldOptions{ContractsDir: root, Contract: "Broken.schema.json", Type: "broken"})
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "#/properties/value") {
				t.Fatalf("error = %v, want path and substring %q", err, test.want)
			}
		})
	}
}

func TestScaffoldTerraformName(t *testing.T) {
	tests := map[string]string{
		"maxDelay":       "max_delay",
		"O11yWidget":     "o11y_widget",
		"rangeEnd":       "range_end",
		"already_snake":  "already_snake",
		"dash-separated": "dash_separated",
	}
	for input, expected := range tests {
		if actual := scaffoldTerraformName(input); actual != expected {
			t.Errorf("scaffoldTerraformName(%q) = %q, want %q", input, actual, expected)
		}
	}
}

// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package elements_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts/schemas/elements"
)

var expectedElements = []string{
	"<o11y:ClusterMap>",
	"<o11y:List>",
	"<o11y:SingleValue>",
	"<o11y:TableChart>",
	"<o11y:Text>",
	"<o11y:TimeSeriesChart>",
}

type schemaDocument struct {
	Schema      string         `json:"$schema"`
	ID          string         `json:"$id"`
	Title       string         `json:"title"`
	Required    []string       `json:"required"`
	Definitions map[string]any `json:"$defs"`
	Raw         map[string]any `json:"-"`
}

func TestCatalogIsExactlyTheSixSupportedElements(t *testing.T) {
	t.Parallel()

	entries, err := elements.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var schemaFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".schema.json") {
			schemaFiles = append(schemaFiles, entry.Name())
		}
	}
	sort.Strings(schemaFiles)

	var catalogFiles []string
	var catalogElements []string
	for _, definition := range elements.Catalog {
		catalogFiles = append(catalogFiles, definition.File)
		catalogElements = append(catalogElements, definition.Element)
	}
	sort.Strings(catalogFiles)
	sort.Strings(catalogElements)
	wantedElements := slices.Clone(expectedElements)
	sort.Strings(wantedElements)

	if !slices.Equal(catalogFiles, schemaFiles) {
		t.Fatalf("catalog files = %v, schema files = %v", catalogFiles, schemaFiles)
	}
	if !slices.Equal(catalogElements, wantedElements) {
		t.Fatalf("catalog elements = %v, want %v", catalogElements, wantedElements)
	}

	for _, element := range expectedElements {
		definition, ok := elements.Find(element)
		if !ok {
			t.Errorf("Find(%q) did not find a schema", element)
			continue
		}
		if definition.Element != element {
			t.Errorf("Find(%q).Element = %q", element, definition.Element)
		}
	}
	if _, ok := elements.Find("<o11y:Heatmap>"); ok {
		t.Error("unexpected schema for unsupported <o11y:Heatmap>")
	}
}

func TestCatalogHasNoDuplicateIdentifiers(t *testing.T) {
	t.Parallel()

	files := make(map[string]bool, len(elements.Catalog))
	elementTags := make(map[string]bool, len(elements.Catalog))
	schemaIDs := make(map[string]bool, len(elements.Catalog))
	for _, definition := range elements.Catalog {
		if files[definition.File] {
			t.Errorf("duplicate catalog file %q", definition.File)
		}
		files[definition.File] = true
		if elementTags[definition.Element] {
			t.Errorf("duplicate catalog element %q", definition.Element)
		}
		elementTags[definition.Element] = true
		if schemaIDs[definition.SchemaID] {
			t.Errorf("duplicate catalog schema ID %q", definition.SchemaID)
		}
		schemaIDs[definition.SchemaID] = true
	}
}

func TestSchemasCompileAsDraft2020AndResolveLocalReferences(t *testing.T) {
	t.Parallel()

	seenIDs := make(map[string]string, len(elements.Catalog))
	seenTitles := make(map[string]string, len(elements.Catalog))
	for _, definition := range elements.Catalog {
		t.Run(definition.File, func(t *testing.T) {
			document := readSchema(t, definition)
			if document.Schema != elements.Draft2020 {
				t.Errorf("$schema = %q, want %q", document.Schema, elements.Draft2020)
			}
			if document.ID != definition.SchemaID {
				t.Errorf("$id = %q, want %q", document.ID, definition.SchemaID)
			}
			if "<"+document.Title+">" != definition.Element {
				t.Errorf("title = %q, element = %q", document.Title, definition.Element)
			}
			if len(document.Definitions) == 0 {
				t.Fatal("schema has no $defs")
			}

			assertUnique(t, seenIDs, document.ID, definition.File, "$id")
			assertUnique(t, seenTitles, document.Title, definition.File, "title")
			assertAllReferencesAreLocalAndResolvable(t, document.Raw)
			_ = compileSchema(t, definition, document.Raw)
		})
	}
}

func TestSchemasRequireTheirRootPropertyGroups(t *testing.T) {
	t.Parallel()

	for _, definition := range elements.Catalog {
		t.Run(definition.File, func(t *testing.T) {
			t.Parallel()

			document := readSchema(t, definition)
			schema := compileSchema(t, definition, document.Raw)
			valid := map[string]any{
				"chart":  map[string]any{},
				"widget": map[string]any{},
			}
			if definition.Element != "<o11y:Text>" {
				valid["datasource"] = map[string]any{}
			}
			if err := schema.Validate(valid); err != nil {
				t.Fatalf("minimal properties are invalid: %v", err)
			}
			for _, property := range document.Required {
				invalid := cloneMap(valid)
				delete(invalid, property)
				if err := schema.Validate(invalid); err == nil {
					t.Errorf("schema accepted missing required root property %q", property)
				}
			}
		})
	}
}

func TestRepresentativeEmittedDocumentsValidate(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"<o11y:ClusterMap>": `{
			"<o11y:ClusterMap>": [],
			"chart": {
				"colorBy": "Range",
				"colorRange": {"palette": "temperature", "min": 0.5, "max": 99.9},
				"colorScale": [{"gte": 80, "paletteIndex": 4}],
				"displayUnit": {"suffix": "%"},
				"groupBy": ["k8s.cluster.name", "k8s.node.name"]
			},
			"datasource": {"program": "data('node.utilization').publish()"},
			"widget": {"title": "Nodes"}
		}`,
		"<o11y:List>": `{
			"<o11y:List>": [],
			"chart": {
				"colorBy": "Scale",
				"displayFields": [{"property": "service.name", "enabled": true}],
				"publishedStreams": {"A": {"name": "Latency", "displayUnit": "Millisecond"}},
				"sort": {"by": "value", "direction": "desc"}
			},
			"datasource": {"program": "data('service.request.duration').publish(label='A')"},
			"widget": {"title": "Services"}
		}`,
		"<o11y:SingleValue>": `{
			"<o11y:SingleValue>": [],
			"chart": {
				"colorScale": [{"lt": 80, "paletteIndex": 2}, {"gte": 80, "color": "#d41f1f"}],
				"displayUnit": {"suffix": "%"},
				"secondaryVisualization": "Sparkline"
			},
			"datasource": {"program": "data('memory.utilization').publish()"},
			"widget": {"title": "Memory"}
		}`,
		"<o11y:TableChart>": `{
			"<o11y:TableChart>": [],
			"chart": {
				"columns": [{"field": "service.name", "headerName": "Service"}],
				"groupBy": ["service.name"],
				"sort": {"by": "value", "order": "desc"}
			},
			"datasource": {"program": "data('service.request.duration').publish(label='A')"},
			"widget": {"title": "Latency by service"}
		}`,
		"<o11y:Text>": `{
			"<o11y:Text>": [],
			"chart": {"markdown": "## Deployment health"},
			"widget": {"title": "Notes"}
		}`,
		"<o11y:TimeSeriesChart>": `{
			"<o11y:TimeSeriesChart>": [],
			"chart": {
				"chartOptions": {"type": "stacked-area", "colorBy": "Metric"},
				"additionalProperties": [{"property": "host.name", "enabled": true}],
				"seriesOptions": {"A": {"paletteIndex": 3, "plotType": "area"}},
				"marks": [{"value": 80.5, "style": "dashed"}]
			},
			"datasource": {
				"program": "data('cpu.utilization').publish(label='A')",
				"maxDelay": null,
				"time": {"type": "relative", "range": 3600000, "rangeEnd": 0}
			},
			"widget": {"title": "CPU", "links": [{"url": "/service/{{{service}}}"}]}
		}`,
	}

	for _, element := range expectedElements {
		t.Run(element, func(t *testing.T) {
			t.Parallel()

			raw, ok := testCases[element]
			if !ok {
				t.Fatalf("no representative document for %s", element)
			}
			document := decodeObject(t, []byte(raw))
			definition, properties := splitEmittedDocument(t, document)
			if definition.Element != element {
				t.Fatalf("document element = %q, test = %q", definition.Element, element)
			}
			contractDocument := readSchema(t, definition)
			schema := compileSchema(t, definition, contractDocument.Raw)
			if err := schema.Validate(properties); err != nil {
				t.Fatalf("representative emitted document is invalid: %v", err)
			}
		})
	}
}

func TestContractNuancesUsedByTerraform(t *testing.T) {
	t.Parallel()

	t.Run("time series option name is optional", func(t *testing.T) {
		validateProperties(t, "<o11y:TimeSeriesChart>", map[string]any{
			"chart": map[string]any{
				"seriesOptions": map[string]any{"A": map[string]any{"paletteIndex": float64(3)}},
			},
			"datasource": map[string]any{},
			"widget":     map[string]any{},
		}, false)
	})

	t.Run("cluster map rejects a named display unit", func(t *testing.T) {
		validateProperties(t, "<o11y:ClusterMap>", map[string]any{
			"chart":      map[string]any{"displayUnit": "Byte"},
			"datasource": map[string]any{},
			"widget":     map[string]any{},
		}, true)
	})

	t.Run("local widget reference is enforced", func(t *testing.T) {
		validateProperties(t, "<o11y:Text>", map[string]any{
			"chart":  map[string]any{},
			"widget": map[string]any{"title": true},
		}, true)
	})
}

func readSchema(t *testing.T, definition elements.Definition) schemaDocument {
	t.Helper()

	contents, err := elements.Read(definition)
	if err != nil {
		t.Fatal(err)
	}
	var document schemaDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	document.Raw = decodeObject(t, contents)
	return document
}

func compileSchema(t *testing.T, definition elements.Definition, document map[string]any) *jsonschema.Schema {
	t.Helper()

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(definition.SchemaID, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(definition.SchemaID)
	if err != nil {
		t.Fatalf("compile %s: %v", definition.File, err)
	}
	return schema
}

func assertAllReferencesAreLocalAndResolvable(t *testing.T, root map[string]any) {
	t.Helper()

	references := 0
	walkObjects(root, func(path string, object map[string]any) {
		ref, ok := object["$ref"].(string)
		if !ok {
			return
		}
		references++
		if !strings.HasPrefix(ref, "#/$defs/") {
			t.Errorf("%s has non-local reference %q", path, ref)
			return
		}
		if _, ok := resolveJSONPointer(root, ref); !ok {
			t.Errorf("%s has unresolved reference %q", path, ref)
		}
	})
	if references == 0 {
		t.Error("schema has no $ref nodes; expected generated shared definitions")
	}
}

func resolveJSONPointer(root any, ref string) (any, bool) {
	if ref == "#" {
		return root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	current := root
	for _, encoded := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment, err := url.PathUnescape(encoded)
		if err != nil {
			return nil, false
		}
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func walkObjects(value any, visit func(string, map[string]any)) {
	var walk func(any, string)
	walk = func(value any, path string) {
		switch typed := value.(type) {
		case map[string]any:
			visit(path, typed)
			for key, child := range typed {
				walk(child, path+"."+key)
			}
		case []any:
			for index, child := range typed {
				walk(child, fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(value, "$")
}

func splitEmittedDocument(t *testing.T, document map[string]any) (elements.Definition, map[string]any) {
	t.Helper()

	properties := cloneMap(document)
	var definition elements.Definition
	found := false
	for key, value := range properties {
		if !strings.HasPrefix(key, "<") {
			continue
		}
		if found {
			t.Fatalf("emitted document has more than one element tag")
		}
		var ok bool
		definition, ok = elements.Find(key)
		if !ok {
			t.Fatalf("emitted document has unknown element tag %q", key)
		}
		arguments, ok := value.([]any)
		if !ok || len(arguments) != 0 {
			t.Fatalf("element %s arguments = %#v, want []", key, value)
		}
		delete(properties, key)
		found = true
	}
	if !found {
		t.Fatal("emitted document has no element tag")
	}
	return definition, properties
}

func validateProperties(t *testing.T, element string, properties map[string]any, wantError bool) {
	t.Helper()

	definition, ok := elements.Find(element)
	if !ok {
		t.Fatalf("no schema for %s", element)
	}
	document := readSchema(t, definition)
	err := compileSchema(t, definition, document.Raw).Validate(properties)
	if wantError && err == nil {
		t.Fatal("schema accepted invalid properties")
	}
	if !wantError && err != nil {
		t.Fatalf("schema rejected valid properties: %v", err)
	}
}

func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func decodeObject(t *testing.T, contents []byte) map[string]any {
	t.Helper()

	var object map[string]any
	if err := json.Unmarshal(contents, &object); err != nil {
		t.Fatal(err)
	}
	if object == nil {
		t.Fatal("JSON document is not an object")
	}
	return object
}

func assertUnique(t *testing.T, seen map[string]string, value, file, field string) {
	t.Helper()

	if prior, duplicate := seen[value]; duplicate {
		t.Errorf("%s %q is shared by %s and %s", field, value, prior, file)
		return
	}
	seen[value] = file
}

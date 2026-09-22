// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package elements_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/observability/charts/schemas/elements"
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

type provenance struct {
	Repository      string           `json:"repository"`
	SourceRef       string           `json:"sourceRef"`
	SourceCommit    string           `json:"sourceCommit"`
	MergeRequest    string           `json:"mergeRequest"`
	SourceDirectory string           `json:"sourceDirectory"`
	ContractSource  string           `json:"contractSource"`
	Generator       string           `json:"generator"`
	SchemaDraft     string           `json:"schemaDraft"`
	VendoredAt      string           `json:"vendoredAt"`
	Note            string           `json:"note"`
	Files           []provenanceFile `json:"files"`
}

type provenanceFile struct {
	File     string `json:"file"`
	Element  string `json:"element"`
	SchemaID string `json:"schemaId"`
	SHA256   string `json:"sha256"`
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

func TestCatalogProvenanceAndChecksums(t *testing.T) {
	t.Parallel()

	manifest := readProvenance(t)
	if manifest.SourceCommit != elements.SourceCommit {
		t.Errorf("source commit = %q, catalog = %q", manifest.SourceCommit, elements.SourceCommit)
	}
	if manifest.SourceDirectory != elements.SourceDirectory {
		t.Errorf("source directory = %q, catalog = %q", manifest.SourceDirectory, elements.SourceDirectory)
	}
	if manifest.SchemaDraft != elements.Draft2020 {
		t.Errorf("schema draft = %q, want %q", manifest.SchemaDraft, elements.Draft2020)
	}
	if manifest.Repository != "https://cd.splunkdev.com/observability/shared/olly" {
		t.Errorf("unexpected source repository %q", manifest.Repository)
	}
	if manifest.MergeRequest != "https://cd.splunkdev.com/observability/shared/olly/-/merge_requests/24406" {
		t.Errorf("unexpected source merge request %q", manifest.MergeRequest)
	}
	if manifest.SourceRef == "" || manifest.ContractSource == "" || manifest.Generator == "" || manifest.VendoredAt == "" || manifest.Note == "" {
		t.Error("provenance must identify the source ref, contract, generator, vendoring date, and update policy")
	}
	if len(manifest.Files) != len(elements.Catalog) {
		t.Fatalf("provenance has %d files, catalog has %d", len(manifest.Files), len(elements.Catalog))
	}

	manifestByFile := make(map[string]provenanceFile, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, duplicate := manifestByFile[file.File]; duplicate {
			t.Fatalf("duplicate provenance entry for %s", file.File)
		}
		manifestByFile[file.File] = file
	}
	checksumFile := readChecksumFile(t)

	for _, definition := range elements.Catalog {
		t.Run(definition.File, func(t *testing.T) {
			t.Parallel()

			contents, err := elements.Read(definition)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(contents)
			actual := hex.EncodeToString(digest[:])
			if actual != definition.SHA256Sum {
				t.Errorf("artifact SHA-256 = %s, catalog = %s", actual, definition.SHA256Sum)
			}
			if checksumFile[definition.File] != actual {
				t.Errorf("SHA256SUMS[%s] = %q, actual = %q", definition.File, checksumFile[definition.File], actual)
			}

			pinned, ok := manifestByFile[definition.File]
			if !ok {
				t.Fatalf("source.json has no entry for %s", definition.File)
			}
			if pinned.Element != strings.TrimSuffix(strings.TrimPrefix(definition.Element, "<"), ">") {
				t.Errorf("provenance element = %q, catalog = %q", pinned.Element, definition.Element)
			}
			if pinned.SchemaID != definition.SchemaID {
				t.Errorf("provenance schema ID = %q, catalog = %q", pinned.SchemaID, definition.SchemaID)
			}
			if pinned.SHA256 != actual {
				t.Errorf("provenance SHA-256 = %q, actual = %q", pinned.SHA256, actual)
			}
		})
	}
	if len(checksumFile) != len(elements.Catalog) {
		t.Errorf("SHA256SUMS has %d entries, want %d", len(checksumFile), len(elements.Catalog))
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

func readProvenance(t *testing.T) provenance {
	t.Helper()

	contents, err := elements.Files.ReadFile("source.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest provenance
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readChecksumFile(t *testing.T) map[string]string {
	t.Helper()

	contents, err := elements.Files.ReadFile("SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			t.Fatalf("malformed SHA256SUMS line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != sha256.Size*2 {
			t.Fatalf("invalid SHA-256 %q for %s", fields[0], fields[1])
		}
		if _, duplicate := checksums[fields[1]]; duplicate {
			t.Fatalf("duplicate SHA256SUMS entry for %s", fields[1])
		}
		checksums[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return checksums
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

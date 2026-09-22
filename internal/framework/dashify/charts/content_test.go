// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package charts

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var expectedContentNames = []string{
	MetricsClusterMapName,
	MetricsListName,
	MetricsSingleValueName,
	MetricsTableName,
	MetricsTimeSeriesName,
	TextName,
}

func TestContentBlocksAreTheSixNestedBlocksWithConditionalRootRequirements(t *testing.T) {
	blocks := ContentBlocks()
	var names []string
	for name, block := range blocks {
		names = append(names, name)
		if _, ok := block.(schema.SingleNestedBlock); !ok {
			t.Errorf("%s block is %T, want schema.SingleNestedBlock", name, block)
		}
	}
	sort.Strings(names)
	wanted := append([]string(nil), expectedContentNames...)
	sort.Strings(wanted)
	if !reflect.DeepEqual(names, wanted) {
		t.Fatalf("content blocks = %v, want %v", names, wanted)
	}

	metricAttributes := []map[string]schema.Attribute{
		MetricsClusterMapSchemaAttributes(), MetricsListSchemaAttributes(),
		MetricsSingleValueSchemaAttributes(), MetricsTableSchemaAttributes(),
		MetricsTimeSeriesSchemaAttributes(),
	}
	for _, attributes := range metricAttributes {
		program, ok := attributes["program"].(schema.StringAttribute)
		if !ok || !program.Optional || program.Required {
			t.Errorf("program schema = %#v, want conditionally-required optional string", attributes["program"])
		}
	}

	clusterRange := MetricsClusterMapSchemaAttributes()["color_range"].(schema.SingleNestedAttribute)
	if !clusterRange.Attributes["palette"].(schema.StringAttribute).Required {
		t.Error("cluster color_range.palette is not required")
	}
	if _, exists := MetricsClusterMapSchemaAttributes()["display_unit"].(schema.SingleNestedAttribute).Attributes["unit"]; exists {
		t.Error("cluster display_unit unexpectedly accepts a named unit")
	}
	listFields := MetricsListSchemaAttributes()["display_fields"].(schema.ListNestedAttribute).NestedObject.Attributes
	if !listFields["property"].(schema.StringAttribute).Required || !listFields["enabled"].(schema.BoolAttribute).Required {
		t.Error("list display_fields requiredness was not generated")
	}
	tableColumns := MetricsTableSchemaAttributes()["columns"].(schema.ListNestedAttribute).NestedObject.Attributes
	if !tableColumns["field"].(schema.StringAttribute).Required {
		t.Error("table columns.field is not required")
	}
	timeProperties := MetricsTimeSeriesSchemaAttributes()["additional_properties"].(schema.ListNestedAttribute).NestedObject.Attributes
	if !timeProperties["property"].(schema.StringAttribute).Required || !timeProperties["enabled"].(schema.BoolAttribute).Required {
		t.Error("time-series additional_properties requiredness was not generated")
	}
}

func TestParseContentRoundTripsAllSix(t *testing.T) {
	tests := []struct {
		tag  string
		name string
		spec map[string]any
	}{
		{"<o11y:ClusterMap>", MetricsClusterMapName, metricSpec("<o11y:ClusterMap>", map[string]any{"colorBy": "Range", "displayUnit": map[string]any{"suffix": "%"}})},
		{"<o11y:List>", MetricsListName, metricSpec("<o11y:List>", map[string]any{"displayFields": []any{map[string]any{"property": "host", "enabled": true}}, "sort": map[string]any{"by": "value", "direction": "desc"}})},
		{"<o11y:SingleValue>", MetricsSingleValueName, metricSpec("<o11y:SingleValue>", map[string]any{"displayUnit": "Millisecond", "maximumFractionDigits": int64(2)})},
		{"<o11y:TableChart>", MetricsTableName, metricSpec("<o11y:TableChart>", map[string]any{"groupBy": []any{"service.name"}, "columns": []any{map[string]any{"field": "A", "enabled": true}}})},
		{"<o11y:TimeSeriesChart>", MetricsTimeSeriesName, metricSpec("<o11y:TimeSeriesChart>", map[string]any{"chartOptions": map[string]any{"type": "area"}, "yAxes": []any{map[string]any{"min": 0.5}}, "seriesOptions": map[string]any{"A": map[string]any{"plotType": "bar", "maximumSignificantDigits": int64(4)}}})},
		{"<o11y:Text>", TextName, map[string]any{"<o11y:Text>": []any{}, "chart": map[string]any{"markdown": "## Notes"}, "widget": map[string]any{"title": "Runbook"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := cloneForTest(t, test.spec)
			entry, metadata, err := ParseContent(test.tag, test.spec)
			if err != nil {
				t.Fatal(err)
			}
			if entry.Name != test.name || !metadata.TypedSafe() {
				t.Fatalf("entry=%q metadata=%+v", entry.Name, metadata)
			}
			if !reflect.DeepEqual(entry.Content.BuildSpec(), test.spec) {
				t.Fatalf("rebuilt spec differs\n got: %#v\nwant: %#v", entry.Content.BuildSpec(), test.spec)
			}
			if !reflect.DeepEqual(before, test.spec) {
				t.Fatal("ParseContent mutated its input")
			}
		})
	}
}

func TestSupportedElementLookupSeparatesUnknownTags(t *testing.T) {
	for _, tag := range []string{"<o11y:ClusterMap>", "<o11y:List>", "<o11y:SingleValue>", "<o11y:TableChart>", "<o11y:TimeSeriesChart>", "<o11y:Text>"} {
		if !IsSupportedElement(tag) {
			t.Errorf("%s is not supported", tag)
		}
		if _, ok := NameForElement(tag); !ok {
			t.Errorf("NameForElement(%s) failed", tag)
		}
	}
	if IsSupportedElement("<o11y:Heatmap>") {
		t.Error("unsupported heatmap reported as supported")
	}
	if _, _, err := ParseContent("<o11y:Heatmap>", map[string]any{}); err == nil {
		t.Error("ParseContent accepted unsupported element")
	}
}

func TestProgramIsRequiredAndNonEmptyAtRuntime(t *testing.T) {
	missing := metricSpec("<o11y:List>", map[string]any{})
	delete(missing["datasource"].(map[string]any), "program")
	_, metadata, err := ParseContent("<o11y:List>", missing)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TypedSafe() || !containsPath(metadata.MissingRequiredFields, "program") {
		t.Fatalf("missing program metadata = %+v", metadata)
	}

	empty := metricSpec("<o11y:List>", map[string]any{})
	empty["datasource"].(map[string]any)["program"] = ""
	_, metadata, err = ParseContent("<o11y:List>", empty)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TypedSafe() || !hasValidationPath(metadata.ValidationErrors, "program") {
		t.Fatalf("empty program metadata = %+v", metadata)
	}
}

func TestApprovedLegacyNormalizationsAreExplicitAndLossless(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		spec map[string]any
		path []string
		want any
	}{
		{"cluster maxPrecision", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"maxPrecision": int64(4)}), []string{"chart", "maximumSignificantDigits"}, int64(4)},
		{"list maxPrecision", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"maxPrecision": int64(4)}), []string{"chart", "maximumSignificantDigits"}, int64(4)},
		{"list stream unit", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"publishedStreams": map[string]any{"A": map[string]any{"unit": "Byte"}}}), []string{"chart", "publishedStreams", "A", "displayUnit"}, "Byte"},
		{"list stream prefix", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"publishedStreams": map[string]any{"A": map[string]any{"prefix": "$"}}}), []string{"chart", "publishedStreams", "A", "displayUnit"}, map[string]any{"prefix": "$"}},
		{"list stream suffix", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"publishedStreams": map[string]any{"A": map[string]any{"suffix": "%"}}}), []string{"chart", "publishedStreams", "A", "displayUnit"}, map[string]any{"suffix": "%"}},
		{"single maxPrecision", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"maxPrecision": int64(4)}), []string{"chart", "maximumSignificantDigits"}, int64(4)},
		{"single numberPrecision", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"numberPrecision": int64(2)}), []string{"chart", "maximumFractionDigits"}, int64(2)},
		{"single publish unit", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valueUnit": "Byte"}}}), []string{"chart", "displayUnit"}, "Byte"},
		{"single publish prefix", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valuePrefix": "$"}}}), []string{"chart", "displayUnit"}, map[string]any{"prefix": "$"}},
		{"single publish suffix", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valueSuffix": "%"}}}), []string{"chart", "displayUnit"}, map[string]any{"suffix": "%"}},
		{"table maxPrecision", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"maxPrecision": int64(4)}), []string{"chart", "maximumSignificantDigits"}, int64(4)},
		{"table scalar group", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"groupBy": "service.name"}), []string{"chart", "groupBy"}, []any{"service.name"}},
		{"table column unit", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"columns": []any{map[string]any{"field": "A", "unit": "Millisecond"}}}), []string{"chart", "columns", "0", "displayUnit"}, "Millisecond"},
		{"table column prefix", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"columns": []any{map[string]any{"field": "A", "prefix": "$"}}}), []string{"chart", "columns", "0", "displayUnit"}, map[string]any{"prefix": "$"}},
		{"table column suffix", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"columns": []any{map[string]any{"field": "A", "suffix": "%"}}}), []string{"chart", "columns", "0", "displayUnit"}, map[string]any{"suffix": "%"}},
		{"time axisPrecision", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"chartOptions": map[string]any{"axisPrecision": int64(3)}}), []string{"chart", "chartOptions", "maximumSignificantDigits"}, int64(3)},
		{"time series precision", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"precision": int64(3)}}}), []string{"chart", "seriesOptions", "A", "maximumSignificantDigits"}, int64(3)},
		{"time series unit", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"valueUnit": "Byte"}}}), []string{"chart", "seriesOptions", "A", "displayUnit"}, "Byte"},
		{"time series prefix", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"valuePrefix": "$"}}}), []string{"chart", "seriesOptions", "A", "displayUnit"}, map[string]any{"prefix": "$"}},
		{"time series suffix", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"valueSuffix": "%"}}}), []string{"chart", "seriesOptions", "A", "displayUnit"}, map[string]any{"suffix": "%"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, metadata, err := ParseContent(test.tag, test.spec)
			if err != nil {
				t.Fatal(err)
			}
			if !metadata.TypedSafe() || len(metadata.Normalizations) == 0 {
				t.Fatalf("metadata = %+v", metadata)
			}
			if got, ok := testPath(entry.Content.BuildSpec(), test.path); !ok || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("canonical %v = %#v (present %v), want %#v", test.path, got, ok, test.want)
			}
		})
	}

	modernWins := metricSpec("<o11y:SingleValue>", map[string]any{"maximumSignificantDigits": int64(5), "maxPrecision": int64(2)})
	entry, metadata, err := ParseContent("<o11y:SingleValue>", modernWins)
	if err != nil || !metadata.TypedSafe() {
		t.Fatalf("modern precedence metadata=%+v err=%v", metadata, err)
	}
	value, _ := testPath(entry.Content.BuildSpec(), []string{"chart", "maximumSignificantDigits"})
	if value != int64(5) {
		t.Fatalf("modern value = %#v", value)
	}
}

func TestAmbiguousLegacyDisplayUnitsRemainRaw(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		spec map[string]any
	}{
		{"list stream", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"publishedStreams": map[string]any{"A": map[string]any{"unit": "Byte", "prefix": "$"}}})},
		{"table column", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"columns": []any{map[string]any{"field": "A", "unit": "Byte", "suffix": "%"}}})},
		{"time series", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"valueUnit": "Byte", "valuePrefix": "$"}}})},
		{"single publish unit and affix", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valueUnit": "Byte", "valueSuffix": "%"}}})},
		{"single multiple publish entries", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valueSuffix": "%"}, map[string]any{"valuePrefix": "$"}}})},
		{"single unknown publish key", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"publishLabelOptions": []any{map[string]any{"valueSuffix": "%", "label": "A"}}})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, metadata, err := ParseContent(test.tag, test.spec)
			if err == nil && metadata.TypedSafe() {
				t.Fatalf("ambiguous legacy shape became typed: %+v", metadata)
			}
		})
	}
}

func TestOmittedEmptyAndNullCollectionsStayDistinct(t *testing.T) {
	omitted := metricSpec("<o11y:TimeSeriesChart>", map[string]any{})
	entry, metadata, err := ParseContent("<o11y:TimeSeriesChart>", omitted)
	if err != nil || !metadata.TypedSafe() {
		t.Fatalf("omitted metadata=%+v err=%v", metadata, err)
	}
	if _, present := testPath(entry.Content.BuildSpec(), []string{"chart", "seriesOptions"}); present {
		t.Error("omitted map was emitted")
	}

	emptyMap := metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{}})
	entry, metadata, err = ParseContent("<o11y:TimeSeriesChart>", emptyMap)
	if err != nil || !metadata.TypedSafe() {
		t.Fatalf("empty map metadata=%+v err=%v", metadata, err)
	}
	if value, present := testPath(entry.Content.BuildSpec(), []string{"chart", "seriesOptions"}); !present || len(value.(map[string]any)) != 0 {
		t.Fatalf("empty map was not preserved: %#v", value)
	}

	emptyList := metricSpec("<o11y:SingleValue>", map[string]any{"colorScale": []any{}})
	entry, metadata, err = ParseContent("<o11y:SingleValue>", emptyList)
	if err != nil || !metadata.TypedSafe() {
		t.Fatalf("empty list metadata=%+v err=%v", metadata, err)
	}
	if value, present := testPath(entry.Content.BuildSpec(), []string{"chart", "colorScale"}); !present || len(value.([]any)) != 0 {
		t.Fatalf("empty list was not preserved: %#v", value)
	}

	explicitNull := metricSpec("<o11y:SingleValue>", map[string]any{"displayUnit": nil})
	_, metadata, err = ParseContent("<o11y:SingleValue>", explicitNull)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TypedSafe() {
		t.Fatalf("explicit null was collapsed into typed state: %+v", metadata)
	}
}

func TestValidationCoversBoundsEnumsConflictsAndCrossFields(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		spec map[string]any
		path string
	}{
		{"enum", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"chartOptions": map[string]any{"type": "pie"}}), "visualization"},
		{"fraction precision below lower bound", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"maximumFractionDigits": int64(-1)}), "maximum_fraction_digits"},
		{"fraction precision above upper bound", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"maximumFractionDigits": int64(21)}), "maximum_fraction_digits"},
		{"significant precision below lower bound", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"maximumSignificantDigits": int64(0)}), "maximum_significant_digits"},
		{"significant precision above upper bound", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"maximumSignificantDigits": int64(21)}), "maximum_significant_digits"},
		{"threshold conflict", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"colorScale": []any{map[string]any{"gt": 1.0, "gte": 1.0}}}), "color_scale.0.gt"},
		{"scale requires mode", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"colorScale": []any{map[string]any{"gt": 1.0}}}), "color_scale"},
		{"scale requires nonempty", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"colorBy": "Scale", "colorScale": []any{}}), "color_scale"},
		{"cluster alternatives conflict", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"colorBy": "Scale", "colorRange": map[string]any{"palette": "cyan"}, "colorScale": []any{map[string]any{"gt": 1.0}}}), "color_range"},
		{"nested required", "<o11y:List>", metricSpec("<o11y:List>", map[string]any{"displayFields": []any{map[string]any{"enabled": true}}}), "display_fields.0.property"},
		{"time y axes maximum", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"yAxes": []any{map[string]any{}, map[string]any{}, map[string]any{}}}), "y_axes"},
		{"time series y axis maximum", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"yAxis": int64(2)}}}), "series.A.y_axis"},
		{"time series palette maximum", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"seriesOptions": map[string]any{"A": map[string]any{"paletteIndex": int64(16)}}}), "series.A.palette_index"},
		{"cluster group by maximum", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"groupBy": []any{"a", "b", "c"}}), "group_by"},
		{"cluster palette maximum", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"colorBy": "Scale", "colorScale": []any{map[string]any{"paletteIndex": int64(22)}}}), "color_scale.0.palette_index"},
		{"cluster color scale maximum", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"colorBy": "Scale", "colorScale": []any{map[string]any{}, map[string]any{}, map[string]any{}, map[string]any{}, map[string]any{}, map[string]any{}}}), "color_scale"},
		{"required link url", "<o11y:TimeSeriesChart>", metricSpecWithWidget("<o11y:TimeSeriesChart>", map[string]any{}, map[string]any{"links": []any{map[string]any{}}}), "links.0.url"},
		{"required table column field", "<o11y:TableChart>", metricSpec("<o11y:TableChart>", map[string]any{"columns": []any{map[string]any{"enabled": true}}}), "columns.0.field"},
		{"required additional property name", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"additionalProperties": []any{map[string]any{"enabled": true}}}), "additional_properties.0.property"},
		{"required additional property enabled", "<o11y:TimeSeriesChart>", metricSpec("<o11y:TimeSeriesChart>", map[string]any{"additionalProperties": []any{map[string]any{"property": "host"}}}), "additional_properties.0.enabled"},
		{"empty general display unit", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"displayUnit": map[string]any{}}), "display_unit"},
		{"empty general display prefix", "<o11y:SingleValue>", metricSpec("<o11y:SingleValue>", map[string]any{"displayUnit": map[string]any{"prefix": ""}}), "display_unit"},
		{"empty custom cluster display unit", "<o11y:ClusterMap>", metricSpec("<o11y:ClusterMap>", map[string]any{"displayUnit": map[string]any{}}), "display_unit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, metadata, err := ParseContent(test.tag, test.spec)
			if err != nil {
				// Invalid wire types may fail during the typed parse itself; these
				// cases deliberately use representable shapes and should instead
				// return path-addressable validation metadata.
				t.Fatal(err)
			}
			if metadata.TypedSafe() || (!hasValidationPath(metadata.ValidationErrors, test.path) && !containsPath(metadata.MissingRequiredFields, test.path)) {
				t.Fatalf("metadata = %+v, want path %q", metadata, test.path)
			}
		})
	}

	model := MetricsSingleValueModel{
		Program: types.StringValue("data('x').publish()"),
		DisplayUnit: &MetricsSingleValueDisplayUnitModel{
			Unit:   types.StringValue("Byte"),
			Prefix: types.StringValue("$"),
		},
	}
	if errors := model.ValidationErrors(); !hasValidationPath(errors, "display_unit.prefix") {
		t.Fatalf("oneof conflict errors = %+v", errors)
	}
}

func TestUnknownOllyShapesNeverBecomeTypedState(t *testing.T) {
	validButUncurated := map[string]any{
		"<o11y:Text>": []any{},
		"chart":       map[string]any{"markdown": "notes"},
		"widget": map[string]any{
			"links": []any{map[string]any{"url": "/runbook"}},
		},
	}
	_, metadata, err := ParseContent("<o11y:Text>", validButUncurated)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TypedSafe() || len(metadata.Leftovers) == 0 || len(metadata.ValidationErrors) != 0 {
		t.Fatalf("valid uncurated shape metadata = %+v", metadata)
	}

	emptyUnknown := map[string]any{
		"<o11y:Text>": []any{},
		"chart":       map[string]any{"markdown": "notes"},
		"widget":      map[string]any{"links": []any{}},
	}
	_, metadata, err = ParseContent("<o11y:Text>", emptyUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TypedSafe() {
		t.Fatalf("semantic comparison collapsed an unknown empty collection: %+v", metadata)
	}
}

func TestSemanticComparisonIgnoresMapOrderAndWholeNumberRepresentation(t *testing.T) {
	left := map[string]any{"b": map[string]any{"x": float64(1)}, "a": []any{"v"}}
	right := map[string]any{"a": []any{"v"}, "b": map[string]any{"x": int64(1)}}
	if !semanticallyEqualSpec(left, right) {
		t.Error("equivalent map order and whole-number representations differ")
	}
	if semanticallyEqualSpec(left, map[string]any{"b": map[string]any{"x": 1.5}, "a": []any{"v"}}) {
		t.Error("different numeric values compare equal")
	}
	for _, equivalent := range []any{json.Number("1.0"), json.Number("1e0"), float64(1), int64(1)} {
		if !semanticallyEqualSpec(map[string]any{"n": equivalent}, map[string]any{"n": int64(1)}) {
			t.Errorf("%T(%v) did not compare equal to int64(1)", equivalent, equivalent)
		}
	}
	if !semanticallyEqualSpec(map[string]any{"n": json.Number("-0")}, map[string]any{"n": int64(0)}) {
		t.Error("negative zero did not compare equal to zero")
	}
}

func TestExactJSONNumberConversionPreservesLosslessness(t *testing.T) {
	const exactLargeInteger = "9007199254740993"
	root := map[string]any{"integer": json.Number(exactLargeInteger), "exact_float": json.Number("0.125"), "inexact_float": json.Number("0.10000000000000001")}
	integer, err := TakeInt64(root, []string{"integer"}, Scalar)
	if err != nil || integer.ValueInt64() != int64(9007199254740993) {
		t.Fatalf("large integer = %v, err=%v", integer, err)
	}
	exactFloat, err := TakeFloat64(root, []string{"exact_float"}, Scalar)
	if err != nil || exactFloat.ValueFloat64() != 0.125 {
		t.Fatalf("exact float = %v, err=%v", exactFloat, err)
	}
	if _, err := TakeFloat64(root, []string{"inexact_float"}, Scalar); err == nil || !strings.Contains(err.Error(), "cannot round-trip") {
		t.Fatalf("inexact float error = %v", err)
	}
	if _, err := jsonInt64(json.Number("1.0")); err != nil {
		t.Fatalf("whole decimal rejected: %v", err)
	}
}

func TestRelativeDurationOverflowIsRejectedWithoutEmittingZeroRange(t *testing.T) {
	const overflow = "999999999999999999999999999999s"
	model := MetricsTimeSeriesModel{
		Program:   types.StringValue("data('x').publish()"),
		TimeRange: types.StringValue(overflow),
	}
	if errors := model.ValidationErrors(); !hasValidationPath(errors, "time_range") {
		t.Fatalf("overflow validation errors = %s", validationMessages(errors))
	}
	if _, present := testPath(model.BuildSpec(), []string{"datasource", "time"}); present {
		t.Fatalf("overflowing duration was emitted: %#v", model.BuildSpec())
	}

	if _, ok := RelativeDurationSpec("1h"); !ok {
		t.Error("valid duration rejected")
	}
	if _, ok := RelativeDurationSpec(overflow); ok {
		t.Error("overflowing duration converted")
	}
}

func metricSpec(tag string, chart map[string]any) map[string]any {
	return metricSpecWithWidget(tag, chart, map[string]any{})
}

func metricSpecWithWidget(tag string, chart, widget map[string]any) map[string]any {
	return map[string]any{
		tag:          []any{},
		"chart":      chart,
		"datasource": map[string]any{"program": "data('demo').publish()"},
		"widget":     widget,
	}
}

func hasValidationPath(errors []ValidationError, wanted string) bool {
	for _, validationError := range errors {
		if validationError.Path == wanted {
			return true
		}
	}
	return false
}

func containsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}

func testPath(root map[string]any, path []string) (any, bool) {
	var current any = root
	for _, segment := range path {
		switch value := current.(type) {
		case map[string]any:
			current = value[segment]
			if current == nil {
				_, exists := value[segment]
				if !exists {
					return nil, false
				}
			}
		case []any:
			if segment != "0" || len(value) == 0 {
				return nil, false
			}
			current = value[0]
		default:
			return nil, false
		}
	}
	return current, true
}

func cloneForTest(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	return cloneSpecMap(value)
}

func validationMessages(errors []ValidationError) string {
	var messages []string
	for _, validationError := range errors {
		messages = append(messages, validationError.Path+": "+validationError.Message)
	}
	return strings.Join(messages, "; ")
}

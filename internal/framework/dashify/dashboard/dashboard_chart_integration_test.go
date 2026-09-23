// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/signalfx/signalfx-go/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
)

func TestDashifyDashboardImportsAllSixChartsAsTypedBlocks(t *testing.T) {
	t.Parallel()

	entries := testDashboardChartEntries()
	children := make([]any, len(entries))
	for i, entry := range entries {
		children[i] = testDashboardChartPanel(entry.Content.BuildSpec())
	}

	parsed, diags := parseDashboardTemplate(testDashboardRecord(t, children, nil))
	require.Empty(t, diags, diags)
	require.Len(t, parsed.Container, len(entries))
	for i, expected := range entries {
		actual := parsed.Container[i].Entries()
		require.Len(t, actual, 1, "container %d", i)
		assert.Equal(t, expected.Name, actual[0].Name)
		assert.JSONEq(t, testDashboardJSON(t, expected.Content.BuildSpec()), testDashboardJSON(t, actual[0].Content.BuildSpec()))
		assert.Nil(t, parsed.Container[i].Template)
	}

	rebuilt, imports, err := buildDashboardSpec(parsed)
	require.NoError(t, err)
	assert.Empty(t, imports, "typed inline charts must not create template imports")

	var document map[string]any
	require.NoError(t, json.Unmarshal(rebuilt, &document))
	for key := range document {
		assert.NotContains(t, key, dashifyImportPrefix)
	}
	rebuiltChildren := document[dashifyDashboardElement].([]any)
	for i, expected := range entries {
		panel := rebuiltChildren[i].(map[string]any)[dashifyPanelElement].([]any)
		require.Len(t, panel, 1)
		content := panel[0].(map[string]any)
		tag, _, err := oneDashifyElement(content)
		require.NoError(t, err)
		expectedTag, _, err := oneDashifyElement(expected.Content.BuildSpec())
		require.NoError(t, err)
		assert.Equal(t, expectedTag, tag)
		assert.NotEqual(t, dashifyChartElement, tag, "writes must use the concrete chart element directly")
	}
}

func TestDashifyDashboardCanonicalizesCleanChartWrapper(t *testing.T) {
	t.Parallel()

	entry := testDashboardChartEntries()[1]
	wrapped := map[string]any{dashifyChartElement: []any{entry.Content.BuildSpec()}}
	parsed, diags := parseDashboardTemplate(testDashboardRecord(t, []any{testDashboardChartPanel(wrapped)}, nil))
	require.Empty(t, diags, diags)
	require.NotNil(t, parsed.Container[0].MetricsSingleValue)

	rebuilt, imports, err := buildDashboardSpec(parsed)
	require.NoError(t, err)
	assert.Empty(t, imports)
	var document map[string]any
	require.NoError(t, json.Unmarshal(rebuilt, &document))
	content := testDashboardPanelContent(t, document[dashifyDashboardElement].([]any)[0])
	tag, _, err := oneDashifyElement(content)
	require.NoError(t, err)
	assert.Equal(t, "<o11y:SingleValue>", tag)
	assert.NotContains(t, content, dashifyChartElement)
}

func TestDashifyDashboardPreservesUnrepresentableRecognizedChartAsRaw(t *testing.T) {
	t.Parallel()

	content := testDashboardChartEntries()[1].Content.BuildSpec()
	content["future"] = map[string]any{"preserved": true}
	parsed, diags := parseDashboardTemplate(testDashboardRecord(t, []any{testDashboardChartPanel(content)}, nil))
	require.False(t, diags.HasError(), diags)
	require.Len(t, diags.Warnings(), 1)
	assert.Equal(t, "Dashboard charts preserved as raw content", diags.Warnings()[0].Summary())
	assert.Contains(t, diags.Warnings()[0].Detail(), "future.preserved")
	require.Len(t, parsed.Container, 1)
	require.NotNil(t, parsed.Container[0].Template)
	assert.Empty(t, parsed.Container[0].Entries())
	assert.JSONEq(t, testDashboardJSON(t, content), parsed.Container[0].Template.Content.ValueString())
}

func TestDashifyDashboardPreservesJSONNumberPrecisionDuringTypedImport(t *testing.T) {
	t.Parallel()

	root := template.RootElementDashboard
	t.Run("exact int64 remains typed", func(t *testing.T) {
		t.Parallel()
		record := &template.Template{
			Type:  template.RecordType,
			Title: "Dashboard",
			Spec: json.RawMessage(`{
				"title":"Dashboard",
				"<Dashboard>":[{"<Panel>":[{
					"<o11y:SingleValue>":[],
					"chart":{},
					"datasource":{"program":"data('demo').publish()","sampleSize":9007199254740993},
					"widget":{}
				}]}]
			}`),
			Metadata: &template.Metadata{RootElement: &root},
		}

		parsed, diags := parseDashboardTemplate(record)
		require.Empty(t, diags, diags)
		require.Len(t, parsed.Container, 1)
		require.NotNil(t, parsed.Container[0].MetricsSingleValue)
		assert.Equal(t, int64(9007199254740993), parsed.Container[0].MetricsSingleValue.SampleSize.ValueInt64())
		assert.Nil(t, parsed.Container[0].Template)

		rebuilt, imports, err := buildDashboardSpec(parsed)
		require.NoError(t, err)
		assert.Empty(t, imports)
		assert.Contains(t, string(rebuilt), `"sampleSize":9007199254740993`)
	})

	t.Run("inexact float64 falls back to raw", func(t *testing.T) {
		t.Parallel()
		record := &template.Template{
			Type:  template.RecordType,
			Title: "Dashboard",
			Spec: json.RawMessage(`{
				"title":"Dashboard",
				"<Dashboard>":[{"<Panel>":[{
					"<o11y:SingleValue>":[],
					"chart":{"colorScale":[{"gt":9007199254740993}]},
					"datasource":{"program":"data('demo').publish()"},
					"widget":{}
				}]}]
			}`),
			Metadata: &template.Metadata{RootElement: &root},
		}

		parsed, diags := parseDashboardTemplate(record)
		require.False(t, diags.HasError(), diags)
		require.Len(t, diags.Warnings(), 1)
		assert.Equal(t, "Dashboard charts preserved as raw content", diags.Warnings()[0].Summary())
		require.Len(t, parsed.Container, 1)
		assert.Nil(t, parsed.Container[0].MetricsSingleValue)
		require.NotNil(t, parsed.Container[0].Template)
		assert.Contains(t, parsed.Container[0].Template.Content.ValueString(), `"gt":9007199254740993`)
	})
}

// A raw panel and a typed panel can encode the same document. MatchesModel is
// what keeps each one's chosen shape stable across refresh: it reports the
// stored document as unchanged, so Read leaves state alone instead of reparsing
// and having to guess which shape was written.
func TestDashifyDashboardMatchesModelForEitherRepresentation(t *testing.T) {
	t.Parallel()

	typedContent := &charts.TextModel{Markdown: types.StringValue("notes")}
	spec := typedContent.BuildSpec()

	typedModel := observabilityDashboardModel{
		Title:     types.StringValue("Dashboard"),
		Container: []dashifyDashboardContainerModel{{dashifyChartFields: dashifyChartFields{Text: typedContent}}},
	}
	rawModel := observabilityDashboardModel{
		Title: types.StringValue("Dashboard"),
		Container: []dashifyDashboardContainerModel{{
			Template: &dashifyTemplateModel{
				TemplateID: types.StringNull(),
				Content:    types.StringValue(testDashboardJSON(t, spec)),
			},
		}},
	}

	// The stored document is whatever the provider last wrote, so build the
	// fixture the way Create does rather than hand-assembling a spec.
	record := testDashboardRecordFromModel(t, typedModel)
	assert.True(t, MatchesModel(record, typedModel), "typed state describes this document")
	assert.True(t, MatchesModel(record, rawModel), "raw state encodes the same document")

	renamed := testDashboardRecordFromModel(t, typedModel)
	renamed.Title = "Renamed"
	assert.False(t, MatchesModel(renamed, typedModel), "a title change is real drift")

	editedModel := typedModel
	editedModel.Container = []dashifyDashboardContainerModel{{
		dashifyChartFields: dashifyChartFields{Text: &charts.TextModel{Markdown: types.StringValue("edited elsewhere")}},
	}}
	assert.False(t,
		MatchesModel(testDashboardRecordFromModel(t, editedModel), typedModel),
		"an external content edit is real drift",
	)
}

// When the document genuinely differs, the canonical parse decides the shape:
// typed when the generated parser proves it lossless, raw otherwise.
func TestDashifyDashboardCanonicalParseOnRealDrift(t *testing.T) {
	t.Parallel()

	entry := testDashboardChartEntries()[1]
	losslessSpec := entry.Content.BuildSpec()
	parsed, diags := parseDashboardTemplate(testDashboardRecord(t, []any{testDashboardChartPanel(losslessSpec)}, nil))
	require.Empty(t, diags, diags)
	assert.Len(t, parsed.Container[0].Entries(), 1, "a losslessly representable chart becomes a typed block")
	assert.Nil(t, parsed.Container[0].Template)

	unrepresentable := entry.Content.BuildSpec()
	unrepresentable["future"] = map[string]any{"notCurated": true}
	parsed, diags = parseDashboardTemplate(testDashboardRecord(t, []any{testDashboardChartPanel(unrepresentable)}, nil))
	require.False(t, diags.HasError(), diags)
	require.NotNil(t, parsed.Container[0].Template, "a chart the typed schema cannot hold stays raw")
	assert.Empty(t, parsed.Container[0].Entries())
	require.Len(t, diags.Warnings(), 1)
	assert.Contains(t, diags.Warnings()[0].Detail(), "future.notCurated")
}

func TestDashifyDashboardResolvesDirectAndCleanWrappedImports(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]map[string]any{
		"direct": {
			"<$import.widget0>": []any{},
		},
		"clean chart wrapper": {
			dashifyChartElement: []any{map[string]any{"<$import.widget0>": []any{}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed, diags := parseDashboardTemplate(testDashboardRecord(
				t,
				[]any{testDashboardChartPanel(content)},
				map[string]any{"$import:widget0": "/v2/template/chart-id"},
			))
			require.Empty(t, diags, diags)
			require.NotNil(t, parsed.Container[0].Template)
			assert.Equal(t, "chart-id", parsed.Container[0].Template.TemplateID.ValueString())

			rebuilt, imports, err := buildDashboardSpec(parsed)
			require.NoError(t, err)
			assert.Equal(t, []string{"/v2/template/chart-id"}, imports)
			var document map[string]any
			require.NoError(t, json.Unmarshal(rebuilt, &document))
			rebuiltContent := testDashboardPanelContent(t, document[dashifyDashboardElement].([]any)[0])
			tag, _, err := oneDashifyElement(rebuiltContent)
			require.NoError(t, err)
			assert.Equal(t, "<$import.widget0>", tag)
			assert.NotContains(t, rebuiltContent, dashifyChartElement)
		})
	}
}

// Refresh has to describe dashboards this provider did not write. An import
// element carrying material Terraform cannot model, or inline content holding a
// nested import, is reported as an unrepresented field rather than failing Read:
// a Read error would also block the plan, apply, and destroy that could fix it.
func TestDashifyDashboardReadsUnmodellableImportShapesWithWarning(t *testing.T) {
	t.Parallel()

	declarations := map[string]any{"$import:widget0": "/v2/template/chart-id"}
	tests := map[string]struct {
		content    map[string]any
		wantID     string
		wantRaw    bool
		wantDetail string
	}{
		"import element with a sibling property": {
			content:    map[string]any{"<$import.widget0>": []any{}, "future": true},
			wantID:     "chart-id",
			wantDetail: "future",
		},
		"import element with a non-empty argument list": {
			content:    map[string]any{"<$import.widget0>": []any{"arg"}},
			wantID:     "chart-id",
			wantDetail: "<$import.widget0>",
		},
		"nested import inside unrecognized content": {
			content:    map[string]any{"<Future>": []any{}, "nested": map[string]any{"<$import.widget0>": []any{}}},
			wantRaw:    true,
			wantDetail: "$import:widget0",
		},
		"nested import inside a dirty chart wrapper": {
			content: map[string]any{
				dashifyChartElement: []any{map[string]any{"<$import.widget0>": []any{}}},
				"future":            true,
			},
			wantRaw:    true,
			wantDetail: "$import:widget0",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed, diags := parseDashboardTemplate(testDashboardRecord(
				t,
				[]any{testDashboardChartPanel(test.content)},
				declarations,
			))
			require.False(t, diags.HasError(), diags)
			require.Len(t, parsed.Container, 1)
			require.NotNil(t, parsed.Container[0].Template)
			if test.wantRaw {
				assert.True(t, parsed.Container[0].Template.TemplateID.IsNull(), "unrecognized content stays raw")
				assert.JSONEq(t, testDashboardJSON(t, test.content), parsed.Container[0].Template.Content.ValueString())
			} else {
				assert.Equal(t, test.wantID, parsed.Container[0].Template.TemplateID.ValueString())
			}
			require.NotEmpty(t, diags.Warnings())
			assert.Contains(t, diags.Warnings()[0].Detail(), test.wantDetail)
		})
	}
}

// Writes stay strict where reads are tolerant. Raw content is written back
// verbatim, but $import: declarations are only emitted for template_id, so
// writing content that holds an import element would leave the reference
// dangling. Rejecting at write time is safe because the user can edit config;
// rejecting at read time would strand them.
func TestDashifyDashboardRejectsWritingImportElementInRawContent(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"top level": `{"<$import.widget0>":[]}`,
		"nested":    `{"<Future>":[],"nested":{"<$import.widget0>":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := buildDashboardSpec(observabilityDashboardModel{
				Title: types.StringValue("Dashboard"),
				Container: []dashifyDashboardContainerModel{{
					Template: &dashifyTemplateModel{
						TemplateID: types.StringNull(),
						Content:    types.StringValue(content),
					},
				}},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "use template_id instead")
		})
	}
}

func TestDashifyDashboardRejectsUnsafeImportShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content      map[string]any
		declarations map[string]any
		want         string
	}{
		"missing declaration": {
			content: map[string]any{"<$import.missing>": []any{}},
			want:    `import "missing" has no matching declaration`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseDashboardTemplate(testDashboardRecord(
				t,
				[]any{testDashboardChartPanel(test.content)},
				test.declarations,
			))
			require.True(t, diags.HasError(), diags)
			assert.Contains(t, diags.Errors()[0].Detail(), test.want)
		})
	}
}

func TestDashifyDashboardDoesNotCanonicalizeMalformedImportTag(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]map[string]any{
		"direct": {"<$import.widget0>trailing": []any{}},
		"wrapped": {
			dashifyChartElement: []any{map[string]any{"<$import.widget0>trailing": []any{}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed, diags := parseDashboardTemplate(testDashboardRecord(
				t,
				[]any{testDashboardChartPanel(content)},
				map[string]any{"$import:widget0>trailing": "/v2/template/chart-id"},
			))
			require.False(t, diags.HasError(), diags)
			require.NotNil(t, parsed.Container[0].Template)
			assert.True(t, parsed.Container[0].Template.TemplateID.IsNull())
			expected, err := json.Marshal(content)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), parsed.Container[0].Template.Content.ValueString())

			rebuilt, imports, err := buildDashboardSpec(parsed)
			require.NoError(t, err)
			assert.Empty(t, imports)
			var document map[string]any
			require.NoError(t, json.Unmarshal(rebuilt, &document))
			assert.Equal(t, content, testDashboardPanelContent(t, document[dashifyDashboardElement].([]any)[0]))
		})
	}
}

func TestDashifyDashboardSupportsTypedChartsAtEveryContainerLevel(t *testing.T) {
	t.Parallel()

	entries := testDashboardChartEntries()
	children := []any{
		testDashboardChartPanel(entries[5].Content.BuildSpec()),
		map[string]any{dashifySectionElement: []any{
			testDashboardChartPanel(entries[1].Content.BuildSpec()),
			map[string]any{dashifyGroupElement: []any{
				testDashboardChartPanel(entries[2].Content.BuildSpec()),
			}},
		}},
		map[string]any{dashifyGroupElement: []any{
			testDashboardChartPanel(entries[4].Content.BuildSpec()),
		}},
	}

	parsed, diags := parseDashboardTemplate(testDashboardRecord(t, children, nil))
	require.Empty(t, diags, diags)
	require.NotNil(t, parsed.Container[0].Text)
	require.NotNil(t, parsed.Container[1].Section)
	require.NotNil(t, parsed.Container[1].Section.Container[0].MetricsSingleValue)
	require.NotNil(t, parsed.Container[1].Section.Container[1].Group)
	require.NotNil(t, parsed.Container[1].Section.Container[1].Group.Container[0].MetricsList)
	require.NotNil(t, parsed.Container[2].Group)
	require.NotNil(t, parsed.Container[2].Group.Container[0].MetricsClusterMap)

	_, imports, err := buildDashboardSpec(parsed)
	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestDashifyDashboardAllSixChartsAtEveryLegalPanelLevel(t *testing.T) {
	t.Parallel()

	type placement struct {
		wrap       func(map[string]any) []any
		entries    func(observabilityDashboardModel) []charts.Entry
		panelChild func([]any) any
	}
	placements := map[string]placement{
		"root": {
			wrap: func(content map[string]any) []any {
				return []any{testDashboardChartPanel(content)}
			},
			entries: func(model observabilityDashboardModel) []charts.Entry {
				return model.Container[0].Entries()
			},
			panelChild: func(children []any) any { return children[0] },
		},
		"section": {
			wrap: func(content map[string]any) []any {
				return []any{map[string]any{
					dashifySectionElement: []any{testDashboardChartPanel(content)},
				}}
			},
			entries: func(model observabilityDashboardModel) []charts.Entry {
				return model.Container[0].Section.Container[0].Entries()
			},
			panelChild: func(children []any) any {
				return children[0].(map[string]any)[dashifySectionElement].([]any)[0]
			},
		},
		"group": {
			wrap: func(content map[string]any) []any {
				return []any{map[string]any{
					dashifyGroupElement: []any{testDashboardChartPanel(content)},
				}}
			},
			entries: func(model observabilityDashboardModel) []charts.Entry {
				return model.Container[0].Group.Container[0].Entries()
			},
			panelChild: func(children []any) any {
				return children[0].(map[string]any)[dashifyGroupElement].([]any)[0]
			},
		},
	}

	for _, entry := range testDashboardChartEntries() {
		for level, placement := range placements {
			t.Run(entry.Name+"/"+level, func(t *testing.T) {
				parsed, diags := parseDashboardTemplate(testDashboardRecord(t, placement.wrap(entry.Content.BuildSpec()), nil))
				require.Empty(t, diags, diags)
				actual := placement.entries(parsed)
				require.Len(t, actual, 1)
				assert.Equal(t, entry.Name, actual[0].Name)

				rebuilt, imports, err := buildDashboardSpec(parsed)
				require.NoError(t, err)
				assert.Empty(t, imports)
				var document map[string]any
				require.NoError(t, json.Unmarshal(rebuilt, &document))
				children := document[dashifyDashboardElement].([]any)
				content := testDashboardPanelContent(t, placement.panelChild(children))
				tag, _, err := oneDashifyElement(content)
				require.NoError(t, err)
				expectedTag, _, err := oneDashifyElement(entry.Content.BuildSpec())
				require.NoError(t, err)
				assert.Equal(t, expectedTag, tag)
				assert.NotEqual(t, dashifyChartElement, tag)

				reparsed, reparsedDiags := parseDashboardTemplate(testDashboardRecord(t, children, nil))
				require.Empty(t, reparsedDiags, reparsedDiags)
				reparsedEntries := placement.entries(reparsed)
				require.Len(t, reparsedEntries, 1)
				assert.Equal(t, entry.Name, reparsedEntries[0].Name)
			})
		}
	}
}
func TestValidateDashifyContainersIncludesGeneratedChartsInExactlyOneRule(t *testing.T) {
	t.Parallel()

	t.Run("multiple charts", func(t *testing.T) {
		t.Parallel()
		var response resource.ValidateConfigResponse
		validateDashifyContainers(
			&response,
			path.Root("container"),
			[]dashifyContainer{{Charts: []charts.Entry{
				{Name: charts.TextName, Content: &charts.TextModel{}},
				{Name: charts.MetricsListName, Content: &charts.MetricsListModel{Program: types.StringValue("program")}},
			}}},
			dashifyDashboardContainerLevel,
		)
		require.Len(t, response.Diagnostics.Errors(), 1)
		assert.Equal(t, "Invalid container content", response.Diagnostics.Errors()[0].Summary())
		withPath, ok := response.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
		require.True(t, ok)
		assert.True(t, path.Root("container").AtListIndex(0).Equal(withPath.Path()))
	})

	t.Run("chart and template", func(t *testing.T) {
		t.Parallel()
		var response resource.ValidateConfigResponse
		validateDashifyContainers(
			&response,
			path.Root("container"),
			[]dashifyContainer{{
				Charts:   []charts.Entry{{Name: charts.TextName, Content: &charts.TextModel{}}},
				Template: &dashifyTemplateModel{TemplateID: types.StringValue("template-id")},
			}},
			dashifyDashboardContainerLevel,
		)
		require.Len(t, response.Diagnostics.Errors(), 1)
		assert.Equal(t, "Invalid container content", response.Diagnostics.Errors()[0].Summary())
	})

	t.Run("conditionally required program", func(t *testing.T) {
		t.Parallel()
		var response resource.ValidateConfigResponse
		validateDashifyContainers(
			&response,
			path.Root("container"),
			[]dashifyContainer{{Charts: []charts.Entry{{
				Name:    charts.MetricsTimeSeriesName,
				Content: &charts.MetricsTimeSeriesModel{},
			}}}},
			dashifyDashboardContainerLevel,
		)
		require.Len(t, response.Diagnostics.Errors(), 1)
		assert.Equal(t, "Invalid chart configuration", response.Diagnostics.Errors()[0].Summary())
		assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "program")
		assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "must be set")
		withPath, ok := response.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
		require.True(t, ok)
		want := path.Root("container").AtListIndex(0).AtName(charts.MetricsTimeSeriesName)
		assert.True(t, want.Equal(withPath.Path()), "got %s, want %s", withPath.Path().String(), want.String())
	})

	t.Run("explicitly empty program", func(t *testing.T) {
		t.Parallel()
		var response resource.ValidateConfigResponse
		validateDashifyContainers(
			&response,
			path.Root("container"),
			[]dashifyContainer{{Charts: []charts.Entry{{
				Name: charts.MetricsTimeSeriesName,
				Content: &charts.MetricsTimeSeriesModel{
					Program: types.StringValue(""),
				},
			}}}},
			dashifyDashboardContainerLevel,
		)
		require.Len(t, response.Diagnostics.Errors(), 1)
		assert.Equal(t, "Invalid chart configuration", response.Diagnostics.Errors()[0].Summary())
		assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "program")
		assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "at least 1")
		withPath, ok := response.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
		require.True(t, ok)
		want := path.Root("container").AtListIndex(0).AtName(charts.MetricsTimeSeriesName)
		assert.True(t, want.Equal(withPath.Path()), "got %s, want %s", withPath.Path().String(), want.String())
	})
}

func testDashboardChartEntries() []charts.Entry {
	return []charts.Entry{
		{
			Name: charts.MetricsTimeSeriesName,
			Content: &charts.MetricsTimeSeriesModel{
				Program: types.StringValue(`data("demo.time").publish(label="A")`),
				Title:   types.StringValue("Time series"),
			},
		},
		{
			Name: charts.MetricsSingleValueName,
			Content: &charts.MetricsSingleValueModel{
				Program: types.StringValue(`data("demo.single").publish(label="A")`),
				Title:   types.StringValue("Single value"),
			},
		},
		{
			Name: charts.MetricsListName,
			Content: &charts.MetricsListModel{
				Program: types.StringValue(`data("demo.list").publish(label="A")`),
				Title:   types.StringValue("List"),
			},
		},
		{
			Name: charts.MetricsTableName,
			Content: &charts.MetricsTableModel{
				Program: types.StringValue(`data("demo.table").publish(label="A")`),
				Title:   types.StringValue("Table"),
			},
		},
		{
			Name: charts.MetricsClusterMapName,
			Content: &charts.MetricsClusterMapModel{
				Program: types.StringValue(`data("demo.cluster").publish(label="A")`),
				Title:   types.StringValue("Cluster map"),
			},
		},
		{
			Name: charts.TextName,
			Content: &charts.TextModel{
				Markdown: types.StringValue("# Notes"),
				Title:    types.StringValue("Text"),
			},
		},
	}
}

// testDashboardRecordFromModel builds the document the provider would have
// written for model, which is what the API stores and returns verbatim.
func testDashboardRecordFromModel(t *testing.T, model observabilityDashboardModel) *template.Template {
	t.Helper()
	spec, _, err := buildDashboardSpec(model)
	require.NoError(t, err)
	root := template.RootElementDashboard
	return &template.Template{
		Type:     template.RecordType,
		Title:    model.Title.ValueString(),
		Spec:     spec,
		Metadata: &template.Metadata{RootElement: &root},
	}
}

func testDashboardRecord(t *testing.T, children []any, properties map[string]any) *template.Template {
	t.Helper()
	spec := map[string]any{
		"title":                 "Dashboard",
		dashifyDashboardElement: children,
	}
	for key, value := range properties {
		spec[key] = value
	}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	root := template.RootElementDashboard
	return &template.Template{
		Type:     template.RecordType,
		Title:    "Dashboard",
		Spec:     raw,
		Metadata: &template.Metadata{RootElement: &root},
	}
}

func testDashboardChartPanel(content map[string]any) map[string]any {
	return map[string]any{dashifyPanelElement: []any{content}}
}

func testDashboardPanelContent(t *testing.T, raw any) map[string]any {
	t.Helper()
	container, ok := raw.(map[string]any)
	require.True(t, ok)
	panel, ok := container[dashifyPanelElement].([]any)
	require.True(t, ok)
	require.Len(t, panel, 1)
	content, ok := panel[0].(map[string]any)
	require.True(t, ok)
	return content
}

func testDashboardJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwdashify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/config"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/signalfx/signalfx-go/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/fwtest"
)

func TestResourceObservabilityChartMetadataAndSchema(t *testing.T) {
	t.Parallel()

	r := NewResourceObservabilityChart()
	var metadataResponse resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "signalfx"}, &metadataResponse)
	assert.Equal(t, "signalfx_observability_chart", metadataResponse.TypeName)

	var schemaResponse resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
	require.False(t, schemaResponse.Schema.ValidateImplementation(context.Background()).HasError())
	assert.True(t, schemaResponse.Schema.Attributes["id"].IsComputed())
	assert.True(t, schemaResponse.Schema.Attributes["title"].IsRequired())
	expectedBlocks := charts.ContentBlocks()
	assert.Len(t, schemaResponse.Schema.Blocks, len(expectedBlocks))
	for name := range expectedBlocks {
		assert.Contains(t, schemaResponse.Schema.Blocks, name)
	}
	assert.NotContains(t, schemaResponse.Schema.Attributes, "root_element")
	assert.NotContains(t, schemaResponse.Schema.Attributes, "spec")
}

func TestValidateObservabilityChart(t *testing.T) {
	t.Parallel()

	validProgram := types.StringValue("data('requests').publish()")
	tests := map[string]struct {
		model       observabilityChartModel
		expectError string
	}{
		"one valid chart": {
			model: observabilityChartModel{Fields: charts.Fields{Text: &charts.TextModel{Markdown: types.StringValue("# Notes")}}},
		},
		"no chart": {
			expectError: "exactly one typed chart block must be set",
		},
		"multiple charts": {
			model: observabilityChartModel{Fields: charts.Fields{
				Text:        &charts.TextModel{},
				MetricsList: &charts.MetricsListModel{Program: validProgram},
			}},
			expectError: "exactly one typed chart block must be set",
		},
		"missing required field": {
			model:       observabilityChartModel{Fields: charts.Fields{MetricsList: &charts.MetricsListModel{}}},
			expectError: "program: must be set",
		},
		"invalid chart value": {
			model: observabilityChartModel{Fields: charts.Fields{MetricsTimeSeries: &charts.MetricsTimeSeriesModel{
				Program:       validProgram,
				Visualization: types.StringValue("pie"),
			}}},
			expectError: "visualization: must be one of",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var response resource.ValidateConfigResponse
			validateObservabilityChart(&response, test.model)
			if test.expectError == "" {
				assert.False(t, response.Diagnostics.HasError(), response.Diagnostics)
				return
			}
			require.True(t, response.Diagnostics.HasError())
			assert.Contains(t, response.Diagnostics[0].Detail(), test.expectError)
		})
	}
}

func TestObservabilityChartContentRoundTrip(t *testing.T) {
	t.Parallel()

	model := observabilityChartModel{
		Title: types.StringValue("Runbook notes"),
		Fields: charts.Fields{Text: &charts.TextModel{
			Title:    types.StringValue("Notes"),
			Markdown: types.StringValue("# Runbook"),
		}},
	}
	write, err := observabilityChartContent(model)
	require.NoError(t, err)
	require.NotNil(t, write.Metadata.RootElement)
	assert.Equal(t, template.RootElementChart, *write.Metadata.RootElement)
	assert.JSONEq(t, `{"<Chart>":[{"<o11y:Text>":[],"chart":{"markdown":"# Runbook"},"widget":{"title":"Notes"}}]}`, string(write.Spec))

	record := &template.Template{
		ID:       "chart-id",
		Title:    write.Title,
		Spec:     write.Spec,
		Metadata: &template.Metadata{RootElement: write.Metadata.RootElement},
	}
	parsed, err := observabilityChartModelFromRecord(record)
	require.NoError(t, err)
	assert.Equal(t, "chart-id", parsed.ID.ValueString())
	assert.Equal(t, "Runbook notes", parsed.Title.ValueString())
	require.NotNil(t, parsed.Text)
	assert.Equal(t, "# Runbook", parsed.Text.Markdown.ValueString())
	assert.Equal(t, "Notes", parsed.Text.Title.ValueString())
}

func TestParseObservabilityChartSpecPreservesExactIntegers(t *testing.T) {
	t.Parallel()

	entry, err := parseObservabilityChartSpec(json.RawMessage(`{
		"<Chart>":[{
			"<o11y:TimeSeriesChart>":[],
			"chart":{},
			"datasource":{"program":"data('requests').publish()","backfillSliceCount":9007199254740993},
			"widget":{}
		}]
	}`))
	require.NoError(t, err)
	model, ok := entry.Content.(*charts.MetricsTimeSeriesModel)
	require.True(t, ok)
	assert.Equal(t, int64(9007199254740993), model.BackfillSliceCount.ValueInt64())
}

func TestParseObservabilityChartSpecRejectsUnsupportedShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		spec string
		want string
	}{
		"invalid JSON":        {`{"<Chart>":`, "invalid Chart specification"},
		"empty object":        {`{}`, "must contain only <Chart>"},
		"wrong root":          {`{"<Dashboard>":[]}`, "has no <Chart> root element"},
		"root sibling":        {`{"<Chart>":[],"future":true}`, "found 2 root properties"},
		"non-list root":       {`{"<Chart>":{}}`, "rather than a list"},
		"empty root":          {`{"<Chart>":[]}`, "exactly one chart is required"},
		"multiple children":   {`{"<Chart>":[{},{}]}`, "exactly one chart is required"},
		"non-object child":    {`{"<Chart>":[true]}`, "rather than an object"},
		"no element":          {`{"<Chart>":[{"chart":{}}]}`, "has no element key"},
		"multiple elements":   {`{"<Chart>":[{"<o11y:Text>":[],"<o11y:List>":[]} ]}`, "multiple element keys"},
		"unsupported element": {`{"<Chart>":[{"<o11y:Future>":[]}]}`, "unsupported element"},
		"unmodeled field": {`{"<Chart>":[{"<o11y:Text>":[],"chart":{"markdown":"notes","future":true},"widget":{}}]}`,
			"cannot be represented by a typed chart block"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseObservabilityChartSpec(json.RawMessage(test.spec))
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestObservabilityChartModelFromRecordRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	chartRoot := template.RootElementChart
	dashboardRoot := template.RootElementDashboard
	validSpec := json.RawMessage(`{"<Chart>":[{"<o11y:Text>":[],"chart":{"markdown":"notes"},"widget":{}}]}`)
	tests := map[string]struct {
		record *template.Template
		want   string
	}{
		"nil":          {nil, "no chart record"},
		"missing id":   {&template.Template{Metadata: &template.Metadata{RootElement: &chartRoot}, Spec: validSpec}, "without an ID"},
		"no metadata":  {&template.Template{ID: "id", Spec: validSpec}, "without root element metadata"},
		"wrong root":   {&template.Template{ID: "id", Metadata: &template.Metadata{RootElement: &dashboardRoot}, Spec: validSpec}, "want \"Chart\""},
		"invalid spec": {&template.Template{ID: "id", Metadata: &template.Metadata{RootElement: &chartRoot}, Spec: json.RawMessage(`{}`)}, "must contain only <Chart>"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := observabilityChartModelFromRecord(test.record)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestResourceObservabilityChartLifecycleAndGeneratedConfig(t *testing.T) {
	store := newTemplateAPIStore()

	testresource.UnitTest(t, testresource.TestCase{
		IsUnitTest: true,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_5_0),
		},
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			store.handlers(),
			fwtest.WithMockResources(NewResourceObservabilityChart),
		),
		Steps: []testresource.TestStep{
			{
				ConfigFile: config.StaticFile("testdata/00_observability_chart.tf"),
				Check: testresource.ComposeAggregateTestCheckFunc(
					testresource.TestCheckResourceAttrSet("signalfx_observability_chart.test", "id"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "title", "Reusable request rate"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "metrics_time_series.title", "Request rate"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "metrics_time_series.program", "data('requests.count').sum().publish(label='A')"),
				),
			},
			{
				ResourceName:    "signalfx_observability_chart.test",
				ImportState:     true,
				ImportStateKind: testresource.ImportBlockWithID,
				GenerateConfig:  true,
			},
			{
				ConfigFile: config.StaticFile("testdata/01_observability_chart_updated.tf"),
				Check: testresource.ComposeAggregateTestCheckFunc(
					testresource.TestCheckResourceAttrSet("signalfx_observability_chart.test", "id"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "title", "Reusable runbook"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "text.title", "Runbook"),
					testresource.TestCheckResourceAttr("signalfx_observability_chart.test", "text.markdown", "# Service runbook"),
				),
			},
		},
	})
}

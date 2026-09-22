// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwobservability

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/signalfx/signalfx-go/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/fwtest"
)

func TestResourceObservabilityDashboardGeneratedConfig(t *testing.T) {
	store := newTemplateAPIStore()

	resource.UnitTest(t, resource.TestCase{
		IsUnitTest: true,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_5_0),
		},
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			store.handlers(),
			fwtest.WithMockResources(NewResourceObservabilityDashboard, NewResourceObservabilityTemplate),
		),
		Steps: []resource.TestStep{
			{
				ConfigFile: config.StaticFile("testdata/observability_dashboard_layout.tf"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("signalfx_observability_dashboard.dashboard_layout", "title", "Complete layout"),
					resource.TestCheckResourceAttr("signalfx_observability_dashboard.dashboard_layout", "control_bar.time_range.default_variable_value", "-PT15M"),
					resource.TestCheckResourceAttr("signalfx_observability_dashboard.dashboard_layout", "control_bar.pinned_filter.#", "2"),
					resource.TestCheckResourceAttr("signalfx_observability_dashboard.dashboard_layout", "container.0.section.container.0.group.container.0.layout.x", `["1/4",8]`),
				),
			},
			{
				ResourceName:    "signalfx_observability_dashboard.dashboard_layout",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				GenerateConfig:  true,
			},
		},
	})
}

func TestResourceObservabilityDashboardUnitTest(t *testing.T) {
	store := newTemplateAPIStore()

	resource.UnitTest(
		t,
		resource.TestCase{
			IsUnitTest: true,
			TerraformVersionChecks: []tfversion.TerraformVersionCheck{
				tfversion.RequireAbove(tfversion.Version0_12_26),
			},
			ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
				t,
				store.handlers(),
				fwtest.WithMockResources(NewResourceObservabilityDashboard, NewResourceObservabilityTemplate),
			),
			Steps: []resource.TestStep{
				{
					ConfigFile: config.StaticFile("testdata/00_observability_dashboard.tf"),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttrSet("signalfx_observability_dashboard.test", "id"),
						resource.TestCheckResourceAttr("signalfx_observability_dashboard.test", "title", "Service overview"),
						resource.TestCheckResourceAttr("signalfx_observability_dashboard.test", "container.0.layout.width", "6/12"),
						resource.TestCheckResourceAttrPair(
							"signalfx_observability_dashboard.test", "container.0.template.template_id",
							"signalfx_observability_template.chart", "id",
						),
					),
				},
				{
					ConfigFile: config.StaticFile("testdata/01_observability_dashboard_updated.tf"),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttrSet("signalfx_observability_dashboard.test", "id"),
						resource.TestCheckResourceAttr("signalfx_observability_dashboard.test", "title", "Updated service overview"),
						resource.TestCheckNoResourceAttr("signalfx_observability_dashboard.test", "container.0.layout.width"),
						resource.TestCheckResourceAttrPair(
							"signalfx_observability_dashboard.test", "container.0.template.template_id",
							"signalfx_observability_template.chart", "id",
						),
					),
				},
			},
		},
	)
}

func TestResourceObservabilityDashboardMetadataAndSchema(t *testing.T) {
	t.Parallel()

	r := NewResourceObservabilityDashboard()
	var metadata frameworkresource.MetadataResponse
	r.Metadata(context.Background(), frameworkresource.MetadataRequest{ProviderTypeName: "signalfx"}, &metadata)
	assert.Equal(t, "signalfx_observability_dashboard", metadata.TypeName)

	var schemaResponse frameworkresource.SchemaResponse
	r.Schema(context.Background(), frameworkresource.SchemaRequest{}, &schemaResponse)
	require.False(t, schemaResponse.Schema.ValidateImplementation(context.Background()).HasError())
	assert.NotEmpty(t, schemaResponse.Schema.Description)
	assert.Contains(t, schemaResponse.Schema.Attributes, "id")
	assert.Contains(t, schemaResponse.Schema.Attributes, "title")
	assert.Contains(t, schemaResponse.Schema.Blocks, "control_bar")
	assert.Contains(t, schemaResponse.Schema.Blocks, "layout")
	assert.Contains(t, schemaResponse.Schema.Blocks, "container")
}

func TestValidateObservabilityTitle(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		value       types.String
		expectError bool
	}{
		"value":      {value: types.StringValue(" Service health ")},
		"empty":      {value: types.StringValue(""), expectError: true},
		"spaces":     {value: types.StringValue("   "), expectError: true},
		"whitespace": {value: types.StringValue("\t\n"), expectError: true},
		"null":       {value: types.StringNull(), expectError: true},
		"unknown":    {value: types.StringUnknown()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var response frameworkresource.ValidateConfigResponse
			validateObservabilityTitle(&response, path.Root("title"), test.value)
			assert.Equal(t, test.expectError, response.Diagnostics.HasError())
		})
	}
}

func TestResourceObservabilityDashboardInlineContentLifecycleAndGeneratedConfig(t *testing.T) {
	store := newTemplateAPIStore()

	resource.UnitTest(t, resource.TestCase{
		IsUnitTest: true,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_5_0),
		},
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			store.handlers(),
			fwtest.WithMockResources(NewResourceObservabilityDashboard),
		),
		Steps: []resource.TestStep{
			{
				ConfigFile: config.StaticFile("testdata/observability_dashboard_inline_content.tf"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("signalfx_observability_dashboard.inline_content", "id"),
					resource.TestCheckResourceAttr("signalfx_observability_dashboard.inline_content", "title", "Inline dashboard content"),
					resource.TestCheckResourceAttrSet("signalfx_observability_dashboard.inline_content", "container.0.template.content"),
					resource.TestCheckNoResourceAttr("signalfx_observability_dashboard.inline_content", "container.0.template.template_id"),
				),
			},
			{
				ResourceName:    "signalfx_observability_dashboard.inline_content",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				GenerateConfig:  true,
			},
		},
	})
}

func TestResourceObservabilityDashboardUntitledContainers(t *testing.T) {
	store := newTemplateAPIStore()

	resource.UnitTest(t, resource.TestCase{
		IsUnitTest: true,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_5_0),
		},
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			store.handlers(),
			fwtest.WithMockResources(NewResourceObservabilityDashboard),
		),
		Steps: []resource.TestStep{{
			Config: `
resource "signalfx_observability_dashboard" "untitled" {
  title = "Untitled containers"

  container {
    section {
      container {
        group {
          container {
            template {
              content = jsonencode({ "<Chart>" = [] })
            }
          }
        }
      }
    }
  }
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("signalfx_observability_dashboard.untitled", "container.0.section.title", ""),
				resource.TestCheckResourceAttr("signalfx_observability_dashboard.untitled", "container.0.section.container.0.group.title", ""),
			),
		}},
	})
}

func TestResourceObservabilityDashboardAllControlsConfig(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		IsUnitTest: true,
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			nil,
			fwtest.WithMockResources(NewResourceObservabilityDashboard),
		),
		Steps: []resource.TestStep{{
			ConfigFile:         config.StaticFile("testdata/observability_dashboard_controls.tf"),
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}

func TestResourceObservabilityDashboardAllSixChartLifecycle(t *testing.T) {
	store := newTemplateAPIStore()
	const resourceName = "signalfx_observability_dashboard.all_six"

	resource.UnitTest(t, resource.TestCase{
		IsUnitTest: true,
		CheckDestroy: func(_ *terraform.State) error {
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.items) != 0 {
				return fmt.Errorf("template store contains %d records after delete, want none", len(store.items))
			}
			return nil
		},
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_5_0),
		},
		ProtoV6ProviderFactories: fwtest.NewMockProto6Server(
			t,
			store.handlers(),
			fwtest.WithMockResources(NewResourceObservabilityDashboard),
		),
		Steps: []resource.TestStep{
			{
				ConfigFile: config.StaticFile("testdata/observability_dashboard_all_six.tf"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "container.#", "6"),
					resource.TestCheckResourceAttr(resourceName, "container.0.metrics_time_series.title", "Time series"),
					resource.TestCheckResourceAttr(resourceName, "container.1.metrics_single_value.title", "Single value"),
					resource.TestCheckResourceAttr(resourceName, "container.2.metrics_list.title", "List"),
					resource.TestCheckResourceAttr(resourceName, "container.3.metrics_table.title", "Table"),
					resource.TestCheckResourceAttr(resourceName, "container.4.metrics_cluster_map.title", "Cluster map"),
					resource.TestCheckResourceAttr(resourceName, "container.5.text.markdown", "# Initial notes"),
					testCheckStoredAllSixChartPayload(store, "All six generated charts", "", "# Initial notes"),
				),
			},
			{
				ConfigFile: config.StaticFile("testdata/observability_dashboard_all_six.tf"),
				PlanOnly:   true,
			},
			{
				ConfigFile: config.StaticFile("testdata/observability_dashboard_all_six_updated.tf"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "title", "All six generated charts updated"),
					resource.TestCheckResourceAttr(resourceName, "container.0.metrics_time_series.title", "Time series updated"),
					resource.TestCheckResourceAttr(resourceName, "container.5.text.markdown", "# Updated notes"),
					testCheckStoredAllSixChartPayload(store, "All six generated charts updated", ".updated", "# Updated notes"),
				),
			},
			{
				ResourceName:    resourceName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				GenerateConfig:  true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("import returned %d states, want 1", len(states))
					}
					attributes := states[0].Attributes
					for path, want := range map[string]string{
						"container.#":                             "6",
						"container.0.metrics_time_series.title":   "Time series updated",
						"container.1.metrics_single_value.title":  "Single value updated",
						"container.2.metrics_list.title":          "List updated",
						"container.3.metrics_table.title":         "Table updated",
						"container.4.metrics_cluster_map.title":   "Cluster map updated",
						"container.5.text.markdown":               "# Updated notes",
						"container.0.metrics_time_series.program": "data('demo.time.updated').publish(label='A')",
						"container.4.metrics_cluster_map.program": "data('demo.cluster.updated').publish(label='A')",
					} {
						got, ok := attributes[path]
						if !ok || got != want {
							return fmt.Errorf("import attribute %s = %q (present %t), want %q", path, got, ok, want)
						}
					}
					return nil
				},
			},
		},
	})
}

func testCheckStoredAllSixChartPayload(store *templateAPIStore, wantTitle, programSuffix, wantMarkdown string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		if len(store.items) != 1 {
			return fmt.Errorf("template store contains %d records, want 1", len(store.items))
		}
		var record *template.Template
		for _, item := range store.items {
			record = item
		}
		if record.Title != wantTitle {
			return fmt.Errorf("stored title = %q, want %q", record.Title, wantTitle)
		}
		if record.Metadata == nil || len(record.Metadata.Imports) != 0 {
			return fmt.Errorf("stored typed dashboard imports = %v, want none", record.Metadata)
		}

		var document map[string]any
		if err := json.Unmarshal(record.Spec, &document); err != nil {
			return fmt.Errorf("decode stored dashboard: %w", err)
		}
		children, hasChildren := document["<Dashboard>"].([]any)
		if !hasChildren || len(children) != 6 {
			return fmt.Errorf("stored dashboard has %d children, want 6", len(children))
		}
		wantTags := []string{
			"<o11y:TimeSeriesChart>",
			"<o11y:SingleValue>",
			"<o11y:List>",
			"<o11y:TableChart>",
			"<o11y:ClusterMap>",
			"<o11y:Text>",
		}
		wantPrograms := []string{
			"data('demo.time" + programSuffix + "').publish(label='A')",
			"data('demo.single" + programSuffix + "').publish(label='A')",
			"data('demo.list" + programSuffix + "').publish(label='A')",
			"data('demo.table" + programSuffix + "').publish(label='A')",
			"data('demo.cluster" + programSuffix + "').publish(label='A')",
		}
		for i, wantTag := range wantTags {
			container, isObject := children[i].(map[string]any)
			if !isObject {
				return fmt.Errorf("stored child %d is %T, want object", i, children[i])
			}
			panel, isPanelList := container["<Panel>"].([]any)
			if !isPanelList || len(panel) != 1 {
				return fmt.Errorf("stored child %d has invalid panel payload", i)
			}
			content, isPanelObject := panel[0].(map[string]any)
			if !isPanelObject {
				return fmt.Errorf("stored panel %d content is %T, want object", i, panel[0])
			}
			if _, wrapped := content["<Chart>"]; wrapped {
				return fmt.Errorf("stored chart %d used a deprecated Chart wrapper", i)
			}
			if _, hasTag := content[wantTag]; !hasTag {
				return fmt.Errorf("stored chart %d lacks direct %s element", i, wantTag)
			}
			if i < len(wantPrograms) {
				datasource, isDatasourceObject := content["datasource"].(map[string]any)
				if !isDatasourceObject || datasource["program"] != wantPrograms[i] {
					return fmt.Errorf("stored chart %d program = %v, want %q", i, datasource["program"], wantPrograms[i])
				}
			}
		}
		textContent := children[5].(map[string]any)["<Panel>"].([]any)[0].(map[string]any)
		chart, isChartObject := textContent["chart"].(map[string]any)
		if !isChartObject || chart["markdown"] != wantMarkdown {
			return fmt.Errorf("stored text markdown = %v, want %q", chart["markdown"], wantMarkdown)
		}
		return nil
	}
}

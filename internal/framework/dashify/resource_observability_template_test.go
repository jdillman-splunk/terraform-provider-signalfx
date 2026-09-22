// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwdashify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/config"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/signalfx/signalfx-go/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/fwtest"
)

func TestResourceObservabilityTemplateMetadataAndSchema(t *testing.T) {
	t.Parallel()

	r := NewResourceObservabilityTemplate()
	var metadataResponse resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "signalfx"}, &metadataResponse)
	assert.Equal(t, "signalfx_observability_template", metadataResponse.TypeName)
	var schemaResponse resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
	assert.True(t, schemaResponse.Schema.Attributes["title"].IsRequired())
	rootElement, ok := schemaResponse.Schema.Attributes["root_element"].(schema.StringAttribute)
	require.True(t, ok)
	assert.True(t, rootElement.IsRequired())
	require.Len(t, rootElement.PlanModifiers, 1)
	assert.True(t, schemaResponse.Schema.Attributes["spec"].IsRequired())
	metadataBlock, ok := schemaResponse.Schema.Blocks["metadata"].(schema.SingleNestedBlock)
	require.True(t, ok)
	assert.Empty(t, metadataBlock.Validators)
	assert.NotContains(t, metadataBlock.Attributes, "root_element")
	assert.True(t, metadataBlock.Attributes["imports"].IsOptional())
	datasource, ok := metadataBlock.Blocks["datasource"].(schema.SingleNestedBlock)
	require.True(t, ok)
	assert.True(t, datasource.Attributes["type"].IsOptional())
	assert.True(t, datasource.Attributes["program_text"].IsOptional())
	assert.True(t, datasource.Attributes["slo_id"].IsOptional())
	assert.NotContains(t, schemaResponse.Schema.Attributes, "template_contents")
	assert.NotContains(t, schemaResponse.Schema.Attributes, "imports")
}

func TestResourceObservabilityTemplateRootElementValidation(t *testing.T) {
	t.Parallel()

	r := NewResourceObservabilityTemplate()
	var schemaResponse resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
	attribute, ok := schemaResponse.Schema.Attributes["root_element"].(schema.StringAttribute)
	require.True(t, ok)
	require.Len(t, attribute.Validators, 1)

	for name, test := range map[string]struct {
		value       string
		expectError bool
	}{
		"chart":                  {value: "Chart"},
		"dashboard":              {value: "Dashboard"},
		"arbitrary root element": {value: "FutureRootElement"},
		"empty":                  {value: "", expectError: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := validator.StringRequest{
				Path:           path.Root("root_element"),
				PathExpression: path.MatchRoot("root_element"),
				ConfigValue:    types.StringValue(test.value),
			}
			response := validator.StringResponse{}
			attribute.Validators[0].ValidateString(context.Background(), request, &response)
			assert.Equal(t, test.expectError, response.Diagnostics.HasError())
		})
	}
}

func TestObservabilityTemplateDatasourceValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		root         string
		datasource   observabilityTemplateDatasourceModel
		expectErrors int
	}{
		"chart datasource": {
			root: "Chart",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringValue(string(template.DatasourceTypeSplunkObservability)),
				ProgramText: types.StringValue("data('requests').publish()"),
				SLOID:       types.StringNull(),
			},
		},
		"chart program without type": {
			root: "Chart",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringNull(),
				ProgramText: types.StringValue("data('requests').publish()"),
				SLOID:       types.StringNull(),
			},
			expectErrors: 1,
		},
		"dashboard type without program": {
			root: "dashboard",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringValue(string(template.DatasourceTypeSplunkObservability)),
				ProgramText: types.StringNull(),
				SLOID:       types.StringNull(),
			},
			expectErrors: 1,
		},
		"SLO datasource": {
			root: "Chart",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringValue(string(template.DatasourceTypeSplunkObservabilitySLO)),
				ProgramText: types.StringValue("data('service.level').publish()"),
				SLOID:       types.StringValue("example-slo-id"),
			},
		},
		"SLO datasource without ID": {
			root: "Chart",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringValue(string(template.DatasourceTypeSplunkObservabilitySLO)),
				ProgramText: types.StringValue("data('service.level').publish()"),
				SLOID:       types.StringNull(),
			},
			expectErrors: 1,
		},
		"custom root follows extensible API validation": {
			root: "FutureRootElement",
			datasource: observabilityTemplateDatasourceModel{
				Type:        types.StringNull(),
				ProgramText: types.StringValue("future datasource contents"),
				SLOID:       types.StringNull(),
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			response := resource.ValidateConfigResponse{}
			validateObservabilityTemplateDatasource(types.StringValue(test.root), test.datasource, &response)
			assert.Len(t, response.Diagnostics, test.expectErrors)
		})
	}
}

func TestObservabilityTemplateSpecValidation(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateObservabilityTemplateSpec(`{"<Chart>":[]}`))
	assert.Error(t, validateObservabilityTemplateSpec(`{"<Chart>":`))
	assert.Error(t, validateObservabilityTemplateSpec(`null`))
	assert.Error(t, validateObservabilityTemplateSpec(`[]`))
}

func TestResourceObservabilityTemplateLifecycleAndGeneratedConfig(t *testing.T) {
	store := newTemplateAPIStore()

	testresource.UnitTest(
		t,
		testresource.TestCase{
			IsUnitTest: true,
			TerraformVersionChecks: []tfversion.TerraformVersionCheck{
				tfversion.SkipBelow(tfversion.Version1_5_0),
			},
			ProtoV5ProviderFactories: fwtest.NewMockProto5Server(
				t,
				store.handlers(),
				fwtest.WithMockResources(NewResourceObservabilityTemplate),
			),
			Steps: []testresource.TestStep{
				{
					ConfigFile: config.StaticFile("testdata/00_observability_template.tf"),
					Check: testresource.ComposeAggregateTestCheckFunc(
						testresource.TestCheckResourceAttrSet("signalfx_observability_template.test", "id"),
						testresource.TestCheckResourceAttr("signalfx_observability_template.test", "title", "Request rate"),
						testresource.TestCheckResourceAttr("signalfx_observability_template.test", "root_element", "Chart"),
						testresource.TestCheckNoResourceAttr("signalfx_observability_template.test", "metadata"),
					),
				},
				{
					ResourceName:    "signalfx_observability_template.test",
					ImportState:     true,
					ImportStateKind: testresource.ImportBlockWithID,
					GenerateConfig:  true,
				},
				{
					ConfigFile: config.StaticFile("testdata/01_observability_template_updated.tf"),
					Check: testresource.ComposeAggregateTestCheckFunc(
						testresource.TestCheckResourceAttrSet("signalfx_observability_template.test", "id"),
						testresource.TestCheckResourceAttr("signalfx_observability_template.test", "title", "Request rate (updated)"),
						testresource.TestCheckResourceAttr("signalfx_observability_template.test", "root_element", "Chart"),
						testresource.TestCheckResourceAttr("signalfx_observability_template.test", "metadata.imports.0", "/v2/template/shared"),
					),
				},
			},
		},
	)
}

func TestObservabilityTemplateResourceModelMapping(t *testing.T) {
	ctx := context.Background()
	imports, diags := types.ListValueFrom(ctx, types.StringType, []string{"/v2/template/chart"})
	require.False(t, diags.HasError())
	datasource := &observabilityTemplateDatasourceModel{
		Type:        types.StringValue(string(template.DatasourceTypeSplunkObservability)),
		ProgramText: types.StringValue("data('requests').publish()"),
		SLOID:       types.StringNull(),
	}
	model := observabilityTemplateModel{
		Title:       types.StringValue("Example"),
		RootElement: types.StringValue(string(template.RootElementChart)),
		Spec:        types.StringValue(`{"<Chart>":[]}`),
		Metadata: &observabilityTemplateMetadataModel{
			Imports:    imports,
			Datasource: datasource,
		},
	}

	write, diags := observabilityTemplateWrite(ctx, model)
	require.False(t, diags.HasError(), diags)
	assert.Equal(t, template.RecordType, write.Type)
	assert.Equal(t, "Example", write.Title)
	assert.JSONEq(t, `{"<Chart>":[]}`, string(write.Spec))
	assert.Equal(t, []string{"/v2/template/chart"}, write.Metadata.Imports)
	require.NotNil(t, write.Metadata.Datasource)
	assert.Equal(t, template.DatasourceTypeSplunkObservability, write.Metadata.Datasource.Type)

	root := template.RootElementChart
	record := &template.Template{
		ID:       "template-id",
		Title:    "Example from API",
		Spec:     json.RawMessage(`{"<Chart>":[{"future":true}]}`),
		Metadata: &template.Metadata{RootElement: &root, Imports: []string{"/v2/template/chart"}},
	}
	state, err := observabilityTemplateModelFromRecord(model, record)
	require.NoError(t, err)
	assert.Equal(t, "template-id", state.ID.ValueString())
	assert.Equal(t, "Example from API", state.Title.ValueString())
	assert.Equal(t, string(template.RootElementChart), state.RootElement.ValueString())
	assert.JSONEq(t, `{"<Chart>":[{"future":true}]}`, state.Spec.ValueString())
	require.NotNil(t, state.Metadata)
	assert.Equal(t, imports, state.Metadata.Imports)
	assert.Same(t, datasource, state.Metadata.Datasource)
}

func TestObservabilityTemplateModelFromRecordLeavesOptionalMetadataUnsetOnImport(t *testing.T) {
	root := template.RootElement("FutureRootElement")
	record := &template.Template{
		ID:       "template-id",
		Title:    "Imported template",
		Spec:     json.RawMessage(`{"<FutureRootElement>":[]}`),
		Metadata: &template.Metadata{RootElement: &root, Imports: []string{"/v2/template/child"}},
	}

	state, err := observabilityTemplateModelFromRecord(observabilityTemplateModel{}, record)
	require.NoError(t, err)
	assert.Equal(t, "FutureRootElement", state.RootElement.ValueString())
	assert.Nil(t, state.Metadata)
}

func TestObservabilityTemplateModelFromRecordPreservesUnsetDirectImports(t *testing.T) {
	root := template.RootElementChart
	prior := observabilityTemplateModel{
		Metadata: &observabilityTemplateMetadataModel{
			Imports: types.ListNull(types.StringType),
		},
	}
	record := &template.Template{
		ID:       "template-id",
		Title:    "Template with descendants",
		Spec:     json.RawMessage(`{"<Chart>":[]}`),
		Metadata: &template.Metadata{RootElement: &root, Imports: []string{"/v2/template/direct", "/v2/template/descendant"}},
	}

	state, err := observabilityTemplateModelFromRecord(prior, record)
	require.NoError(t, err)
	require.NotNil(t, state.Metadata)
	assert.True(t, state.Metadata.Imports.IsNull())
}

func TestObservabilityTemplateWriteWithoutOptionalMetadata(t *testing.T) {
	model := observabilityTemplateModel{
		Title:       types.StringValue("Example"),
		RootElement: types.StringValue(string(template.RootElementChart)),
		Spec:        types.StringValue(`{"<Chart>":[]}`),
	}

	write, diags := observabilityTemplateWrite(context.Background(), model)
	require.False(t, diags.HasError(), diags)
	require.NotNil(t, write.Metadata.RootElement)
	assert.Equal(t, template.RootElementChart, *write.Metadata.RootElement)
	assert.Empty(t, write.Metadata.Imports)
	assert.Nil(t, write.Metadata.Datasource)
}

// templateAPIStore is a minimal in-memory fake of the Template API shared by
// the Template and Dashboard resource lifecycle tests.
type templateAPIStore struct {
	mu    sync.Mutex
	next  int
	items map[string]*template.Template
}

func newTemplateAPIStore() *templateAPIStore {
	return &templateAPIStore{items: make(map[string]*template.Template)}
}

func (s *templateAPIStore) handlers() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /v2/template":        http.HandlerFunc(s.create),
		"GET /v2/template/{id}":    http.HandlerFunc(s.read),
		"PUT /v2/template/{id}":    http.HandlerFunc(s.update),
		"DELETE /v2/template/{id}": http.HandlerFunc(s.delete),
	}
}

func (s *templateAPIStore) create(w http.ResponseWriter, r *http.Request) {
	var write template.Content
	if err := json.NewDecoder(r.Body).Decode(&write); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.next++
	record := &template.Template{
		ID:       fmt.Sprintf("template-%d", s.next),
		Type:     write.Type,
		Title:    write.Title,
		Spec:     write.Spec,
		Metadata: &template.Metadata{RootElement: write.Metadata.RootElement, Imports: write.Metadata.Imports},
	}
	s.items[record.ID] = record
	s.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(template.Result{Data: record})
}

func (s *templateAPIStore) read(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	record, ok := s.items[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(template.Result{Data: record})
}

func (s *templateAPIStore) update(w http.ResponseWriter, r *http.Request) {
	var write template.Content
	if err := json.NewDecoder(r.Body).Decode(&write); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	record, ok := s.items[r.PathValue("id")]
	if ok {
		record.Type = write.Type
		record.Title = write.Title
		record.Spec = write.Spec
		record.Metadata = &template.Metadata{RootElement: write.Metadata.RootElement, Imports: write.Metadata.Imports}
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(template.Result{Data: record})
}

func (s *templateAPIStore) delete(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	delete(s.items, r.PathValue("id"))
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

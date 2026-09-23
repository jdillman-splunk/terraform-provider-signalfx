// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwdashify

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
	fwembed "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/embed"
	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/fwerr"
	fwshared "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/shared"
)

type observabilityChartResource struct {
	fwembed.ResourceData
	fwembed.ResourceIDImporter
}

var (
	_ resource.Resource                = (*observabilityChartResource)(nil)
	_ resource.ResourceWithConfigure   = (*observabilityChartResource)(nil)
	_ resource.ResourceWithImportState = (*observabilityChartResource)(nil)
)

func NewResourceObservabilityChart() resource.Resource {
	return &observabilityChartResource{}
}

func (r *observabilityChartResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_observability_chart"
}

func (r *observabilityChartResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.ResourceData.Configure(ctx, req, resp)
}

func (r *observabilityChartResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a reusable Observability Chart Template using exactly one generated typed chart block. Use signalfx_observability_template for unsupported or raw Chart content.",
		Attributes: map[string]schema.Attribute{
			"id": fwshared.ResourceIDAttribute(),
			"title": schema.StringAttribute{
				Required:    true,
				Description: "Reusable Chart Template title.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		},
		Blocks: charts.ContentBlocks(),
	}
}

func (r *observabilityChartResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model observabilityChartModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateObservabilityTitle(resp, path.Root("title"), model.Title)
	validateObservabilityChart(resp, model)
}

func validateObservabilityChart(resp *resource.ValidateConfigResponse, model observabilityChartModel) {
	entries := model.Entries()
	if len(entries) != 1 {
		resp.Diagnostics.AddError(
			"Invalid chart content",
			fmt.Sprintf("exactly one typed chart block must be set, got %d", len(entries)),
		)
		return
	}
	entry := entries[0]
	chartPath := path.Root(entry.Name)
	for _, field := range entry.Content.MissingRequiredFields() {
		resp.Diagnostics.AddAttributeError(
			chartPath,
			"Invalid chart configuration",
			field+": must be set",
		)
	}
	validationErrors := append([]charts.ValidationError(nil), entry.Content.ValidationErrors()...)
	sort.SliceStable(validationErrors, func(i, j int) bool {
		if validationErrors[i].Path != validationErrors[j].Path {
			return validationErrors[i].Path < validationErrors[j].Path
		}
		return validationErrors[i].Message < validationErrors[j].Message
	})
	for _, validationErr := range validationErrors {
		detail := validationErr.Message
		if validationErr.Path != "" {
			detail = validationErr.Path + ": " + detail
		}
		resp.Diagnostics.AddAttributeError(
			chartPath,
			"Invalid chart configuration",
			detail,
		)
	}
}

func (r *observabilityChartResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model observabilityChartModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	write, err := observabilityChartContent(model)
	if err != nil {
		resp.Diagnostics.AddError("Error creating chart", err.Error())
		return
	}
	result, err := r.Details().Client.CreateTemplate(ctx, write)
	if resp.Diagnostics.Append(fwerr.ErrorHandler(ctx, resp.State, err)...); resp.Diagnostics.HasError() || err != nil {
		return
	}
	record, err := observabilityTemplateFromResult(result)
	if err != nil {
		resp.Diagnostics.AddError("Error creating chart", err.Error())
		return
	}
	model.ID = types.StringValue(record.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *observabilityChartResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state observabilityChartModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, err := r.Details().Client.GetTemplate(ctx, state.ID.ValueString(), nil)
	if resp.Diagnostics.Append(fwerr.ErrorHandler(ctx, resp.State, err)...); resp.Diagnostics.HasError() || err != nil {
		return
	}
	record, err := observabilityTemplateFromResult(result)
	if err != nil {
		resp.Diagnostics.AddError("Error reading chart", err.Error())
		return
	}
	model, err := observabilityChartModelFromRecord(record)
	if err != nil {
		resp.Diagnostics.AddError("Unsupported Chart Template", err.Error())
		return
	}
	model.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *observabilityChartResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model observabilityChartModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state observabilityChartModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	model.ID = state.ID
	write, err := observabilityChartContent(model)
	if err != nil {
		resp.Diagnostics.AddError("Error updating chart", err.Error())
		return
	}
	result, err := r.Details().Client.UpdateTemplate(ctx, model.ID.ValueString(), write)
	if resp.Diagnostics.Append(fwerr.ErrorHandler(ctx, resp.State, err)...); resp.Diagnostics.HasError() || err != nil {
		return
	}
	record, err := observabilityTemplateFromResult(result)
	if err != nil {
		resp.Diagnostics.AddError("Error updating chart", err.Error())
		return
	}
	model.ID = types.StringValue(record.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *observabilityChartResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state observabilityChartModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(fwerr.ErrorHandler(ctx, resp.State, r.Details().Client.DeleteTemplate(ctx, state.ID.ValueString()))...)
}

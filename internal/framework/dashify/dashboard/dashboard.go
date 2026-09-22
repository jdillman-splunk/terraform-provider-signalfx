// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

// This file contains the Terraform-facing dashboard schema and validation
// facade plus private Dashify support. The resource lifecycle lives in the
// parent observability package.

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/signalfx/signalfx-go/template"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
	fwshared "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/shared"
)

// Blocks returns the nested dashboard schema owned by this package. The
// parent observability package supplies the resource-level attributes.
func Blocks() map[string]schema.Block {
	return map[string]schema.Block{
		"control_bar": dashifyControlBarBlock(),
		"layout":      dashifyLayoutOptionsBlock(),
		"container": schema.ListNestedBlock{
			Description: "Ordered dashboard contents. Terraform declaration order is authoritative.",
			NestedObject: schema.NestedBlockObject{
				Blocks: dashifyContainerBlocks(dashifyDashboardContainerLevel),
			},
		},
	}
}

// dashifyContainerLevelRule is the single source of truth for which
// content blocks a container may hold at each nesting level: it drives
// schema block registration, the ValidateConfig one-of check, and the
// resulting error message, so the three stay in sync by construction
// instead of via three hand-maintained switches.
type dashifyContainerLevelRule struct {
	subject      string
	allowSection bool
	allowGroup   bool
}

var dashifyContainerLevelRules = map[dashifyContainerLevel]dashifyContainerLevelRule{
	dashifyDashboardContainerLevel: {
		subject:      "each dashboard container",
		allowSection: true,
		allowGroup:   true,
	},
	dashifySectionContainerLevel: {
		subject:    "each container within a section",
		allowGroup: true,
	},
	dashifyGroupContainerLevel: {
		subject: "each container within a group",
	},
}

func dashifyContainerBlocks(level dashifyContainerLevel) map[string]schema.Block {
	blocks := charts.ContentBlocks()
	blocks["layout"] = dashifyItemLayoutBlock()
	blocks["template"] = schema.SingleNestedBlock{
		Description: "Dashboard content supplied by either a reusable Observability Template reference or a raw inline dashboard JSON object.",
		Attributes: map[string]schema.Attribute{
			"template_id": schema.StringAttribute{Optional: true, Description: "ID of the referenced Template."},
			"content": schema.StringAttribute{
				Optional:    true,
				Description: "Self-contained dashboard JSON object rendered inline. Exactly one of content or template_id must be set.",
				PlanModifiers: []planmodifier.String{
					fwshared.JSONSemanticEqualityModifier{},
				},
			},
		},
	}
	rule := dashifyContainerLevelRules[level]
	if rule.allowSection {
		blocks["section"] = schema.SingleNestedBlock{
			Description: "A section containing containers and optional groups.",
			Attributes: map[string]schema.Attribute{
				"title": schema.StringAttribute{
					Optional:    true,
					Computed:    true,
					Default:     stringdefault.StaticString(""),
					Description: "Optional section title. Omit it for an untitled section.",
				},
				"collapse":    schema.BoolAttribute{Optional: true, Description: "Whether the section is currently collapsed."},
				"collapsible": schema.BoolAttribute{Optional: true, Description: "Whether the section can be collapsed."},
			},
			Blocks: map[string]schema.Block{
				"layout": dashifyLayoutOptionsBlock(),
				"container": schema.ListNestedBlock{
					Description: "Ordered contents of this section.",
					NestedObject: schema.NestedBlockObject{
						Blocks: dashifyContainerBlocks(dashifySectionContainerLevel),
					},
				},
			},
		}
	}
	if rule.allowGroup {
		blocks["group"] = schema.SingleNestedBlock{
			Description: "A group containing related containers.",
			Attributes: map[string]schema.Attribute{
				"title": schema.StringAttribute{
					Optional:    true,
					Computed:    true,
					Default:     stringdefault.StaticString(""),
					Description: "Optional group title. Omit it for an untitled group.",
				},
				"headerless": schema.BoolAttribute{Optional: true, Description: "Whether to hide the group header."},
			},
			Blocks: map[string]schema.Block{
				"layout": dashifyLayoutOptionsBlock(),
				"container": schema.ListNestedBlock{
					Description: "Ordered contents of this group.",
					NestedObject: schema.NestedBlockObject{
						Blocks: dashifyContainerBlocks(dashifyGroupContainerLevel),
					},
				},
			},
		}
	}
	return blocks
}

func dashifyItemLayoutBlock() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		Description: "Placement and size of this container inside its parent layout. Lengths accept numbers or relative strings; clamped values and coordinate arrays can be supplied with jsonencode.",
		Attributes: map[string]schema.Attribute{
			"absolute":   schema.BoolAttribute{Optional: true, Description: "Whether to position the container independently using its x and y coordinates."},
			"width":      dashifyLayoutLengthAttribute("Starting width of the container."),
			"height":     dashifyLayoutLengthAttribute("Starting height of the container."),
			"min_width":  dashifyLayoutLengthAttribute("Minimum width of the container."),
			"max_width":  dashifyLayoutLengthAttribute("Maximum width of the container."),
			"min_height": dashifyLayoutLengthAttribute("Minimum height of the container."),
			"max_height": dashifyLayoutLengthAttribute("Maximum height of the container."),
			"x":          dashifyLayoutLengthAttribute("Horizontal coordinate. A jsonencoded array is treated as a sum of lengths."),
			"y":          dashifyLayoutLengthAttribute("Vertical coordinate. A jsonencoded array is treated as a sum of lengths."),
		},
	}
}

func dashifyLayoutOptionsBlock() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		Description: "Settings for the layout that arranges this level's containers.",
		Attributes: map[string]schema.Attribute{
			"gap": schema.Float64Attribute{
				Optional:    true,
				Description: "Visual gap between adjacent containers in pixels.",
				Validators:  []validator.Float64{float64validator.AtLeast(0)},
			},
			"step": schema.Float64Attribute{
				Optional:    true,
				Description: "Layout resolution in pixels. Lengths are rounded to multiples of this value.",
				Validators:  []validator.Float64{float64validator.AtLeast(1)},
			},
		},
		Blocks: map[string]schema.Block{
			"defaults": schema.SingleNestedBlock{
				Description: "Default placement and size constraints inherited by every container in this layout.",
				Attributes:  dashifyLayoutDefaultAttributes(),
			},
		},
	}
}

func dashifyLayoutDefaultAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"absolute":   schema.BoolAttribute{Optional: true, Description: "Default absolute-positioning behavior."},
		"width":      dashifyLayoutLengthAttribute("Default starting width."),
		"height":     dashifyLayoutLengthAttribute("Default starting height."),
		"min_width":  dashifyLayoutLengthAttribute("Default minimum width."),
		"max_width":  dashifyLayoutLengthAttribute("Default maximum width."),
		"min_height": dashifyLayoutLengthAttribute("Default minimum height."),
		"max_height": dashifyLayoutLengthAttribute("Default maximum height."),
	}
}

func dashifyLayoutLengthAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{Optional: true, Description: description}
}

// ValidateConfig applies dashboard-specific validation after the parent
// resource has decoded the Terraform configuration.
func ValidateConfig(resp *resource.ValidateConfigResponse, model Model) {
	validateDashifyControlBar(resp, path.Root("control_bar"), model.ControlBar)
	validateDashifyLayoutOptions(resp, path.Root("layout"), model.Layout)
	validateDashifyContainers(
		resp,
		path.Root("container"),
		dashifyContainersFromDashboardModels(model.Container),
		dashifyDashboardContainerLevel,
	)
}

func validateDashifyContainers(resp *resource.ValidateConfigResponse, base path.Path, containers []dashifyContainer, level dashifyContainerLevel) {
	for i, container := range containers {
		containerPath := base.AtListIndex(i)
		validateDashifyLayout(resp, containerPath.AtName("layout"), container.Layout)

		contentCount := len(container.Charts)
		if container.Template != nil {
			contentCount++
		}
		if container.Section != nil {
			contentCount++
		}
		if container.Group != nil {
			contentCount++
		}
		if contentCount != 1 || !dashifyContainerContentAllowed(container, level) {
			resp.Diagnostics.AddAttributeError(containerPath, "Invalid container content", dashifyContainerContentError(level))
			continue
		}

		if container.Template != nil {
			validateDashifyTemplate(resp, containerPath, container.Template)
		}
		for _, entry := range container.Charts {
			validateDashifyChart(resp, containerPath, entry)
		}
		if container.Section != nil {
			sectionPath := containerPath.AtName("section")
			validateDashifyLayoutOptions(resp, sectionPath.AtName("layout"), container.Section.Layout)
			if len(container.Section.Container) == 0 {
				resp.Diagnostics.AddAttributeError(sectionPath, "Empty section", "section must contain at least one container")
			}
			validateDashifyContainers(resp, sectionPath.AtName("container"), container.Section.Container, dashifySectionContainerLevel)
		}
		if container.Group != nil {
			groupPath := containerPath.AtName("group")
			validateDashifyLayoutOptions(resp, groupPath.AtName("layout"), container.Group.Layout)
			if len(container.Group.Container) == 0 {
				resp.Diagnostics.AddAttributeError(groupPath, "Empty group", "group must contain at least one container")
			}
			validateDashifyContainers(resp, groupPath.AtName("container"), container.Group.Container, dashifyGroupContainerLevel)
		}
	}
}

func dashifyContainerContentAllowed(container dashifyContainer, level dashifyContainerLevel) bool {
	rule := dashifyContainerLevelRules[level]
	return len(container.Charts) > 0 || container.Template != nil ||
		(container.Section != nil && rule.allowSection) ||
		(container.Group != nil && rule.allowGroup)
}

func dashifyContainerContentError(level dashifyContainerLevel) string {
	rule, ok := dashifyContainerLevelRules[level]
	if !ok {
		return "each container must set exactly one supported content block"
	}
	chartNames := make([]string, 0, len(charts.ContentBlocks()))
	for name := range charts.ContentBlocks() {
		chartNames = append(chartNames, name)
	}
	sort.Strings(chartNames)
	content := append([]string{"template"}, chartNames...)
	if rule.allowSection {
		content = append(content, "section")
	}
	if rule.allowGroup {
		content = append(content, "group")
	}
	return fmt.Sprintf("%s must set exactly one content block: %s", rule.subject, joinWithOr(content))
}

func joinWithOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
	}
}

func validateDashifyChart(resp *resource.ValidateConfigResponse, containerPath path.Path, entry charts.Entry) {
	chartPath := containerPath.AtName(entry.Name)
	errors := append([]charts.ValidationError(nil), entry.Content.ValidationErrors()...)
	sort.SliceStable(errors, func(i, j int) bool {
		if errors[i].Path != errors[j].Path {
			return errors[i].Path < errors[j].Path
		}
		return errors[i].Message < errors[j].Message
	})
	for _, validationErr := range errors {
		resp.Diagnostics.AddAttributeError(
			dashifyChartValidationPath(chartPath, validationErr.Path, dashifyChartMapKeys(entry.Content)),
			"Invalid chart configuration",
			validationErr.Message,
		)
	}
}

// dashifyChartMapKeys reports the keys set on every map-typed attribute of a
// chart model, keyed by tfsdk attribute name. Reflection keeps this in step with
// the generated chart schemas: a map attribute added to charts/schemas/*.yml
// cannot fall out of validation paths the way a hand-listed set would. Keys sort
// longest-first so a dotted key wins over a shorter key that prefixes it.
func dashifyChartMapKeys(content charts.Content) map[string][]string {
	keys := map[string][]string{}
	collectDashifyMapKeys(reflect.ValueOf(content), keys)
	for _, values := range keys {
		sort.Slice(values, func(i, j int) bool {
			if len(values[i]) != len(values[j]) {
				return len(values[i]) > len(values[j])
			}
			return values[i] < values[j]
		})
	}
	return keys
}

func collectDashifyMapKeys(value reflect.Value, keys map[string][]string) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			collectDashifyMapKeys(value.Elem(), keys)
		}
	case reflect.Struct:
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			name, tagged := field.Tag.Lookup("tfsdk")
			if entry := value.Field(i); tagged && entry.Kind() == reflect.Map {
				for _, key := range entry.MapKeys() {
					keys[name] = append(keys[name], key.String())
				}
			}
			collectDashifyMapKeys(value.Field(i), keys)
		}
	case reflect.Slice:
		for i := range value.Len() {
			collectDashifyMapKeys(value.Index(i), keys)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			collectDashifyMapKeys(value.MapIndex(key), keys)
		}
	}
}

// dashifyChartValidationPath turns a chart-relative validation path such as
// "series.latency.p99.y_axis" into a schema path. Generated code joins map keys
// with the same "." it uses between attributes, so mapKeys supplies the keys
// actually present to resolve where a key ends.
func dashifyChartValidationPath(base path.Path, relative string, mapKeys map[string][]string) path.Path {
	current := base
	remaining := relative
	for remaining != "" {
		segment, rest, _ := strings.Cut(remaining, ".")
		if index, err := strconv.Atoi(segment); err == nil {
			current = current.AtListIndex(index)
			remaining = rest
			continue
		}
		current = current.AtName(segment)
		remaining = rest
		for _, key := range mapKeys[segment] {
			if remaining == key || strings.HasPrefix(remaining, key+".") {
				current = current.AtMapKey(key)
				remaining = strings.TrimPrefix(strings.TrimPrefix(remaining, key), ".")
				break
			}
		}
	}
	return current
}

func validateDashifyLayout(resp *resource.ValidateConfigResponse, layoutPath path.Path, layout *dashifyLayoutModel) {
	if layout == nil {
		return
	}
	if dashifyLayoutIsEmpty(layout) {
		resp.Diagnostics.AddAttributeError(layoutPath, "Empty layout block", "layout must set at least one placement or size option")
		return
	}
	for _, field := range dashifyLayoutModelFields(layout) {
		validateDashifyLayoutValue(resp, layoutPath.AtName(field.name), *field.value, field.coordinate)
	}
}

func validateDashifyLayoutOptions(resp *resource.ValidateConfigResponse, layoutPath path.Path, layout *dashifyLayoutOptionsModel) {
	if layout == nil {
		return
	}
	if layout.Gap.IsNull() && layout.Step.IsNull() && layout.Defaults == nil {
		resp.Diagnostics.AddAttributeError(layoutPath, "Empty layout block", "layout must set gap, step, defaults, or a combination")
		return
	}
	if layout.Defaults == nil {
		return
	}
	defaultsPath := layoutPath.AtName("defaults")
	if dashifyLayoutDefaultsAreEmpty(layout.Defaults) {
		resp.Diagnostics.AddAttributeError(defaultsPath, "Empty defaults block", "defaults must set at least one placement or size option")
		return
	}
	for _, field := range dashifyLayoutDefaultsModelFields(layout.Defaults) {
		validateDashifyLayoutValue(resp, defaultsPath.AtName(field.name), *field.value, field.coordinate)
	}
}

func validateDashifyLayoutValue(resp *resource.ValidateConfigResponse, valuePath path.Path, value types.String, coordinate bool) {
	if _, _, err := dashifyLayoutValue(value, coordinate); err != nil {
		resp.Diagnostics.AddAttributeError(valuePath, "Invalid layout value", err.Error())
	}
}

func dashifyLayoutIsEmpty(layout *dashifyLayoutModel) bool {
	return dashifyLayoutFieldsEmpty(layout.Absolute, dashifyLayoutModelFields(layout))
}

func dashifyLayoutDefaultsAreEmpty(defaults *dashifyLayoutDefaultsModel) bool {
	return dashifyLayoutFieldsEmpty(defaults.Absolute, dashifyLayoutDefaultsModelFields(defaults))
}

func validateDashifyTemplate(resp *resource.ValidateConfigResponse, containerPath path.Path, model *dashifyTemplateModel) {
	templatePath := containerPath.AtName("template")
	idKnown := !model.TemplateID.IsUnknown()
	contentKnown := !model.Content.IsUnknown()
	idSet := idKnown && !model.TemplateID.IsNull()
	contentSet := contentKnown && !model.Content.IsNull()

	if idKnown && contentKnown && idSet == contentSet {
		resp.Diagnostics.AddAttributeError(templatePath, "Invalid template content", "template must set exactly one of template_id or content")
		return
	}
	if idSet && model.TemplateID.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(templatePath.AtName("template_id"), "Missing required value", "template_id must be non-empty when set")
	}
	if contentSet {
		if _, err := decodeDashifyInlineContent(model.Content.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(templatePath.AtName("content"), "Invalid inline content", err.Error())
		}
	}
}

// TemplateContent encodes a dashboard model for the Observability Template API.
func TemplateContent(model Model) (*template.Content, error) {
	spec, imports, err := buildDashboardSpec(model)
	if err != nil {
		return nil, err
	}
	rootElement := template.RootElementDashboard
	return &template.Content{
		Type:  template.RecordType,
		Title: model.Title.ValueString(),
		Spec:  spec,
		Metadata: template.WriteMetadata{
			RootElement: &rootElement,
			Imports:     imports,
		},
	}, nil
}

func unsupportedDashboardSpec(message string) diag.Diagnostics {
	return diag.Diagnostics{diag.NewErrorDiagnostic("Unsupported dashboard template", message)}
}

func formatDashboardParseError(err error) diag.Diagnostics {
	return unsupportedDashboardSpec(fmt.Sprintf("%v", err))
}

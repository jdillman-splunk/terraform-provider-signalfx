// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/signalfx/signalfx-go/template"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/observability/charts"
	fwshared "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/shared"
)

const (
	dashifyDashboardElement = "<Dashboard>"
	dashifyChartElement     = "<Chart>"
	dashifyPanelElement     = "<Panel>"
	dashifySectionElement   = "<Section>"
	dashifyGroupElement     = "<Group>"
	dashifyImportPrefix     = "$import:"
	dashifyImportElement    = "<$import."
	dashifyTemplatePrefix   = "/v2/template/"
	dashifyLeftoverLimit    = 10
)

// dashifyImportAliasFromElement recognizes the exact literal import-element
// form accepted by Dashify. A prefix match is not sufficient: for example,
// "<$import.widget>trailing" is an ordinary (unsupported) property rather
// than a reference to the "widget" import.
func dashifyImportAliasFromElement(tag string) (string, bool) {
	if !strings.HasPrefix(tag, dashifyImportElement) || !strings.HasSuffix(tag, ">") {
		return "", false
	}
	alias := tag[len(dashifyImportElement) : len(tag)-1]
	if alias == "" || strings.Contains(alias, ">") {
		return "", false
	}
	return alias, true
}

// isDashifyImportElement keeps the write path (decodeDashifyInlineContent)
// and the read path (parseDashifyPanel) on the same exact tag grammar.
func isDashifyImportElement(tag string) bool {
	_, ok := dashifyImportAliasFromElement(tag)
	return ok
}

// Converts the Terraform model into a complete Dashify document and its direct imports.
func buildDashboardSpec(model observabilityDashboardModel) (json.RawMessage, []string, error) {
	spec := map[string]any{"title": model.Title.ValueString()}
	if model.ControlBar != nil {
		spec["controlBar"] = buildDashifyControlBar(model.ControlBar)
	}
	children, saved, imports, err := buildDashifyContainerList(
		spec,
		dashifyContainersFromDashboardModels(model.Container),
		"_",
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	spec[dashifyDashboardElement] = children
	layout, err := buildDashifyLayoutOptions(model.Layout)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard layout: %w", err)
	}
	layout["saved"] = saved
	spec["layout"] = layout
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal dashboard spec: %w", err)
	}
	return raw, imports, nil
}

// Builds one container level with its positional layout entries and nested imports.
func buildDashifyContainerList(spec map[string]any, containers []dashifyContainer, listKey string, metadata map[string]any) ([]any, map[string]any, []string, error) {
	children := make([]any, len(containers))
	items := make([]any, len(containers))
	saved := map[string]any{}
	var imports []string

	for i, container := range containers {
		id := fmt.Sprintf("%s.%d", listKey, i)
		switch {
		case container.Section != nil:
			sectionChildren, sectionSaved, sectionImports, err := buildDashifyContainerList(spec, container.Section.Container, id, buildDashifySectionMetadata(container.Section))
			if err != nil {
				return nil, nil, nil, fmt.Errorf("container %s section: %w", id, err)
			}
			child := map[string]any{dashifySectionElement: sectionChildren}
			layout, err := buildDashifyLayoutOptions(container.Section.Layout)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("container %s section layout: %w", id, err)
			}
			if len(layout) > 0 {
				child["layout"] = layout
			}
			children[i] = child
			for key, value := range sectionSaved {
				saved[key] = value
			}
			imports = append(imports, sectionImports...)
		case container.Group != nil:
			groupChildren, groupSaved, groupImports, err := buildDashifyContainerList(spec, container.Group.Container, id, buildDashifyGroupMetadata(container.Group))
			if err != nil {
				return nil, nil, nil, fmt.Errorf("container %s group: %w", id, err)
			}
			child := map[string]any{dashifyGroupElement: groupChildren}
			layout, err := buildDashifyLayoutOptions(container.Group.Layout)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("container %s group layout: %w", id, err)
			}
			if len(layout) > 0 {
				child["layout"] = layout
			}
			children[i] = child
			for key, value := range groupSaved {
				saved[key] = value
			}
			imports = append(imports, groupImports...)
		default:
			child, reference, err := buildDashifyPanelChild(spec, container.Template, container.Charts, id)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("container %s panel: %w", id, err)
			}
			children[i] = child
			if reference != "" {
				imports = append(imports, reference)
			}
		}
		item, err := buildDashifyLayoutItem(id, container.Layout)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("container %s layout: %w", id, err)
		}
		items[i] = item
	}

	entry := map[string]any{"items": items}
	for key, value := range metadata {
		entry[key] = value
	}
	saved[listKey] = entry
	return children, saved, imports, nil
}

// Wraps an imported Template or opaque inline content in a Panel; only
// imports add API metadata, signaled by a non-empty reference.
func buildDashifyPanelChild(spec map[string]any, model *dashifyTemplateModel, chartEntries []charts.Entry, id string) (map[string]any, string, error) {
	if len(chartEntries) > 0 {
		if len(chartEntries) != 1 || model != nil {
			return nil, "", fmt.Errorf("exactly one chart or template content block must be set")
		}
		return map[string]any{
			dashifyPanelElement: []any{chartEntries[0].Content.BuildSpec()},
		}, "", nil
	}
	if model == nil {
		return nil, "", fmt.Errorf("chart or template content block is missing")
	}
	if model.TemplateID.IsUnknown() || model.Content.IsUnknown() {
		return nil, "", fmt.Errorf("template_id and content must be known before writing")
	}
	idSet := !model.TemplateID.IsNull()
	contentSet := !model.Content.IsNull()
	if idSet == contentSet {
		return nil, "", fmt.Errorf("exactly one of template_id or content must be set")
	}
	if contentSet {
		content, err := decodeDashifyInlineContent(model.Content.ValueString())
		if err != nil {
			return nil, "", err
		}
		return map[string]any{dashifyPanelElement: []any{content}}, "", nil
	}
	if model.TemplateID.ValueString() == "" {
		return nil, "", fmt.Errorf("template_id must be non-empty when set")
	}
	alias := dashifyImportAlias(id)
	reference := dashifyTemplatePrefix + model.TemplateID.ValueString()
	spec[dashifyImportPrefix+alias] = reference
	return map[string]any{
		dashifyPanelElement: []any{
			map[string]any{dashifyImportElement + alias + ">": []any{}},
		},
	}, reference, nil
}

// Validates an opaque Panel child and reserves import elements for template_id.
func decodeDashifyInlineContent(raw string) (map[string]any, error) {
	var value any
	if err := decodeDashifyJSON([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("content must be valid JSON: %w", err)
	}
	content, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("content must be a JSON object")
	}
	tag, _, err := oneDashifyElement(content)
	if err != nil {
		return nil, fmt.Errorf("content %w", err)
	}
	if isDashifyImportElement(tag) {
		return nil, fmt.Errorf("content cannot be an import element; use template_id instead")
	}
	if nestedTag, ok := findDashifyImportElement(content); ok {
		return nil, fmt.Errorf("content cannot contain import element %q; use template_id instead", nestedTag)
	}
	return content, nil
}

// Derives a stable import alias from a container's positional layout ID.
func dashifyImportAlias(id string) string {
	return "widget" + strings.ReplaceAll(strings.TrimPrefix(id, "_."), ".", "_")
}

// MatchesModel reports whether the stored template is semantically identical to
// the document model would write. The Dashify document does not record whether a
// panel was written as a typed block or as raw template.content, so a refresh
// that reparsed it would have to guess. Callers skip the reparse entirely when
// this returns true: state already describes this exact document, so the shape
// recorded there is authoritative and no drift exists to report.
func MatchesModel(record *template.Template, model Model) bool {
	if record == nil || record.Title != model.Title.ValueString() {
		return false
	}
	spec, _, err := buildDashboardSpec(model)
	if err != nil {
		return false
	}
	return fwshared.SemanticallyEqualJSON(string(spec), string(record.Spec))
}

// ParseTemplate decodes API content into a model. Supported charts are admitted
// to typed state only when their generated parser proves the conversion
// lossless; everything else is preserved verbatim as raw template.content.
func ParseTemplate(record *template.Template) (Model, diag.Diagnostics) {
	return parseDashboardTemplate(record)
}

// Consumes modeled fields and warns about leftovers that a complete-document update would drop.
func parseDashboardTemplate(record *template.Template) (observabilityDashboardModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	var model observabilityDashboardModel
	if record == nil || record.Metadata == nil || record.Metadata.RootElement == nil {
		return model, unsupportedDashboardSpec("the template has no Dashboard root element metadata")
	}
	if *record.Metadata.RootElement != template.RootElementDashboard {
		return model, unsupportedDashboardSpec(fmt.Sprintf("the template root element is %q, not %q", *record.Metadata.RootElement, template.RootElementDashboard))
	}

	var spec map[string]any
	if err := decodeDashifyJSON(record.Spec, &spec); err != nil || spec == nil {
		if err == nil {
			err = fmt.Errorf("spec is not a JSON object")
		}
		return model, formatDashboardParseError(err)
	}
	model.Title = types.StringValue(record.Title)
	// spec.title duplicates the record title and has no Terraform model field.
	delete(spec, "title")
	controlBar, controlLeftovers, err := parseDashifyControlBar(spec)
	if err != nil {
		return model, formatDashboardParseError(err)
	}
	model.ControlBar = controlBar
	children, ok := spec[dashifyDashboardElement].([]any)
	if !ok {
		return model, unsupportedDashboardSpec(fmt.Sprintf("spec has no %q element list", dashifyDashboardElement))
	}
	delete(spec, dashifyDashboardElement)
	layout, err := parseDashifyLayoutOptions(spec)
	if err != nil {
		return model, formatDashboardParseError(err)
	}
	model.Layout = layout

	used := map[string]bool{}
	containers, _, leftovers, chartFallbacks, err := parseDashifyContainerList(spec, used, "_", "container", children, dashifyDashboardContainerLevel)
	if err != nil {
		return model, formatDashboardParseError(err)
	}
	model.Container, err = dashifyDashboardModelsFromContainers(containers)
	if err != nil {
		return model, formatDashboardParseError(fmt.Errorf("convert parsed containers: %w", err))
	}
	leftovers = append(controlLeftovers, leftovers...)
	for alias := range used {
		delete(spec, dashifyImportPrefix+alias)
	}
	leftovers = append(leftovers, dashifyLeftovers("", spec)...)
	if raw := strings.TrimSpace(string(record.SignalView)); raw != "" && raw != "null" {
		leftovers = append(leftovers, "signalview")
	}
	if record.Type != "" && record.Type != template.RecordType {
		leftovers = append(leftovers, "type")
	}
	if len(leftovers) > 0 {
		sort.Strings(leftovers)
		diags.AddWarning(
			"Dashboard fields not represented in Terraform state",
			fmt.Sprintf(
				"The stored dashboard contains %d field(s) this provider does not model:\n  %s\n\nThese fields are absent from Terraform state. A later update replaces the complete dashboard document and removes them.",
				len(leftovers),
				strings.Join(truncateDashifyLeftovers(leftovers, dashifyLeftoverLimit), "\n  "),
			),
		)
	}
	if len(chartFallbacks) > 0 {
		sort.Strings(chartFallbacks)
		diags.AddWarning(
			"Dashboard charts preserved as raw content",
			fmt.Sprintf(
				"The stored dashboard contains %d recognized chart(s) that cannot be represented losslessly by the curated typed chart schema. Terraform preserved each complete chart in template.content instead:\n  %s\n\nNo chart properties were discarded. You can keep the raw representation or simplify the stored chart to the supported typed shape.",
				len(chartFallbacks),
				strings.Join(truncateDashifyLeftovers(chartFallbacks, dashifyLeftoverLimit), "\n  "),
			),
		)
	}
	return model, diags
}

// Joins each content element at one nesting level with its separately stored layout entry.
func parseDashifyContainerList(spec map[string]any, used map[string]bool, listKey, path string, children []any, level dashifyContainerLevel) ([]dashifyContainer, map[string]any, []string, []string, error) {
	layouts, metadata, leftovers, err := parseDashifyLayouts(spec, listKey, len(children))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("layout %q: %w", listKey, err)
	}
	// The UI can leave null entries behind when a dashboard container is
	// removed. Treat those entries and their matching layout items as
	// tombstones: Terraform cannot represent them, but the remaining containers
	// are still safe to import. Keep the original positional ID while parsing so
	// surviving sections and groups retain their matching saved layout metadata;
	// buildDashboardSpec compacts them when it writes the document back.
	containers := make([]dashifyContainer, 0, len(children))
	var chartFallbacks []string
	for i, child := range children {
		id := fmt.Sprintf("%s.%d", listKey, i)
		if child == nil {
			leftovers = append(leftovers, fmt.Sprintf("%s.%d (null element; matching layout %s omitted)", path, i, id))
			continue
		}
		containerPath := fmt.Sprintf("%s.%d", path, len(containers))
		container, containerLeftovers, containerFallbacks, err := parseDashifyContainer(spec, used, id, containerPath, child, level)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("container %s: %w", id, err)
		}
		container.Layout = layouts[i]
		containers = append(containers, container)
		leftovers = append(leftovers, containerLeftovers...)
		chartFallbacks = append(chartFallbacks, containerFallbacks...)
	}
	return containers, metadata, leftovers, chartFallbacks, nil
}

// Dispatches an element according to the content allowed at its dashboard, section, or group level.
func parseDashifyContainer(spec map[string]any, used map[string]bool, id, path string, raw any, level dashifyContainerLevel) (dashifyContainer, []string, []string, error) {
	var container dashifyContainer
	node, ok := raw.(map[string]any)
	if !ok {
		return container, nil, nil, fmt.Errorf("is %T rather than an element object", raw)
	}
	tag, value, err := oneDashifyElement(node)
	if err != nil {
		return container, nil, nil, err
	}
	switch tag {
	case dashifySectionElement:
		if level != dashifyDashboardContainerLevel {
			return container, nil, nil, dashifyUnexpectedContainerElement(tag, level)
		}
		items, ok := value.([]any)
		if !ok {
			return container, nil, nil, fmt.Errorf("section value is %T rather than a list", value)
		}
		delete(node, tag)
		layout, err := parseDashifyLayoutOptions(node)
		if err != nil {
			return container, nil, nil, fmt.Errorf("section: %w", err)
		}
		sectionContainers, metadata, childLeftovers, childFallbacks, err := parseDashifyContainerList(spec, used, id, path+".section.container", items, dashifySectionContainerLevel)
		if err != nil {
			return container, nil, nil, fmt.Errorf("section: %w", err)
		}
		title, collapse, collapsible, err := parseDashifySectionMetadata(metadata)
		if err != nil {
			return container, nil, nil, err
		}
		container.Section = &dashifySection{Title: title, Collapse: collapse, Collapsible: collapsible, Layout: layout, Container: sectionContainers}
		return container, append(dashifyLeftovers(path, node), childLeftovers...), childFallbacks, nil
	case dashifyGroupElement:
		if level == dashifyGroupContainerLevel {
			return container, nil, nil, dashifyUnexpectedContainerElement(tag, level)
		}
		items, ok := value.([]any)
		if !ok {
			return container, nil, nil, fmt.Errorf("group value is %T rather than a list", value)
		}
		delete(node, tag)
		layout, err := parseDashifyLayoutOptions(node)
		if err != nil {
			return container, nil, nil, fmt.Errorf("group: %w", err)
		}
		groupContainers, metadata, childLeftovers, childFallbacks, err := parseDashifyContainerList(spec, used, id, path+".group.container", items, dashifyGroupContainerLevel)
		if err != nil {
			return container, nil, nil, fmt.Errorf("group: %w", err)
		}
		title, headerless, err := parseDashifyGroupMetadata(metadata)
		if err != nil {
			return container, nil, nil, err
		}
		container.Group = &dashifyGroup{Title: title, Headerless: headerless, Layout: layout, Container: groupContainers}
		return container, append(dashifyLeftovers(path, node), childLeftovers...), childFallbacks, nil
	case dashifyPanelElement:
		ref, chartEntries, contentLeftovers, contentFallbacks, err := parseDashifyPanel(spec, used, id, path, value)
		if err != nil {
			return container, nil, nil, err
		}
		delete(node, tag)
		container.Template = ref
		container.Charts = chartEntries
		return container, append(dashifyLeftovers(path, node), contentLeftovers...), contentFallbacks, nil
	default:
		return container, nil, nil, dashifyUnexpectedContainerElement(tag, level)
	}
}

// Reports which child elements are valid at a container's nesting level,
// derived from dashifyContainerLevelRules so this message cannot drift from
// the schema/validation rules it describes.
func dashifyUnexpectedContainerElement(tag string, level dashifyContainerLevel) error {
	rule := dashifyContainerLevelRules[level]
	supported := []string{"panels"}
	if rule.allowSection {
		supported = append(supported, "sections")
	}
	if rule.allowGroup {
		supported = append(supported, "groups")
	}
	return fmt.Errorf("is a %s element; only %s are supported at this level", tag, joinWithAnd(supported))
}

func joinWithAnd(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

// Resolves imports, losslessly types supported charts, and preserves every
// other element as opaque inline content. Returns the template reference, any
// typed chart, unrepresented leftovers, and raw-fallback reasons.
func parseDashifyPanel(spec map[string]any, used map[string]bool, id, path string, raw any) (*dashifyTemplateModel, []charts.Entry, []string, []string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("panel value is %T rather than a list", raw)
	}
	if len(items) != 1 {
		return nil, nil, nil, nil, fmt.Errorf("panel contains %d elements; exactly one template reference or inline content object is supported", len(items))
	}
	content, ok := items[0].(map[string]any)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("panel content is %T rather than an element object", items[0])
	}

	chartContent, wrapped, err := unwrapDashifyChartContent(content)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tag, value, err := oneDashifyElement(chartContent)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if alias, ok := dashifyImportAliasFromElement(tag); ok {
		reference, ok := spec[dashifyImportPrefix+alias].(string)
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("import %q has no matching declaration", alias)
		}
		idValue, ok := strings.CutPrefix(reference, dashifyTemplatePrefix)
		if !ok || idValue == "" {
			return nil, nil, nil, nil, fmt.Errorf("import %q references %q rather than a %s<id> template", alias, reference, dashifyTemplatePrefix)
		}
		used[alias] = true
		// An import element normally carries an empty argument list and no
		// siblings. Anything else has no Terraform representation, so consume
		// only the tag and report the rest: Read still succeeds, and the leftover
		// warning says a later update drops what is left behind.
		if args, empty := value.([]any); empty && len(args) == 0 {
			delete(chartContent, tag)
		}
		return &dashifyTemplateModel{TemplateID: types.StringValue(idValue)}, nil, dashifyLeftovers(path, chartContent), nil, nil
	}

	if charts.IsSupportedElement(tag) {
		entry, metadata, parseErr := charts.ParseContent(tag, chartContent)
		if parseErr == nil && metadata.TypedSafe() {
			return nil, []charts.Entry{entry}, nil, nil, nil
		}

		reason := dashifyChartFallbackReason(metadata, parseErr)
		model, encodeErr := dashifyRawTemplateContent(content, id)
		if encodeErr != nil {
			return nil, nil, nil, nil, encodeErr
		}
		wrapperNote := ""
		if wrapped {
			wrapperNote = " (inside a clean <Chart> wrapper)"
		}
		return model, nil, nil, []string{fmt.Sprintf("%s %s%s: %s", path, tag, wrapperNote, reason)}, nil
	}

	model, err := dashifyRawTemplateContent(content, id)
	return model, nil, nil, nil, err
}

func dashifyRawTemplateContent(content map[string]any, id string) (*dashifyTemplateModel, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("container %s encode inline content: %w", id, err)
	}
	return &dashifyTemplateModel{Content: types.StringValue(string(encoded))}, nil
}

// unwrapDashifyChartContent recognizes only the approved, structurally clean
// single-child wrapper. The original outer object is retained for raw fallback.
func unwrapDashifyChartContent(content map[string]any) (map[string]any, bool, error) {
	tag, value, err := oneDashifyElement(content)
	if err != nil {
		return nil, false, err
	}
	if tag != dashifyChartElement {
		return content, false, nil
	}
	if len(content) != 1 {
		return content, false, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) != 1 {
		return content, false, nil
	}
	inner, ok := items[0].(map[string]any)
	if !ok {
		return content, false, nil
	}
	if _, _, err := oneDashifyElement(inner); err != nil {
		return content, false, nil
	}
	return inner, true, nil
}

func findDashifyImportElement(value any) (string, bool) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if isDashifyImportElement(key) {
				return key, true
			}
			if tag, ok := findDashifyImportElement(value[key]); ok {
				return tag, true
			}
		}
	case []any:
		for _, child := range value {
			if tag, ok := findDashifyImportElement(child); ok {
				return tag, true
			}
		}
	}
	return "", false
}

func dashifyChartFallbackReason(metadata charts.ParseMetadata, parseErr error) string {
	if parseErr != nil {
		return parseErr.Error()
	}
	parts := make([]string, 0, 4)
	if len(metadata.Leftovers) > 0 {
		parts = append(parts, "unsupported properties: "+strings.Join(metadata.Leftovers, ", "))
	}
	if len(metadata.MissingRequiredFields) > 0 {
		parts = append(parts, "missing required properties: "+strings.Join(metadata.MissingRequiredFields, ", "))
	}
	if len(metadata.ValidationErrors) > 0 {
		validation := make([]string, len(metadata.ValidationErrors))
		for i, validationErr := range metadata.ValidationErrors {
			validation[i] = validationErr.Path + " " + validationErr.Message
		}
		parts = append(parts, "invalid properties: "+strings.Join(validation, ", "))
	}
	if !metadata.Lossless && len(parts) == 0 {
		parts = append(parts, "rebuilding the curated model would change the stored chart")
	}
	return strings.Join(parts, "; ")
}

// Finds one angle-bracket element while leaving ordinary sibling properties untouched.
func oneDashifyElement(node map[string]any) (string, any, error) {
	var tag string
	var value any
	for key, raw := range node {
		if strings.HasPrefix(key, "<") {
			if tag != "" {
				return "", nil, fmt.Errorf("has multiple dashboard element keys")
			}
			tag = key
			value = raw
		}
	}
	if tag == "" {
		return "", nil, fmt.Errorf("has no dashboard element key")
	}
	return tag, value, nil
}

// Returns unconsumed leaf paths, treating unknown arrays as indivisible values.
func dashifyLeftovers(prefix string, node map[string]any) []string {
	var leftovers []string
	for key, value := range node {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch value := value.(type) {
		case nil:
		case map[string]any:
			leftovers = append(leftovers, dashifyLeftovers(path, value)...)
		case []any:
			if len(value) > 0 {
				leftovers = append(leftovers, path)
			}
		default:
			leftovers = append(leftovers, path)
		}
	}
	return leftovers
}

// Bounds warning output and reports how many unmodeled paths were omitted.
func truncateDashifyLeftovers(leftovers []string, limit int) []string {
	if len(leftovers) <= limit {
		return leftovers
	}
	return append(leftovers[:limit:limit], fmt.Sprintf("... and %d more", len(leftovers)-limit))
}

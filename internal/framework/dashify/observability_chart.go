// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwdashify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/signalfx/signalfx-go/template"

	"github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts"
)

const observabilityChartElement = "<Chart>"

type observabilityChartModel struct {
	ID    types.String `tfsdk:"id"`
	Title types.String `tfsdk:"title"`
	charts.Fields
}

func observabilityChartContent(model observabilityChartModel) (*template.Content, error) {
	entries := model.Entries()
	if len(entries) != 1 {
		return nil, fmt.Errorf("exactly one chart block must be set, got %d", len(entries))
	}
	entry := entries[0]
	if missing := entry.Content.MissingRequiredFields(); len(missing) > 0 {
		return nil, fmt.Errorf("%s is missing required fields: %s", entry.Name, strings.Join(missing, ", "))
	}
	if validationErrors := entry.Content.ValidationErrors(); len(validationErrors) > 0 {
		return nil, fmt.Errorf("%s is invalid: %s", entry.Name, formatChartValidationErrors(validationErrors))
	}

	spec, err := json.Marshal(map[string]any{
		observabilityChartElement: []any{entry.Content.BuildSpec()},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding chart specification: %w", err)
	}
	rootElement := template.RootElementChart
	return &template.Content{
		Type:  template.RecordType,
		Title: model.Title.ValueString(),
		Spec:  spec,
		Metadata: template.WriteMetadata{
			RootElement: &rootElement,
		},
	}, nil
}

func observabilityChartModelFromRecord(record *template.Template) (observabilityChartModel, error) {
	if record == nil {
		return observabilityChartModel{}, errors.New("template API returned no chart record")
	}
	if record.ID == "" {
		return observabilityChartModel{}, errors.New("template API returned a chart record without an ID")
	}
	if record.Metadata == nil || record.Metadata.RootElement == nil {
		return observabilityChartModel{}, errors.New("template API returned a chart record without root element metadata")
	}
	if *record.Metadata.RootElement != template.RootElementChart {
		return observabilityChartModel{}, fmt.Errorf("template root element is %q, want %q", *record.Metadata.RootElement, template.RootElementChart)
	}

	entry, err := parseObservabilityChartSpec(record.Spec)
	if err != nil {
		return observabilityChartModel{}, err
	}
	model := observabilityChartModel{
		ID:    types.StringValue(record.ID),
		Title: types.StringValue(record.Title),
	}
	if err := model.SetEntries([]charts.Entry{entry}); err != nil {
		return observabilityChartModel{}, err
	}
	return model, nil
}

func parseObservabilityChartSpec(raw []byte) (charts.Entry, error) {
	var root map[string]any
	if err := decodeObservabilityChartJSON(raw, &root); err != nil {
		return charts.Entry{}, fmt.Errorf("invalid Chart specification: %w", err)
	}
	if len(root) != 1 {
		return charts.Entry{}, fmt.Errorf("Chart specification must contain only %s, found %d root properties", observabilityChartElement, len(root))
	}
	rawChildren, exists := root[observabilityChartElement]
	if !exists {
		return charts.Entry{}, fmt.Errorf("Chart specification has no %s root element", observabilityChartElement)
	}
	children, ok := rawChildren.([]any)
	if !ok {
		return charts.Entry{}, fmt.Errorf("%s is %T rather than a list", observabilityChartElement, rawChildren)
	}
	if len(children) != 1 {
		return charts.Entry{}, fmt.Errorf("%s contains %d children; exactly one chart is required", observabilityChartElement, len(children))
	}
	content, ok := children[0].(map[string]any)
	if !ok {
		return charts.Entry{}, fmt.Errorf("%s child is %T rather than an object", observabilityChartElement, children[0])
	}
	tag, err := oneObservabilityChartElement(content)
	if err != nil {
		return charts.Entry{}, err
	}
	if !charts.IsSupportedElement(tag) {
		return charts.Entry{}, fmt.Errorf("Chart contains unsupported element %q; use signalfx_observability_template for raw Chart content", tag)
	}
	entry, metadata, err := charts.ParseContent(tag, content)
	if err != nil {
		return charts.Entry{}, fmt.Errorf("parsing %s: %w", tag, err)
	}
	if !metadata.TypedSafe() {
		return charts.Entry{}, fmt.Errorf("%s cannot be represented by a typed chart block: %s; use signalfx_observability_template for raw Chart content", tag, formatChartParseMetadata(metadata))
	}
	return entry, nil
}

func decodeObservabilityChartJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("contains multiple JSON values")
		}
		return err
	}
	return nil
}

func oneObservabilityChartElement(content map[string]any) (string, error) {
	var tag string
	for key := range content {
		if !strings.HasPrefix(key, "<") {
			continue
		}
		if tag != "" {
			return "", errors.New("Chart child has multiple element keys")
		}
		tag = key
	}
	if tag == "" {
		return "", errors.New("Chart child has no element key")
	}
	return tag, nil
}

func formatChartParseMetadata(metadata charts.ParseMetadata) string {
	var parts []string
	if len(metadata.Leftovers) > 0 {
		parts = append(parts, "unmodeled properties: "+strings.Join(metadata.Leftovers, ", "))
	}
	if len(metadata.MissingRequiredFields) > 0 {
		parts = append(parts, "missing required fields: "+strings.Join(metadata.MissingRequiredFields, ", "))
	}
	if len(metadata.ValidationErrors) > 0 {
		parts = append(parts, "invalid properties: "+formatChartValidationErrors(metadata.ValidationErrors))
	}
	if len(parts) == 0 {
		return "rebuilding the model would change the stored chart"
	}
	return strings.Join(parts, "; ")
}

func formatChartValidationErrors(validationErrors []charts.ValidationError) string {
	formatted := make([]string, len(validationErrors))
	for i, validationErr := range validationErrors {
		formatted[i] = validationErr.Message
		if validationErr.Path != "" {
			formatted[i] = validationErr.Path + ": " + validationErr.Message
		}
	}
	return strings.Join(formatted, ", ")
}

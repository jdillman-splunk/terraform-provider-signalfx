// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package elements exposes the pinned Olly JSON Schema catalog used to check
// Terraform's generated Dashify chart documents.
package elements

import (
	"embed"
	"fmt"
)

const (
	Draft2020       = "https://json-schema.org/draft/2020-12/schema"
	SourceCommit    = "140332fd80ac611adf819a4c43424319789eb517"
	SourceDirectory = "apps/app-modern-dashboards/src/_templates/schemas"
)

// Definition identifies one schema artifact and its Dashify element tag.
type Definition struct {
	File      string
	Element   string
	SchemaID  string
	SHA256Sum string
}

// Catalog is intentionally explicit: adding or removing a chart is a reviewed
// provider surface change rather than a side effect of an extra file.
var Catalog = [...]Definition{
	{
		File:      "O11yClusterMap.schema.json",
		Element:   "<o11y:ClusterMap>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-cluster-map.json",
		SHA256Sum: "78ded1c70e5ab638f8d9583f96b4778fc9825a6f1f6da18b1a1d399fda70757f",
	},
	{
		File:      "O11yList.schema.json",
		Element:   "<o11y:List>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-list.json",
		SHA256Sum: "1f7177904844751a1a13a9faa46ae2f071fc5b4a23ae888255795f0513b93954",
	},
	{
		File:      "O11ySingleValue.schema.json",
		Element:   "<o11y:SingleValue>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-single-value.json",
		SHA256Sum: "b96807dfbb117fc91b82b00f04213c0b1db911d38a182416621dfb9cf70b0894",
	},
	{
		File:      "O11yTableChart.schema.json",
		Element:   "<o11y:TableChart>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-table-chart.json",
		SHA256Sum: "c9eeebf9a306cd0a4956bb347f66b5e34431cbe51fa5021efd61e57e9ec53512",
	},
	{
		File:      "O11yText.schema.json",
		Element:   "<o11y:Text>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-text.json",
		SHA256Sum: "e977bb78ddf4544255e39277ef78316a5637089423d9f83df41d8ffc3e3a5caf",
	},
	{
		File:      "O11yTimeSeriesChart.schema.json",
		Element:   "<o11y:TimeSeriesChart>",
		SchemaID:  "https://schema.splunkdev.com/dashify/v1/elements/o11y-time-series-chart.json",
		SHA256Sum: "45be863bb9584517051801abdd2da105a6be32a6542c94f061d7bc98ac7f8db9",
	},
}

// Files contains the immutable schemas and their provenance metadata.
//
//go:embed O11y*.schema.json source.json SHA256SUMS README.md
var Files embed.FS

// Read returns one cataloged schema. It refuses arbitrary paths so callers
// cannot accidentally treat source.json as an element contract.
func Read(definition Definition) ([]byte, error) {
	for _, candidate := range Catalog {
		if definition.File == candidate.File {
			return Files.ReadFile(candidate.File)
		}
	}
	return nil, fmt.Errorf("dashify element schema %q is not in the catalog", definition.File)
}

// Find returns the schema definition for an exact serialized element tag.
func Find(element string) (Definition, bool) {
	for _, definition := range Catalog {
		if definition.Element == element {
			return definition, true
		}
	}
	return Definition{}, false
}

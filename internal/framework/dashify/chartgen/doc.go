// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

// Command chartgen reads the chart DSL files in charts/schemas/*.yml and
// generates Go code for the charts package
// (internal/framework/dashify/charts) plus the bridge the dashboard
// package imports
// (internal/framework/dashify/dashboard/dashboard_charts_generated.go).
// Each chart type gets its own generated file. Nothing in the provider
// parses YAML at runtime; only the generated Go is compiled in.
//
// runGeneration (generator.go) runs for both -mode=write and -mode=check.
// Before generating anything, it compiles the vendored Olly JSON Schema
// contracts under charts/schemas/elements (contract.go) and checks each
// YAML file's mappings against the matching contract (witness.go). A typo'd
// mapping path, or a field the contract doesn't have, fails the build
// instead of silently producing a broken chart.
//
// The DSL supports scalar and enum properties, negate/relative-duration
// transforms, list/map/block/oneof attributes, and arrays, built from
// properties, extends (shared partials under charts/schemas/common),
// normalizations, and directives like mapping, conflicts_with,
// requires_value, and required_when. Anything else fails generation with an
// error instead of being dropped silently.
//
// # Usage
//
//	go run ./internal/framework/dashify/chartgen -mode=write|check [-repo-root=path]
//
// -mode=write regenerates the charts package and the dashboard bridge.
// -mode=check (what CI runs via `make check-generated-charts`) fails if
// regenerating would change anything, catching both hand-edited generated
// files and out-of-date generation.
//
// To print a starter DSL file without writing anything:
//
//	go run ./internal/framework/dashify/chartgen -mode=scaffold \
//	  -contract=O11yTimeSeriesChart.schema.json -type=metrics_time_series
//
// # Adding a new chart element
//
// Example: Olly adds an `o11y:PieChart` element and we want to expose it as
// `metrics_pie_chart`. All of this lands in one MR.
//
//  1. Vendor the contract. Drop O11yPieChart.schema.json into
//     charts/schemas/elements exactly as it came from the pinned Olly
//     commit; its own "$id" and "title" fields are all the catalog needs,
//     so no other file changes here. Run
//     `go test ./internal/framework/dashify/charts/schemas/elements`
//     to confirm it compiles as a schema. See that directory's README.md
//     for the full policy: never hand-edit a vendored JSON file; fix it in
//     Olly first.
//
//  2. Scaffold a starting point instead of typing the YAML by hand:
//
//     go run ./internal/framework/dashify/chartgen -mode=scaffold \
//     -contract=O11yPieChart.schema.json -type=metrics_pie_chart \
//     > internal/framework/dashify/charts/schemas/metrics_pie_chart.yml
//
//     Check the warnings printed to stderr before trusting the output.
//
//  3. Clean up that file like the other charts/schemas/*.yml files: drop
//     fields Terraform doesn't need, reuse shared shapes from
//     charts/schemas/common where they fit, change any `float` that should
//     be `number`, write real descriptions, and add conflicts_with,
//     requires_value, or required_when where needed. Keep
//     dashify.element.name as the exact Dashify tag (`o11y:PieChart`) so
//     witness.go can match it to the contract.
//
//  4. Regenerate: `make generate-charts`. This writes
//     charts/metrics_pie_chart_generated.go and updates
//     charts/content_generated.go, charts/contract_generated.go, and
//     dashboard/dashboard_charts_generated.go. Don't hand-edit generated
//     files; change the YAML and regenerate instead.
//
//  5. Check it: `make check-generated-charts` should report no diff, and
//     `go test ./internal/framework/dashify/...` should pass.
//
//  6. Add a Terraform example under
//     examples/resources/observability_dashboard/, then run
//     `make gen-docs` to refresh docs/resources/observability_dashboard.md
//     and `make check-docs` to confirm it's committed.
//
//  7. Add a CHANGELOG.md entry under "Unreleased", then open the MR with
//     the vendored contract, the YAML, the regenerated files, the example,
//     the docs, and the changelog entry together.
package main

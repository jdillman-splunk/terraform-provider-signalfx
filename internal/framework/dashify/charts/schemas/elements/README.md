# Dashify element contracts

This directory vendors the six generated JSON Schema contracts for the
Observability chart elements supported by Terraform. The authoritative source
is Olly's `O11yTemplateContracts.ts`; the JSON files are generated artifacts,
not hand-maintained provider schemas.

`catalog.go`'s `Catalog` is discovered from the embedded files themselves:
each `O11y*.schema.json` file's own `$id` and `title` fields become its
`SchemaID` and `Element`. Adding or removing a chart element is just adding
or removing a file here - nothing else in this package needs to change. The
Go tests compile every contract as JSON Schema Draft 2020-12 and verify the
local `$ref`/`$defs` graph without network access.

## Updating the catalog

1. Change the TypeScript contracts in Olly and run Olly's schema generator and
   focused schema tests.
2. Copy the reviewed `O11y*.schema.json` artifact(s) from that Olly commit
   without reformatting them.
3. Run `go test ./internal/framework/dashify/charts/schemas/elements` and the chart generator tests.

Do not patch a vendored JSON file here. A contract correction must be made and
tested in Olly first, then re-vendored.

## Scaffolding Terraform chart schemas

The provider's chart YAML remains a deliberately curated public API rather
than a direct copy of these contracts. To avoid retyping the mechanical JSON
shape when starting a chart, emit a complete starter document to stdout:

```sh
go run ./internal/framework/dashify/chartgen \
  -mode=scaffold \
  -contract=O11yTimeSeriesChart.schema.json \
  -type=metrics_time_series
```

To scaffold only a reusable definition, select one top-level `$defs` entry.
Quote the pointer so the shell does not expand `$defs`:

```sh
go run ./internal/framework/dashify/chartgen \
  -mode=scaffold \
  -contract=O11yTimeSeriesChart.schema.json \
  -pointer='#/$defs/O11yWidgetProps'
```

Scaffolding never writes repository files. Redirect stdout only after
reviewing the warnings on stderr. The output intentionally needs curation:
remove unsupported fields, flatten or rename fields for Terraform, choose
`number` instead of the conservative generated `float` where an integer is
required, add provider-specific validation and normalizations, and replace
generated placeholder descriptions. Deprecated contract fields are reported
and omitted. Unsupported active JSON Schema shapes fail with their schema path
instead of being silently dropped.

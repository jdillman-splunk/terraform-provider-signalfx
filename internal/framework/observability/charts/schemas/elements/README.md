# Dashify element contracts

This directory vendors the six generated JSON Schema contracts for the
Observability chart elements supported by Terraform. The authoritative source
is Olly's `O11yTemplateContracts.ts`; the JSON files are generated artifacts,
not hand-maintained provider schemas.

`source.json` pins the source repository, merge request, commit, generator,
schema draft, element names, schema IDs, and per-file hashes. `SHA256SUMS`
provides a standard checksum list for the byte-for-byte artifacts. The Go
catalog and tests compile every contract as JSON Schema Draft 2020-12 and
verify the local `$ref`/`$defs` graph without network access.

## Updating the catalog

1. Change the TypeScript contracts in Olly and run Olly's schema generator and
   focused schema tests.
2. Copy all six `O11y*.schema.json` artifacts from the same reviewed Olly
   commit without reformatting them.
3. Update `source.json` to that commit and refresh both checksum lists.
4. Run `go test ./internal/framework/observability/charts/schemas/elements` and the chart generator tests.

Do not patch a vendored JSON file here. A contract correction must be made and
tested in Olly first, then re-vendored as one coherent catalog.

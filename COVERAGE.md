# Chart schema coverage and gaps

Every field the typed chart schemas in this directory deliberately do **not**
model, and why. Written for whoever maintains this catalog: read the relevant
section before adding a field, so a gap that was already investigated and
rejected doesn't get relitigated, and a gap that is merely unfinished doesn't
get mistaken for a decision.

The schema files themselves carry only field-level mapping notes. Coverage
decisions live here.

## Scoping rules

A field earns schema surface if either holds:

- **Classic parity** — an existing classic `signalfx_*_chart` resource sets it,
  so a migration that dropped it would lose author intent or silently change
  behavior.
- **Editor support** — Dashify's own widget editor can author it.

Fields with neither are omitted. Counts below are from the
`terraform-monitoring` inventory and are the evidence for each call.

Three kinds of entry appear here:

- **Permanent gap** — the classic field has no Dashify equivalent at all.
  Confirmed against the Dashify element contracts, not assumed. These will not
  be closed by adding schema surface.
- **Dropped** — a real Dashify field left out on scoping grounds. Cheap to add
  back if a need appears.
- **Editor round-trip gap** — a field that renders and imports correctly, but
  that Dashify's own save path discards. Authoring it in Terraform works; a
  subsequent UI save may not preserve it.

## Cross-cutting

### Datasource — `common/signalfx_datasource.yml`

- **Permanent gap: classic `timezone`** (954 active uses across all chart
  types). Dashify has no per-chart display timezone. The nearest field,
  `datasource.timezone`, is SignalFlow calendar-window semantics rather than a
  display setting, and no real document sets it. Do not map classic `timezone`
  onto it.
- **Dropped: classic `disable_sampling`.** Dashify has no boolean sampling
  toggle for most charts; it stores a numeric cap, which `sample_size` models
  directly. Classic `disable_sampling = true` is roughly `sample_size = 100`
  and `false` is the ~1000 default, but no transform converts one to the
  other, so author `sample_size` directly. `signalfx_table_chart` is the one
  classic resource with a genuinely boolean equivalent — see the table section
  below.
- **Included on parity grounds only:** `max_delay`, `minimum_resolution`, and
  `sample_size` have no Dashify UI control (they are advanced/JSON-only in the
  app) but are set on nearly every classic chart, so dropping them would
  silently change query behavior. `program` is the only one of these with an
  editor control.

### Widget chrome — `common/widget.yml`

`O11yWidgetProps` is byte-identical across all six element contracts, so every
field in this fragment is available to every chart type. The whole of it is
modeled except one field.

`title`, `description`, `links`, `headerless`, and `borderless` are all
modeled. The last three have no classic equivalent — `headerless` and
`borderless` have none at all, and the nearest analogue to `links` is
`signalfx_data_link`, used 4 times across `terraform-monitoring`. They are
modeled anyway: this fragment is Dashify-native chrome that classic never had,
and Terraform should be able to author it going forward rather than being
capped at the classic feature set.

- **Dropped: `aiGenerated`.** Provenance metadata the app sets when a widget
  came from an AI flow, not author intent. Nothing in Terraform should claim a
  chart was AI-generated, and round-tripping it would let a Terraform apply
  silently relabel a hand-written chart. This is the one field in
  `O11yWidgetProps` left uncurated, and `charts` tests rely on it as their
  example of a valid-but-uncurated shape.

### Precision — `common/precision.yml`

Only significant digits is modeled. Classic's `max_precision` maps to it and is
set on 1030/1175 single-value and 1738/1744 list charts. Dashify's
`maximumFractionDigits` has a UI control but no classic equivalent, so it is
left out under the classic-parity rule. (The time-series chart models it
separately at both chart and axis level, where it is editor-authored.)

### Per-plot overrides — `common/published_streams.yml`

Extended by the list chart only. Not extended elsewhere, because doing so would
write JSON that Dashify silently ignores:

- **SingleValue** has no `publishedStreams` concept at all; it takes one
  chart-level `display_unit` instead.
- **TableChart** types `publishedStreams`, but the renderer never reads it and
  the importer never writes it. Per-column name and unit come from `columns`.

Classic `viz_options.axis` and `viz_options.plot_type` are time-chart-only and
are not modeled in this shared fragment.

### Secondary visualization — `common/secondary_visualization.yml`

Reused by SingleValue and List, which share the identical editor component,
enum values, and default. **Table has no equivalent concept** — do not extend
this fragment for chart types with no matching UI control.

## `metrics_time_series`

Covers the common editor-supported path rather than every legacy option, and
deliberately offers no open-ended merge bag. The inventory supports the cutoff:
virtually every chart uses line, area, or column rendering; advanced
legend-field blocks occur on roughly 13% and event-option blocks on roughly 1%.
That leaves a comfortable basic-80% path without pretending every legacy option
belongs in the typed schema. Anything outside it is authorable through
`signalfx_observability_template`.

Permanent gaps:

- **classic `axes_include_zero`** (895 active uses) — not in Dashify's
  `chartOptions` or per-axis `YAxisOptions`. The classic→Dashify importer
  deliberately drops it rather than approximating it with min/max.
- **classic `event_options { label, display_name, color }`** (501 active uses)
  — Dashify's event annotation config is render-only React state, not part of
  the serialized document. Only the event query (`datasource.events.query`) is
  persisted, and this schema does not model event overlays at all yet.
- **classic `histogram_options.color_theme`** — a classic Histogram plot type
  converts to a *different* Dashify element (`o11y:Heatmap`, `chart.palette`),
  not a TimeSeriesChart property. Heatmap charts are out of scope, so this
  never reaches this schema.
- **classic `refresh_interval`** — converted away into datasource
  resolution/backfill on import; nothing in the serialized document
  corresponds to it for a time-series chart.
- **classic `start_time` / `end_time` absolute ranges** — declared on 1127
  dashboards but non-null on only 49 real time charts. Only relative ranges
  are modeled.

Editor round-trip note: chart-level `maximum_significant_digits` is distinct
from each `y_axes` entry's own. The Dashify editor persists only the per-axis
form on save, so setting the chart-level field and later editing an axis in the
UI relocates the value to that axis alone.

## `metrics_list`

Scoped to what the 1806 existing `signalfx_list_chart` resources set. Fields
with no classic equivalent, plus deprecated, runtime-derived, and JSON-only
ones, are omitted.

- **Permanent gaps:** classic `unit_prefix` (List has no binary-unit toggle —
  see the single-value section), `refresh_interval`, and `timezone`.
- **Editor round-trip gap:** `color_by` and `color_scale` are real,
  render-honored, import-preserved List fields, but the Dashify Widget
  Builder's save path does not include them in what it writes back. Editing
  this chart's *other* settings in the UI and saving will silently drop them.
  Modeled anyway: omitting a field 1764 real charts set is a much larger and
  more certain parity loss than a UI-save edge case.

## `metrics_single_value`

Scoped to what the 1240 existing `signalfx_single_value_chart` resources set.

Permanent gaps:

- **classic `unit_prefix`** (1171 active, all `Metric`/non-binary) — `useKmg2`
  exists only on TimeSeriesChart's `chartOptions`; `SingleValueProps` has no
  binary-unit toggle at all.
- **classic per-plot `viz_options.display_name`** (1181 active) — SingleValue
  has no per-plot naming in Dashify. The chart shows one value, so there is
  nothing to individually label the way a list or time-series chart's
  per-series names do.
- **classic `refresh_interval`** (272 active) and **`timezone`** (56 active) —
  see the shared datasource section.

Editor round-trip note: `color_scale` bucket upper bounds (`lt`/`lte`) are not
authored by the Dashify editor, which derives them from the next bucket's lower
bound. They are a real, import-preserved part of the document, so they are
modeled for round-trip fidelity.

## `metrics_table`

The element is `o11y:TableChart`, not `o11y:Table` — confirmed against the real
element tag. Scoped to what the 103 existing `signalfx_table_chart` resources
set. This is the rarest classic chart type by a wide margin (0.5% of all
charts), so the schema is deliberately minimal.

- **Permanent gap:** classic `timezone` and `refresh_interval`.
- **`published_streams` not extended** — see the shared fragment section above.
- **No `secondary_visualization`** — no such concept for Table.
- **classic `viz_options`** (per-plot label and unit overrides) has no
  counterpart on TableChart in the current data model. The real Dashify field
  for per-column display is `columns`, keyed by SignalFlow publish label.
- **`disable_sampling` is modeled here, unlike other charts.** TableChart has a
  genuine boolean `disableSampling` prop rather than a numeric `sampleSize`
  cap. Classic sets it on 103/103 resources, though only 32 non-default.
- **`hide_missing_values` is a parity exception.** No classic equivalent on
  table charts, but Dashify has a real control and list charts use the
  identical concept on 1709 resources. Kept as a one-line field rather than a
  deliberate capability gap.

## `metrics_cluster_map`

ClusterMap — not Dashify's own newer, unrelated `o11y:Heatmap` element (a
time-bucketed histogram with ~0 classic usage) — is the confirmed migration
target for classic `signalfx_heatmap_chart`: `convertHeatmap.ts` in
`app-modern-dashboards` converts a classic Heatmap chart's
`colorBy`/`colorScale`/`colorRange`/`groupBy` straight into
`<o11y:ClusterMap>`. Scoped to what the 171 existing `signalfx_heatmap_chart`
resources set, cross-checked against `ClusterMapConfigPanel.tsx`.

- **Permanent gap: classic `sort_by`** (158/171 — the single most common field
  on this chart, by far). ClusterMap has no sort-order concept anywhere in
  `ClusterMapProps` or `ClusterMapConfig`; cells lay out in query-result order.
  A user who needs a specific order has to encode it in the SignalFlow
  `program` itself, for example with `top()`. This is a bigger real-usage gap
  than any other permanent gap in this catalog.
- **Also permanent gaps:** classic `timezone` (147/171) and `refresh_interval`
  (153/171). Classic `unit_prefix` is 170/171 but always `Metric` (the
  default), and `ClusterMapProps` has no unit-prefix concept at all, so it is
  dropped the same way the single-value and list schemas drop it.
- **Dropped: classic boolean `disable_sampling`** (170/171). Like every chart
  type except TableChart, ClusterMap has no boolean sampling toggle — author
  `sample_size` directly.
- **Dropped: `no_data_options`** — a real `ClusterMapProps` field, but 0
  classic usage (`signalfx_heatmap_chart` has no equivalent) and not
  round-tripped by Dashify's own ClusterMap editor, whose
  `templateMatchingKeys` omits it. Saving other settings in the UI silently
  drops it. Fails both the parity and editor-support rules.
- **`display_unit` is a parity exception.** 0 classic usage (no
  `viz_options`/`publishLabelOptions` field exists on
  `signalfx_heatmap_chart`), but it is a real, renderer-honored,
  import-preserved `ClusterMapProps` field kept in `templateMatchingKeys` —
  Dashify's own editor round-trips it even though its panel has no control to
  author a new value. Kept for the same reason as the table chart's
  `hide_missing_values`: a one-line field is cheaper than a capability gap, and
  authoring it from Terraform is a real value-add the native editor does not
  offer.
- **Lossy mapping: classic `color_range`** (1/171). Classic's `color_range` is
  a literal hex color; Dashify has only three named sequential palettes, so a
  classic hex value has no lossless equivalent in the typed surface.

## `text`

Scoped to the 1241 existing `signalfx_text_chart` resources — the largest
single chart-type gap before this schema existed. Text charts are the third
most common classic chart type after time and list charts.

- **Dropped: optional datasource.** Permitted by the Olly contract, but a text
  widget has no metric plumbing, so a query on it would do nothing.
- **Dropped: `variables`.** The template overwrites it from the dashboard's own
  filters at render time, so it is runtime-derived. Authoring it would be a
  no-op.

`widget.links` was previously excluded here as well. It is now modeled for
every chart type through the shared widget fragment.

## Classic field mapping

Which classic `signalfx_*_chart` field each schema field came from, and how
often the inventory sets it. This is the evidence behind every scoping call
above, and the reference for anyone writing a classic→Dashify migration. The
schema files themselves no longer carry these notes.

Counts are active uses in `terraform-monitoring`.

### `common/widget.yml`

| schema field | classic field | usage |
| --- | --- | --- |
| `title` | `name` | 100% of charts |
| `description` | `description` | 577/1224 single-value, 735/1763 list, 24/103 table |
| `links` | nearest is `signalfx_data_link` | 4 across the whole repo |
| `headerless`, `borderless` | none | — |

### `common/signalfx_datasource.yml`

| schema field | classic field | usage |
| --- | --- | --- |
| `program` | `program_text` | 100% of charts |
| `max_delay` | `max_delay` | 738/1115 single-value, 966/1655 list, 85/102 table |
| `minimum_resolution` | `minimum_resolution` | 85/100 table; rare elsewhere |
| `sample_size` | `disable_sampling`, boolean → numeric cap | see Dropped, above |

### `common/precision.yml`

| schema field | classic field | usage |
| --- | --- | --- |
| `maximum_significant_digits` | `max_precision` | 1030/1175 single-value, 1738/1744 list |

### `common/published_streams.yml`

This fragment is classic's `viz_options` block — the single most-used nested
block in the inventory: 1894 blocks across single-value charts, 3995 across
list charts, 193 across table charts. Field-for-field:

```
classic viz_options        ->  Dashify publishedStreams[<label>]
label                      ->  (the map key itself)
display_name               ->  name           (6210/6264 - near-universal)
value_unit                 ->  displayUnit    (1152)
value_suffix               ->  displayUnit    (287)
value_prefix               ->  displayUnit    (17 - rare, same oneof covers it)
color                      ->  paletteIndex   (802)
axis, plot_type            ->  time-chart only, not modeled
```

Classic `viz_options.color` is a color *name* string such as `"blue"`. The
serializer maps names to palette indices, because Dashify's color picker only
ever emits `paletteIndex`.

### `common/display_unit.yml`

Classic configs set units per-plot
(`viz_options.value_unit`/`value_prefix`/`value_suffix`), never chart-wide —
except where a chart has no per-plot concept at all, which is SingleValue.
That is why this fragment is referenced per-plot rather than `extends`ed at a
chart root.

### `metrics_list`

| schema field | classic field | usage |
| --- | --- | --- |
| `hide_missing_values` | `hide_missing_values` | 1709/1709 — always explicitly set |
| `color_by` | `color_by` | 1764/1806 |
| `color_scale` | `color_scale` | 137 blocks, always paired with `color_by = "Scale"` |
| `display_fields` | `legend_options_fields` | 5636 blocks, field-for-field identical (`{property, enabled}`) |
| `sort` | `sort_by` | 1391/1742 |

- `color_by` — Dashify's own default is `Metric`, but classic authors set
  `Dimension` far more often (1513 of 1764).
- `sort` — classic encodes this as a sign-prefixed string: `-value` (1081),
  `+value` (113), plus `+sf_metric`, `-name`, and similar. Dashify splits it
  into `by` + `direction`, so the serializer parses the sign off rather than
  passing the string through.

### `metrics_single_value`

- `display_unit` — classic single-value charts have no per-plot `viz_options`
  concept. The legacy importer collapses `viz_options[0]`'s
  unit/prefix/suffix into the one chart-level `SingleValueProps.displayUnit`,
  which is the only display-unit control SingleValue has.
- `color_scale` — 1235 blocks, the second-most-used nested block after
  `viz_options`. Classic pairs it with `color_by = "Scale"`; Dashify infers the
  same thing from `colorScale` being present, so no separate `color_by` field
  is needed here.

### `metrics_table`

| schema field | classic field | usage |
| --- | --- | --- |
| `group_by` | `group_by` | 100/100 — always set |
| `disable_sampling` | `disable_sampling` | 103/103 — always set, only 32 non-default |
| `hide_missing_values` | none | parity exception, above |

Classic `viz_options` has no counterpart on TableChart in the current data
model; per-column display comes from `columns` instead.

### `metrics_cluster_map`

| schema field | classic field | usage |
| --- | --- | --- |
| `group_by` | `group_by` | 169/171, 64 of those an explicitly empty list |
| `color_scale` | `color_scale` | 135/171 — the dominant coloring mode here |
| `color_range` | `color_range` | 1/171 — the rarest field in the schema |
| `color_by` | encoded implicitly | see below |

- `group_by` — the editor allows at most 2 selections, and no real classic
  resource sets more than 2 values either.
- `color_by` — classic has no explicit field. `options.ColorBy` defaults to
  `Range` and becomes `Scale` only when a `color_scale` block is present.
  135/171 charts set `color_scale` (implying Scale), 1/171 sets `color_range`
  (implying Range with explicit bounds), and the remaining 35 rely on the Range
  default with auto min/max. Unlike the single-value chart, Dashify does *not*
  infer the mode here, so `color_by` must be authored explicitly — see that
  field's own note in the schema.

### `text`

| schema field | classic field | usage |
| --- | --- | --- |
| `markdown` | `options.markdown` | 1229/1241 — near-universal |

### Threshold bounds

Every `color_scale` bucket bound is a `float` rather than an integer because
real classic thresholds are not always whole numbers — the inventory contains
`gt = 0.5` and `gte = 99.9`.

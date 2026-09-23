---
page_title: "Splunk Observability Cloud: signalfx_observability_chart"
description: |-
  Manages a reusable Observability Chart Template using one typed chart block.
---

# Resource: signalfx_observability_chart

This resource manages a reusable Chart record in the Observability Template API. It does not manage a classic chart object. Reference its `id` from a `signalfx_observability_dashboard` `template` block to place the chart on a dashboard.

Set exactly one of the six typed chart blocks. Imports and refreshes are intentionally strict: the stored Chart must use a supported type and every field must be representable without losing data. Use `signalfx_observability_template` with `root_element = "Chart"` for future chart types, raw JSON, or supported charts containing fields outside the typed schema.

## Example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_chart" "request_rate" {
  title = "Reusable request rate"

  metrics_time_series {
    title         = "Request rate"
    program       = "A = data('requests.count').sum().publish('A')"
    time_range    = "1h"
    visualization = "line"

    series = {
      A = {
        name = "Requests"
        display_unit = {
          suffix = "/s"
        }
      }
    }
  }
}

resource "signalfx_observability_dashboard" "service" {
  title = "Service overview"

  container {
    template {
      template_id = signalfx_observability_chart.request_rate.id
    }
  }
}
```

## Arguments

* `title` - (Required) Reusable Chart Template record title.
* `metrics_time_series`, `metrics_list`, `metrics_single_value`, `metrics_table`, `metrics_cluster_map`, or `text` - (Required, Exactly One) Typed chart content. The five metrics blocks require a non-empty SignalFlow `program`; their optional `time_range` must be a positive whole-number duration such as `15m` or `1h`. `text` has no datasource fields.

Every chart block supports an optional display `title` and `description`. The record `title` and chart display `title` are independent.

| Block | Additional fields | Required nested members and constraints |
| --- | --- | --- |
| `metrics_time_series` | `visualization`, `show_data_markers`, `show_event_lines`, `hide_missing_values`, `color_by`, `resolution`, `backfill_slice_count`, `use_binary_units`, `max_series`, `maximum_significant_digits`, `maximum_fraction_digits`, `show_legend`, `legend_position`, `show_series_value`, `can_toggle_series_visibility`, `legend_dimension`, `preferred_editor`, `no_data_message`, `links`, `y_axes`, `additional_properties`, `series` | `links[].url`; `additional_properties[].property` and `enabled` |
| `metrics_list` | `maximum_significant_digits`, `show_timestamp`, `secondary_visualization`, `published_streams`, `hide_missing_values`, `color_by`, `color_scale`, `display_fields`, `sort` | `display_fields[].property` and `enabled`; `sort.by` and `direction`; `color_scale` and `color_by = "Scale"` require each other |
| `metrics_single_value` | `maximum_significant_digits`, `maximum_fraction_digits`, `show_timestamp`, `secondary_visualization`, `display_unit`, `color_scale` | None beyond the display-unit rule below |
| `metrics_table` | `maximum_significant_digits`, `show_timestamp`, `group_by`, `hide_missing_values`, `disable_sampling`, `columns` | `columns[].field` |
| `metrics_cluster_map` | `maximum_significant_digits`, `show_timestamp`, `display_unit`, `group_by`, `color_by`, `color_range`, `color_scale` | `color_range.palette`; `color_scale` and `color_by = "Scale"` require each other; `color_range` and `color_scale` conflict |
| `text` | `markdown` | None |

The metrics blocks also support `max_delay`, `minimum_resolution`, `sample_size`, and `time_range`. Where available, a `display_unit` requires either a named `unit` or at least one custom `prefix` or `suffix`; this rule also applies to display units nested under `series`, `published_streams`, and `columns`. `metrics_cluster_map.display_unit` accepts only a custom prefix or suffix.

## Attributes

* `id` - The Chart Template record ID.

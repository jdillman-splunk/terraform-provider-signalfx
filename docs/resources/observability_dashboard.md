---
page_title: "Splunk Observability Cloud: signalfx_observability_dashboard"
description: |-
  Manages an Observability dashboard Template using reusable Template references, typed charts, raw inline dashboard content, and dashboard controls.
---

# Resource: signalfx_observability_dashboard

The dashboard supports reusable Template references, six curated typed chart blocks, raw inline dashboard JSON, complete persisted layout options, sections, and groups.
Terraform owns the complete dashboard Template document. Place the dashboard in a Directory by including `/v2/template/${signalfx_observability_dashboard.service.id}` in the complete `templates` list of a `signalfx_observability_directory` resource.

## Example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_template" "chart" {
  title        = "Request rate"
  root_element = "Chart"

  spec = jsonencode({
    "<Chart>" = [{
      "<o11y:SingleValue>" = []
      chart                = {}
      datasource = {
        program = "A = data('requests.count').sum().publish('A')"
      }
      widget = {
        title = "Request rate"
      }
    }]
  })
}

resource "signalfx_observability_dashboard" "service" {
  title = "Service overview"

  container {
    layout {
      width  = "6/12"
      height = "2"
    }
    template {
      template_id = signalfx_observability_template.chart.id
    }
  }
}
```

## Inline content example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_dashboard" "inline_content" {
  title = "Inline dashboard content"

  container {
    layout {
      width  = "6/12"
      height = "2"
    }

    template {
      content = jsonencode({
        "<o11y:SingleValue>" = []
        chart = {
          color = "blue"
        }
        datasource = {
          program = "A = data('requests.count').sum().publish('A')"
        }
        widget = {
          title = "Request rate"
        }
      })
    }
  }
}
```

## Typed chart example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_dashboard" "typed_charts" {
  title = "Typed chart examples"

  # Typed charts can be direct dashboard containers.
  container {
    metrics_time_series {
      title         = "Request rate"
      program       = "A = data('requests.count').sum().publish('A')"
      time_range    = "1h"
      visualization = "line"

      y_axes = [{
        label   = "Requests per second"
        minimum = 0
      }]

      series = {
        A = {
          name          = "Requests"
          palette_index = 3
          display_unit = {
            suffix = "/s"
          }
        }
      }
    }
  }

  container {
    metrics_single_value {
      title      = "Success rate"
      program    = "A = data('requests.success').sum().publish('A')"
      time_range = "15m"

      display_unit = {
        suffix = "%"
      }

      color_scale = [
        {
          lt            = 99
          palette_index = 18
        },
        {
          gte           = 99
          palette_index = 11
        },
      ]
    }
  }

  container {
    text {
      title    = "Runbook"
      markdown = "[Open the service runbook](https://example.com/runbooks/checkout)"
    }
  }

  # A section container can hold a chart or a group.
  container {
    section {
      title = "Service details"

      container {
        metrics_list {
          title      = "Instances"
          program    = "A = data('requests.count').sum(by=['host']).publish('A')"
          time_range = "30m"
          color_by   = "Scale"

          color_scale = [
            {
              lt            = 100
              palette_index = 11
            },
            {
              gte           = 100
              palette_index = 18
            },
          ]

          display_fields = [{
            property = "host"
            enabled  = true
          }]

          sort = {
            by        = "value"
            direction = "desc"
          }

          published_streams = {
            A = {
              name          = "Requests"
              palette_index = 3
            }
          }
        }
      }

      # Group containers can hold typed charts too.
      container {
        group {
          title = "Breakdowns"

          container {
            metrics_table {
              title      = "Latency by service"
              program    = "A = data('service.latency').mean(by=['service.name']).publish('A')"
              time_range = "1h"
              group_by   = "service.name"

              columns = [{
                field       = "A"
                header_name = "Latency"
                enabled     = true
                display_unit = {
                  unit = "Millisecond"
                }
              }]
            }
          }

          container {
            metrics_cluster_map {
              title      = "CPU by node"
              program    = "A = data('cpu.utilization').mean(by=['k8s.cluster.name', 'k8s.node.name']).publish('A')"
              time_range = "15m"
              group_by   = ["k8s.cluster.name", "k8s.node.name"]
              color_by   = "Scale"

              display_unit = {
                suffix = "%"
              }

              color_scale = [
                {
                  lt            = 80
                  palette_index = 11
                },
                {
                  gte           = 80
                  palette_index = 18
                },
              ]
            }
          }
        }
      }
    }
  }
}
```

## Advanced layout example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_template" "latency" {
  title        = "Service latency"
  root_element = "Chart"

  spec = jsonencode({
    "<Chart>" = [{
      "<o11y:SingleValue>" = []
      chart                = {}
      datasource = {
        program = "A = data('latency.p99').mean().publish('A')"
      }
      widget = {
        title = "Service latency"
      }
    }]
  })
}

resource "signalfx_observability_dashboard" "advanced_layout" {
  title = "Advanced service layout"

  layout {
    gap  = 8
    step = 8

    defaults {
      min_width  = "4"
      min_height = "2"
    }
  }

  container {
    section {
      title       = "Service health"
      collapse    = false
      collapsible = true

      layout {
        gap  = 4
        step = 4
      }

      container {
        group {
          title      = "Latency details"
          headerless = true

          layout {
            gap  = 0
            step = 1

            defaults {
              height = "20"
            }
          }

          container {
            layout {
              absolute  = true
              width     = jsonencode({ value = "1/2", min = 4, max = "100%" })
              height    = "20"
              min_width = "4"
              max_width = "100%"
              x         = jsonencode(["1/4", 8])
              y         = "12"
            }

            template {
              template_id = signalfx_observability_template.latency.id
            }
          }
        }
      }
    }
  }
}
```

## Dashboard controls example

```terraform
terraform {
  required_providers {
    signalfx = {
      source = "splunk-terraform/signalfx"
    }
  }
}

resource "signalfx_observability_template" "control_chart" {
  title        = "Request rate"
  root_element = "Chart"

  spec = jsonencode({
    "<Chart>" = [{
      "<o11y:SingleValue>" = []
      chart                = {}
      datasource = {
        program = "A = data('requests.count').sum().publish('A')"
      }
      widget = {
        title = "Request rate"
      }
    }]
  })
}

resource "signalfx_observability_dashboard" "controls" {
  title = "Service health"

  control_bar {
    time_range {
      label                  = "Time Range"
      default_variable_value = "-15m"
    }

    density {
      default_variable_value = 60
    }

    pinned_filter {
      variable_name          = "service"
      key                    = "service.name"
      default_variable_value = ["checkout"]
      suggested_values       = ["checkout", "payments"]
      required               = true
      application_mode       = "add"
    }

    filter_set {
      hidden = false

      filter {
        key    = "deployment.environment"
        values = ["prod"]
      }
    }
  }

  container {
    template {
      template_id = signalfx_observability_template.control_chart.id
    }
  }
}
```

## Arguments

* `title` - (Required) Dashboard title.
* `layout` - (Optional) Settings for the root layout that arranges dashboard containers.
  * `gap` - (Optional) Visual gap between adjacent containers in pixels. Must be at least `0`.
  * `step` - (Optional) Layout resolution in pixels. Must be at least `1`.
  * `defaults` - (Optional) Default `absolute`, width, height, and min/max constraints inherited by root containers.
* `container` - (Optional) Ordered dashboard containers. Each root container contains exactly one `template`, typed chart, `section`, or `group` block.
  * `layout` - (Optional) Placement of this container in its parent layout. Supports `absolute`, `width`, `height`, `min_width`, `max_width`, `min_height`, `max_height`, `x`, and `y`.
  * `template` - (Optional) Dashboard panel content. Set exactly one of `template_id`, which imports a reusable Template by ID, or `content`, which accepts a self-contained dashboard JSON object rendered inline.
  * `metrics_time_series`, `metrics_list`, `metrics_single_value`, `metrics_table`, `metrics_cluster_map`, or `text` - (Optional) Typed chart content. The five metrics blocks require a non-empty SignalFlow `program`; their optional `time_range` must be a positive whole-number duration such as `15m` or `1h`. `text` has no datasource fields.
  * `section` - (Optional) Section with an optional `title`, `collapse`, `collapsible`, an optional child `layout`, and ordered `container` blocks. Omit `title` for an untitled section. Each section container contains exactly one `template`, typed chart, or `group` block.
  * `group` - (Optional) Group with an optional `title`, `headerless`, an optional child `layout`, and ordered `container` blocks. Omit `title` for an untitled group. Groups may be placed directly on a dashboard or inside a section. Each group container contains exactly one `template` or typed chart block.
* `control_bar` - (Optional) Dashboard controls applied to imported or inline panel content. Only configured controls are stored; the UI may add runtime defaults.
  * `time_range` - (Optional) The dashboard time-range control, always serialized as `TIME`. Its `default_variable_value` accepts values such as `-15m`, `-PT15M`, or an absolute time range.
  * `density` - (Optional) The chart density control, always serialized as `DENSITY`. `default_variable_value` must be `30`, `60`, `120`, or `240`.
  * `pinned_filter` - (Optional, Repeatable) An ordered filter control. `variable_name` is required and must be unique; `key` defaults to `variable_name`. `default_variable_value` and `suggested_values` are optional string lists. `application_mode` accepts `add`, `override`, or `ignore`; `override` only applies when a chart query already filters on the key.
  * `filter_set` - (Optional) The ad-hoc filter picker, always serialized as `FILTERS`. Its optional ordered `filter` blocks contain required `key` and `values`; an empty values list is a no-op. Each filter can also set `negated` or `disabled`.

All control blocks support optional `label`, `description`, and `hidden` fields. Pinned filters also support `only_suggest_preferred_values`, `match_missing`, and `required`. Controls are serialized in the canonical order `TIME`, `DENSITY`, pinned filters, `FILTERS`. Preferred suggestions are stored but are not currently consumed by the rendered filter descriptor. UI saves can add default singleton controls; complete-document ownership means a later Terraform update can remove controls not configured here.

### Typed chart fields

Every typed chart supports `title` and `description`. Each `metrics_*` chart also supports `program` (required and non-empty), `max_delay`, `minimum_resolution`, `sample_size`, and `time_range`. The `text` block has no datasource fields.

| Block | Additional fields | Required nested members and constraints |
| --- | --- | --- |
| `metrics_time_series` | `visualization`, `show_data_markers`, `show_event_lines`, `hide_missing_values`, `color_by`, `resolution`, `backfill_slice_count`, `use_binary_units`, `max_series`, `maximum_significant_digits`, `maximum_fraction_digits`, `show_legend`, `legend_position`, `show_series_value`, `can_toggle_series_visibility`, `legend_dimension`, `preferred_editor`, `no_data_message`, `links`, `y_axes`, `additional_properties`, `series` | `links[].url`; `additional_properties[].property` and `enabled` |
| `metrics_list` | `maximum_significant_digits`, `show_timestamp`, `secondary_visualization`, `published_streams`, `hide_missing_values`, `color_by`, `color_scale`, `display_fields`, `sort` | `display_fields[].property` and `enabled`; `sort.by` and `direction`; `color_scale` and `color_by = "Scale"` require each other |
| `metrics_single_value` | `maximum_significant_digits`, `maximum_fraction_digits`, `show_timestamp`, `secondary_visualization`, `display_unit`, `color_scale` | None beyond the display-unit rule below |
| `metrics_table` | `maximum_significant_digits`, `show_timestamp`, `group_by`, `hide_missing_values`, `disable_sampling`, `columns` | `columns[].field` |
| `metrics_cluster_map` | `maximum_significant_digits`, `show_timestamp`, `display_unit`, `group_by`, `color_by`, `color_range`, `color_scale` | `color_range.palette`; `color_scale` and `color_by = "Scale"` require each other; `color_range` and `color_scale` conflict |
| `text` | `markdown` | None |

Where available, a `display_unit` object requires either a named `unit` or at least one custom `prefix` or `suffix`; this also applies to display units nested under `series`, `published_streams`, and `columns`. `metrics_cluster_map.display_unit` accepts only a custom `prefix` or `suffix`, not a named unit. Service or UI defaults documented for optional chart fields are not serialized when those fields are omitted from configuration.

Within a `template` block, `template_id` and `content` are mutually exclusive and one is required. `content` is an opaque JSON escape hatch: it must be an object containing exactly one dashboard element key and is stored as the sole child of the panel. Both a direct element such as `<o11y:SingleValue>` and a standalone-style `<Chart>` wrapper are accepted. Use `template_id` rather than an inline `<$import.*>` element. Formatting and object-key ordering differences in `content` do not produce a Terraform change.

Typed chart blocks intentionally expose a curated Terraform surface rather than every field in the complete Splunk Observability Cloud chart contract. Use `template.content` for supported chart types with unmodeled fields, future chart types, or other content that must be preserved without dropping fields. Typed charts are written as direct `<o11y:...>` panel elements. On import, a clean `<Chart>` wrapper containing exactly one losslessly representable supported chart is canonicalized to the corresponding direct typed block.

For a panel with no prior Terraform representation (including ID-only import), automatic typed-chart selection happens only when every chart field can be represented losslessly. Otherwise, the provider preserves the element in `template.content` and emits a warning. A chart already represented by `template.content` remains raw on refresh. If a chart already represented by a typed block later contains unsupported or incorrectly shaped data, refresh returns an error instead of dropping data or silently changing it to raw content.

Root, section, and group `layout` blocks configure the layout that arranges their immediate child containers. A `container.layout` block instead configures that single container's placement inside its parent.

All length arguments are strings. Use ordinary values such as `"4"`, `"6/12"`, or `"50%"` for simple lengths. Use `jsonencode({ value = "1/2", min = 4, max = "100%" })` for a clamped length, and use a JSON-encoded array only for a multi-part `x` or `y` coordinate.

Terraform container declaration order is authoritative. Reordering containers in the UI is accepted on refresh but is not retained by the next Terraform update. `group.headerless` controls the group header; a chart's widget header remains part of the referenced Template's `widget.headerless` configuration.

## Attributes

* `id` - The dashboard Template record ID.

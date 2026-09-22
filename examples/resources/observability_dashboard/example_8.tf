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

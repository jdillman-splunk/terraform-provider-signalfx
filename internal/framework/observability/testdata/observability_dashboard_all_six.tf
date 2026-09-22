resource "signalfx_observability_dashboard" "all_six" {
  title = "All six generated charts"

  container {
    metrics_time_series {
      title   = "Time series"
      program = "data('demo.time').publish(label='A')"
    }
  }

  container {
    metrics_single_value {
      title   = "Single value"
      program = "data('demo.single').publish(label='A')"
    }
  }

  container {
    metrics_list {
      title   = "List"
      program = "data('demo.list').publish(label='A')"
    }
  }

  container {
    metrics_table {
      title   = "Table"
      program = "data('demo.table').publish(label='A')"
    }
  }

  container {
    metrics_cluster_map {
      title   = "Cluster map"
      program = "data('demo.cluster').publish(label='A')"
    }
  }

  container {
    text {
      title    = "Notes"
      markdown = "# Initial notes"
    }
  }
}

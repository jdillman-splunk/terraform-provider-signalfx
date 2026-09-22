resource "signalfx_observability_dashboard" "all_six" {
  title = "All six generated charts updated"

  container {
    metrics_time_series {
      title   = "Time series updated"
      program = "data('demo.time.updated').publish(label='A')"
    }
  }

  container {
    metrics_single_value {
      title   = "Single value updated"
      program = "data('demo.single.updated').publish(label='A')"
    }
  }

  container {
    metrics_list {
      title   = "List updated"
      program = "data('demo.list.updated').publish(label='A')"
    }
  }

  container {
    metrics_table {
      title   = "Table updated"
      program = "data('demo.table.updated').publish(label='A')"
    }
  }

  container {
    metrics_cluster_map {
      title   = "Cluster map updated"
      program = "data('demo.cluster.updated').publish(label='A')"
    }
  }

  container {
    text {
      title    = "Notes updated"
      markdown = "# Updated notes"
    }
  }
}

resource "signalfx_observability_chart" "test" {
  title = "Reusable request rate"

  metrics_time_series {
    title   = "Request rate"
    program = "data('requests.count').sum().publish(label='A')"
  }
}

resource "signalfx_observability_chart" "test" {
  title = "Reusable runbook"

  text {
    title    = "Runbook"
    markdown = "# Service runbook"
  }
}

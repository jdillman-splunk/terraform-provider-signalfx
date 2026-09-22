// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

// Command chartgen turns the six supported charts/schemas/*.yml chart
// schemas into generated child-package models plus the dashboard transport
// bridge. The checked-in schema set is intentionally exact for Part 2.
//
// Usage: go run ./internal/framework/observability/chartgen -mode=write|check [-repo-root=path]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	mode := flag.String("mode", "", "generation mode: write or check")
	repoRoot := flag.String("repo-root", ".", "repository root containing the Observability chart sources")
	flag.Parse()
	if flag.NArg() != 0 || (*mode != "write" && *mode != "check") {
		fmt.Fprintln(os.Stderr, "usage: chartgen -mode=write|check [-repo-root=path]")
		os.Exit(1)
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chartgen:", err)
		os.Exit(1)
	}
	options := generationOptions{
		SchemasDir:           filepath.Join(root, "internal", "framework", "observability", "charts", "schemas"),
		OutDir:               filepath.Join(root, "internal", "framework", "observability", "charts"),
		PackageName:          "charts",
		BridgePath:           filepath.Join(root, "internal", "framework", "observability", "dashboard", "dashboard_charts_generated.go"),
		LegacyGeneratedPaths: []string{filepath.Join(root, "internal", "framework", "observability", "dashboard_charts_generated.go")},
		BridgePackage:        "dashboard",
		ChartsImport:         "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/observability/charts",
		Check:                *mode == "check",
	}
	if err := runGeneration(options); err != nil {
		fmt.Fprintln(os.Stderr, "chartgen:", err)
		os.Exit(1)
	}
}

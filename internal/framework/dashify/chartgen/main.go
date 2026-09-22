// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

// See doc.go for the package overview, DSL summary, and the walkthrough for
// adding a new chart element.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	mode := flag.String("mode", "", "generation mode: write, check, or scaffold")
	repoRoot := flag.String("repo-root", ".", "repository root containing the Observability chart sources")
	contract := flag.String("contract", "", "scaffold source contract basename from charts/schemas/elements")
	pointer := flag.String("pointer", "#", "scaffold source pointer: # or #/$defs/<name>")
	chartType := flag.String("type", "", "Terraform chart type for a complete scaffold")
	flag.Parse()
	if flag.NArg() != 0 || (*mode != "write" && *mode != "check" && *mode != "scaffold") {
		printUsage()
		os.Exit(1)
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chartgen:", err)
		os.Exit(1)
	}
	schemasDir := filepath.Join(root, "internal", "framework", "dashify", "charts", "schemas")
	if *mode == "scaffold" {
		output, warnings, scaffoldErr := scaffoldContract(scaffoldOptions{
			ContractsDir: filepath.Join(schemasDir, "elements"),
			Contract:     *contract,
			Pointer:      *pointer,
			Type:         *chartType,
		})
		if scaffoldErr != nil {
			fmt.Fprintln(os.Stderr, "chartgen:", scaffoldErr)
			os.Exit(1)
		}
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, "chartgen: warning:", warning)
		}
		if _, err := os.Stdout.Write(output); err != nil {
			fmt.Fprintln(os.Stderr, "chartgen:", err)
			os.Exit(1)
		}
		return
	}
	if *contract != "" || *pointer != "#" || *chartType != "" {
		fmt.Fprintln(os.Stderr, "chartgen: -contract, -pointer, and -type are only valid with -mode=scaffold")
		os.Exit(1)
	}
	options := generationOptions{
		SchemasDir:           schemasDir,
		OutDir:               filepath.Join(root, "internal", "framework", "dashify", "charts"),
		PackageName:          "charts",
		BridgePath:           filepath.Join(root, "internal", "framework", "dashify", "dashboard", "dashboard_charts_generated.go"),
		LegacyGeneratedPaths: []string{filepath.Join(root, "internal", "framework", "dashify", "dashboard_charts_generated.go")},
		BridgePackage:        "dashboard",
		ChartsImport:         "github.com/splunk-terraform/terraform-provider-signalfx/internal/framework/dashify/charts",
		Check:                *mode == "check",
	}
	if err := runGeneration(options); err != nil {
		fmt.Fprintln(os.Stderr, "chartgen:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: chartgen -mode=write|check [-repo-root=path]")
	fmt.Fprintln(os.Stderr, "       chartgen -mode=scaffold -contract=<file.schema.json> -type=<chart_type> [-pointer=#] [-repo-root=path]")
	fmt.Fprintln(os.Stderr, "       chartgen -mode=scaffold -contract=<file.schema.json> -pointer=#/$defs/<name> [-repo-root=path]")
}

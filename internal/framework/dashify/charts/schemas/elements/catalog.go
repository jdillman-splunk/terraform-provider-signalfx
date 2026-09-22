// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package elements exposes the pinned Olly JSON Schema catalog used to check
// Terraform's generated Dashify chart documents.
package elements

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const Draft2020 = "https://json-schema.org/draft/2020-12/schema"

// Definition identifies one schema artifact and its Dashify element tag.
type Definition struct {
	File     string
	Element  string
	SchemaID string
}

// Files contains the vendored schemas. Adding or removing a chart element is
// just adding or removing an O11y*.schema.json file here: Catalog discovers
// each file's Definition from its own "$id" and "title" fields below.
//
//go:embed O11y*.schema.json
var Files embed.FS

// Catalog lists every embedded schema, discovered from the files themselves.
var Catalog = discoverCatalog()

func discoverCatalog() []Definition {
	entries, err := Files.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("elements: reading embedded schemas: %v", err))
	}
	var catalog []Definition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		contents, err := Files.ReadFile(entry.Name())
		if err != nil {
			panic(fmt.Sprintf("elements: reading %s: %v", entry.Name(), err))
		}
		var document struct {
			ID    string `json:"$id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(contents, &document); err != nil {
			panic(fmt.Sprintf("elements: parsing %s: %v", entry.Name(), err))
		}
		if document.ID == "" || document.Title == "" {
			panic(fmt.Sprintf("elements: %s has no $id or title", entry.Name()))
		}
		catalog = append(catalog, Definition{
			File:     entry.Name(),
			Element:  "<" + document.Title + ">",
			SchemaID: document.ID,
		})
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].File < catalog[j].File })
	return catalog
}

// Read returns one cataloged schema. It refuses arbitrary paths so callers
// cannot read arbitrary embedded files by constructing a bogus Definition.
func Read(definition Definition) ([]byte, error) {
	for _, candidate := range Catalog {
		if definition.File == candidate.File {
			return Files.ReadFile(candidate.File)
		}
	}
	return nil, fmt.Errorf("dashify element schema %q is not in the catalog", definition.File)
}

// Find returns the schema definition for an exact serialized element tag.
func Find(element string) (Definition, bool) {
	for _, definition := range Catalog {
		if definition.Element == element {
			return definition, true
		}
	}
	return Definition{}, false
}

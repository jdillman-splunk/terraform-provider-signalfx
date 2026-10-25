// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package interpreter

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeObjectPreservesNumbersAndRequiresObject(t *testing.T) {
	for name, raw := range map[string]string{
		"array root": `[]`,
		"null root":  `null`,
		"incomplete": `{"chart":[}`,
		"empty":      ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeObject(raw); err == nil {
				t.Fatal("accepted malformed JSON or a non-object root")
			}
		})
	}
	object, err := DecodeObject(`{"large":9007199254740993,"null":null,"empty":[],"nested":[{"value":1.0}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if object["large"] != json.Number("9007199254740993") || !reflect.DeepEqual(object["empty"], []any{}) {
		t.Fatalf("lost numeric or empty array semantics: %#v", object)
	}
	wire, err := json.Marshal(object)
	if err != nil || !strings.Contains(string(wire), `"value":1.0`) {
		t.Fatalf("lost numeric spelling: %s, %v", wire, err)
	}
}

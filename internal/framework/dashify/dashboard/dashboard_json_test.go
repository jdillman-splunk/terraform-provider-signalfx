// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeDashifyJSONPreservesExactNumbers(t *testing.T) {
	t.Parallel()

	var decoded map[string]any
	require.NoError(t, decodeDashifyJSON([]byte(`{"integer":9007199254740993,"decimal":1.0,"negativeZero":-0}`), &decoded))
	assert.Equal(t, json.Number("9007199254740993"), decoded["integer"])
	assert.Equal(t, json.Number("1.0"), decoded["decimal"])
	assert.Equal(t, json.Number("-0"), decoded["negativeZero"])

	inline, err := decodeDashifyInlineContent(`{"<o11y:Text>":[],"chart":{"markdown":"notes","future":9007199254740993},"widget":{}}`)
	require.NoError(t, err)
	encoded, err := json.Marshal(inline)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"future":9007199254740993`)
}

func TestDashifyExactInt64AcceptsJSONNumberWithoutFloatRounding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  int64
		ok    bool
	}{
		{name: "above float exact range", value: json.Number("9007199254740993"), want: 9007199254740993, ok: true},
		{name: "integral decimal", value: json.Number("30.0"), want: 30, ok: true},
		{name: "integral exponent", value: json.Number("3e1"), want: 30, ok: true},
		{name: "max", value: json.Number("9223372036854775807"), want: math.MaxInt64, ok: true},
		{name: "min", value: json.Number("-9223372036854775808"), want: math.MinInt64, ok: true},
		{name: "fraction", value: json.Number("1.5"), ok: false},
		{name: "overflow", value: json.Number("9223372036854775808"), ok: false},
		{name: "rounded float overflow", value: float64(9223372036854775807), ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := dashifyExactInt64(test.value)
			assert.Equal(t, test.ok, ok)
			if test.ok {
				assert.Equal(t, test.want, got)
			}
		})
	}
}

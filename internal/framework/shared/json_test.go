// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwshared

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSemanticallyEqualJSON(t *testing.T) {
	t.Parallel()

	assert.True(t, SemanticallyEqualJSON(`{"a":1,"b":2}`, "{\"b\": 2, \"a\": 1}"))
	assert.True(t, SemanticallyEqualJSON(`{"a":1.0,"b":-0}`, `{"b":0,"a":1e0}`))
	assert.False(t, SemanticallyEqualJSON(`{"a":1}`, `{"a":2}`))
	assert.False(t, SemanticallyEqualJSON(`{"a":9007199254740992}`, `{"a":9007199254740993}`))
}

// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwshared

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// SemanticJSONFingerprint returns a stable hash of a JSON value. Object key
// order and insignificant number spelling are erased, while array order and
// JSON value types remain significant.
func SemanticJSONFingerprint(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", errors.New("decode JSON: multiple values")
		}
		return "", fmt.Errorf("decode JSON trailer: %w", err)
	}

	var canonical strings.Builder
	if err := writeCanonicalJSON(&canonical, value); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:]), nil
}

// SemanticallyEqualJSON compares JSON values without floating-point rounding
// or treating object key order and insignificant number spelling as changes.
func SemanticallyEqualJSON(left, right string) bool {
	leftFingerprint, err := SemanticJSONFingerprint(json.RawMessage(left))
	if err != nil {
		return false
	}
	rightFingerprint, err := SemanticJSONFingerprint(json.RawMessage(right))
	if err != nil {
		return false
	}
	return leftFingerprint == rightFingerprint
}

// JSONSemanticEqualityModifier preserves prior state for semantically equal
// configured JSON.
type JSONSemanticEqualityModifier struct{}

func (JSONSemanticEqualityModifier) Description(_ context.Context) string {
	return "Treats JSON content as unchanged when it is semantically equivalent to the prior value."
}

func (m JSONSemanticEqualityModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (JSONSemanticEqualityModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if SemanticallyEqualJSON(req.StateValue.ValueString(), req.ConfigValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

func writeCanonicalJSON(target *strings.Builder, value any) error {
	switch value := value.(type) {
	case nil:
		target.WriteString("null;")
	case bool:
		if value {
			target.WriteString("bool:1;")
		} else {
			target.WriteString("bool:0;")
		}
	case string:
		writeCanonicalString(target, "string", value)
	case json.Number:
		number, ok := new(big.Rat).SetString(value.String())
		if !ok {
			return fmt.Errorf("normalize JSON number %q", value)
		}
		writeCanonicalString(target, "number", number.RatString())
	case []any:
		target.WriteString("array[")
		for _, item := range value {
			if err := writeCanonicalJSON(target, item); err != nil {
				return err
			}
		}
		target.WriteString("];")
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		target.WriteString("object{")
		for _, key := range keys {
			writeCanonicalString(target, "key", key)
			if err := writeCanonicalJSON(target, value[key]); err != nil {
				return err
			}
		}
		target.WriteString("};")
	default:
		return fmt.Errorf("canonicalize decoded JSON value of type %T", value)
	}
	return nil
}

func writeCanonicalString(target *strings.Builder, kind, value string) {
	target.WriteString(kind)
	target.WriteByte(':')
	target.WriteString(strconv.Itoa(len(value)))
	target.WriteByte(':')
	target.WriteString(value)
	target.WriteByte(';')
}

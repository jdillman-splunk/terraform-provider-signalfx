// Copyright Splunk, Inc.
// SPDX-License-Identifier: MPL-2.0

package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
)

// decodeDashifyJSON keeps JSON numbers in their exact lexical form. Chart
// conversion decides later whether a value fits the Terraform type that
// represents it; decoding the complete document through float64 first would
// already have rounded integers above 2^53 and made that loss undetectable.
func decodeDashifyJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("contains multiple JSON values")
		}
		return err
	}
	return nil
}

func dashifyFiniteNumber(raw any) (float64, bool) {
	var number float64
	switch value := raw.(type) {
	case float64:
		number = value
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func dashifyExactInt64(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int64:
		return value, true
	case float64:
		// float64(math.MaxInt64) rounds up to 2^63, so the upper check must
		// be exclusive rather than comparing with math.MaxInt64 directly.
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
			value >= math.Exp2(63) || value < -math.Exp2(63) {
			return 0, false
		}
		return int64(value), true
	case json.Number:
		rational, ok := new(big.Rat).SetString(value.String())
		if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
			return 0, false
		}
		return rational.Num().Int64(), true
	default:
		return 0, false
	}
}

// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubestore

import (
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// The bounds on what parseQuantity will parse. ParseQuantity rounds every value up to nano
// precision, which for 1e-N builds a power of ten of about N digits: 1e-999999999, twelve
// bytes, does not finish. At ±1,000 a value is already past a float's range or rounds up
// to 1n, so the bound refuses nothing a float could carry.
const (
	quantityMaxLength   = 64
	quantityMaxExponent = 1000
)

// parseQuantity reads a Kubernetes quantity as a float: cores for CPU, bytes for memory.
// False for text that is not a quantity, for a value no float holds, and for text past the
// bounds, which it refuses before parsing.
func parseQuantity(s string) (float64, bool) {
	if len(s) > quantityMaxLength {
		return 0, false
	}
	if exp, ok := exponent(s); ok && (exp > quantityMaxExponent || exp < -quantityMaxExponent) {
		return 0, false
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0, false
	}
	f := q.AsApproximateFloat64()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// exponent reads a decimal exponent, an e or E then a signed integer, off the end of s. A
// bare E is the exa suffix, not an exponent. An exponent too large for an int reads as the
// largest one, which the bound refuses.
func exponent(s string) (int, bool) {
	i := strings.LastIndexAny(s, "eE")
	if i <= 0 {
		return 0, false
	}
	digits := s[i+1:]
	if digits != "" && (digits[0] == '+' || digits[0] == '-') {
		digits = digits[1:]
	}
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	exp, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return math.MaxInt, true
	}
	return exp, true
}

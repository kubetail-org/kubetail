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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuantityReadsAKubernetesQuantity(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]float64{
		"250m": 0.25,
		"2":    2,
		"1Gi":  1 << 30,
		"1e3":  1000,
		"1E":   1e18, // an exa, not an exponent
	} {
		got, ok := parseQuantity(in)
		assert.True(t, ok, in)
		assert.InDelta(t, want, got, want*1e-9, in)
	}
	for _, in := range []string{"", "junk", "1e400"} {
		_, ok := parseQuantity(in)
		assert.False(t, ok, in)
	}
}

// Parsing costs far more than the text's length: 1e-999999999 builds a power of ten a
// billion digits long. The bounds refuse such text before the parse, so each answer is
// immediate.
func TestParseQuantityRefusesWhatItCannotAfford(t *testing.T) {
	t.Parallel()

	for _, in := range []string{strings.Repeat("9", 64), "1e-1000"} {
		_, ok := parseQuantity(in)
		assert.True(t, ok, in)
	}
	// require, so a bound that is missing fails here rather than hanging on the next case.
	for _, in := range []string{strings.Repeat("9", 65), "1e-1001", "1e1001", "1E+99999999", "1e99999999999999999999", "1e-999999999"} {
		start := time.Now()
		_, ok := parseQuantity(in)
		require.False(t, ok, in)
		require.Less(t, time.Since(start), time.Second, in)
	}
}

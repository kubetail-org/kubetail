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

package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// As reads the Refusal out of a bare refusal, an Unmirrored or a NoNodes, wrapped
// or not, and finds none in any other error.
func TestAsReadsTheRefusal(t *testing.T) {
	t.Parallel()
	want := New(CodeNotMirrored, "no pods")
	for name, err := range map[string]error{
		"bare":       want,
		"wrapped":    fmt.Errorf("fetch: %w", want),
		"unmirrored": Unmirrored{Refusal: want, Source: -1},
		"no nodes":   fmt.Errorf("fetch: %w", NoNodes{Refusal: want}),
	} {
		got, ok := As(err)
		assert.True(t, ok, name)
		assert.Equal(t, want, got, name)
	}

	_, ok := As(errors.New("boom"))
	assert.False(t, ok)
	_, ok = As(nil)
	assert.False(t, ok)
}

// A refusal's error text is its Reason alone.
func TestARefusalReadsAsItsReason(t *testing.T) {
	t.Parallel()
	assert.EqualError(t, New(CodeBadGrep, "the grep does not compile"), "the grep does not compile")
}

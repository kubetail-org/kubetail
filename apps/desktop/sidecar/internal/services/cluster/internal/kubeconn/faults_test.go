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

package kubeconn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both builds: an empty spec is the zero Faults, which adds no layer and fails no store.
func TestAnEmptySpecIsNoFaults(t *testing.T) {
	t.Parallel()

	faults, err := ParseFaults("")

	require.NoError(t, err)
	assert.Equal(t, Faults{}, faults)
	assert.False(t, faults.StoreFails())
	assert.Nil(t, faults.wrapper(""))
}

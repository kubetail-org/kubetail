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

package bash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A Go program starts a thread per CPU and a few more, and Linux counts each,
// so the margin grows with the CPUs from a floor of 1024.
func TestTheProcessMarginGrowsWithTheCPUs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1024, processMargin(1))
	assert.Equal(t, 1024, processMargin(8))
	assert.Equal(t, 4096, processMargin(32))
}

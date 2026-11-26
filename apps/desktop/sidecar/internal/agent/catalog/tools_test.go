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

package catalog

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
)

// Every tool on a list has a listing, and every listing is of a tool on a list, so
// Settings lists exactly the tools a chat can be offered.
func TestToolListingsMatchTheLists(t *testing.T) {
	listed := map[string]bool{}
	for _, list := range lists {
		for _, name := range list {
			listed[name] = true
		}
	}
	var names []string
	for _, l := range ToolListings() {
		assert.True(t, listed[l.Name], "%s is on no list", l.Name)
		assert.NotContains(t, names, l.Name, "%s is listed twice", l.Name)
		assert.NotEmpty(t, l.Label, l.Name)
		assert.NotEmpty(t, l.Description, l.Name)
		assert.NotEmpty(t, l.Group, l.Name)
		names = append(names, l.Name)
	}
	for name := range listed {
		assert.True(t, slices.Contains(names, name), "%s has no listing", name)
	}
}

// TaskStop alone is always on: a turn can always stop the work it started.
func TestOnlyTaskStopIsAlwaysOn(t *testing.T) {
	c := New(Config{})
	assert.True(t, c.AlwaysOn(taskstop.Name))
	assert.False(t, c.AlwaysOn(bash.Name))
	assert.False(t, c.AlwaysOn("Gone"))
}

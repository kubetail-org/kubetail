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

package realpath

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRelationTellsOneFolderInsideAndApart(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dirs := mkdirs(t, base, "state/logs", "cache")
	state, logs, cache := filepath.Dir(dirs[0]), dirs[0], dirs[1]

	assert.Equal(t, SameFolder, Relation(state, state))
	assert.Equal(t, Inside, Relation(logs, state))
	assert.Equal(t, Apart, Relation(state, logs), "the outer folder is not inside the inner")
	assert.Equal(t, Apart, Relation(cache, state))
}

// A folder reached through a link is the folder it leads to.
func TestRelationComparesThroughALink(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	logs := mkdirs(t, base, "state/logs")[0]
	name := filepath.Join(base, "link")
	link(t, filepath.Dir(logs), name)

	assert.Equal(t, SameFolder, Relation(name, filepath.Dir(logs)))
	assert.Equal(t, Inside, Relation(logs, name))
}

// Names past the deepest folder that exists are compared as given.
func TestRelationComparesAMissingTailByItsText(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	state := filepath.Join(base, "state")

	assert.Equal(t, SameFolder, Relation(state, state))
	assert.Equal(t, Inside, Relation(filepath.Join(state, "logs"), state))
	assert.Equal(t, Apart, Relation(filepath.Join(base, "State", "logs"), state))
	assert.Equal(t, Apart, Relation(filepath.Join(base, "stateful"), state), "a name that only starts like it")
}

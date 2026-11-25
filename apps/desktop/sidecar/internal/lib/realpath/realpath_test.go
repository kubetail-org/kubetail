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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Resolved first, since Windows can hand out a temp dir under its short name.
func TestCanonicalKeepsAPathTheDiskLists(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Code", "svc"), 0o755))
	svc := filepath.Join(dir, "Code", "svc")
	paths := []string{svc, filepath.Join(dir, "Code", "missing", "x")}
	got, err := Canonical(paths)
	require.NoError(t, err)
	assert.Equal(t, paths, got, "a path as listed, and one that is not kept as given")
}

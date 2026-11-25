//go:build !windows

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

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

func TestFileOfTellsFilesApart(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "a", "b")
	file := filepath.Join(d[0], "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(d[0], link))

	a, ok := fileOf(d[0])
	require.True(t, ok)
	again, _ := fileOf(d[0] + "/.")
	assert.Equal(t, a, again)
	b, _ := fileOf(d[1])
	assert.NotEqual(t, a, b)
	l, ok := fileOf(link)
	require.True(t, ok)
	assert.NotEqual(t, a, l, "a link is a file of its own")
	_, ok = fileOf(file)
	assert.True(t, ok)
	_, ok = fileOf(filepath.Join(base, "missing"))
	assert.False(t, ok)

	// A hard-linked file is one each of its folders holds.
	require.NoError(t, os.Link(file, filepath.Join(d[1], "file")))
	_, ok = fileOf(file)
	assert.False(t, ok)
}

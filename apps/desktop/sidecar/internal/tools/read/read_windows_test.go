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

//go:build windows

package read

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A UNC or device path is refused before anything touches it: opening one
// would reach out to another machine. It is never asked about.
func TestReadRefusesAPathOffThisMachine(t *testing.T) {
	_, tl, rt := chat(t)
	for _, path := range []string{`\\host\share\x.txt`, `\\?\C:\x.txt`, `\\.\x`} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, path), path)
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, "Read opens files on this machine only.", text, path)
	}
}

// A saved file named in Git Bash's form of its drive path is read as the native
// path it stands for.
func TestReadTakesAGitBashDrivePath(t *testing.T) {
	dir, tl, rt := chat(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "out.txt"), []byte("a\n"), 0o600))
	vol := filepath.VolumeName(dir)
	gitBash := "/" + strings.ToLower(vol[:1]) + filepath.ToSlash(dir[len(vol):]) + "/out.txt"

	text, isError := tl.Run(t.Context(), rt, call(gitBash))
	assert.False(t, isError, gitBash)
	assert.Equal(t, "     1\ta\n", text)
}

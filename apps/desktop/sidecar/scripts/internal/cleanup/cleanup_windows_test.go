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

package cleanup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Windows refuses to rename a folder while a file under it is open, as the
// running sidecar's lock is.
func TestAnAppFolderWithAnOpenFileIsRunning(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Kubetail")
	require.NoError(t, os.MkdirAll(filepath.Join(app, "data"), 0o700))
	f, err := os.Create(filepath.Join(app, "data", lockFile))
	require.NoError(t, err)
	defer f.Close()

	assert.ErrorContains(t, refuseWhileHeld(app), "quit Kubetail first")
}

func TestAnAppFolderWithNothingOpenIsNotRunning(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Kubetail")
	require.NoError(t, os.MkdirAll(filepath.Join(app, "data"), 0o700))

	require.NoError(t, refuseWhileHeld(app))
	assert.DirExists(t, app)
}

func TestAMissingAppFolderIsNotRunning(t *testing.T) {
	assert.NoError(t, refuseWhileHeld(filepath.Join(t.TempDir(), "Kubetail")))
}

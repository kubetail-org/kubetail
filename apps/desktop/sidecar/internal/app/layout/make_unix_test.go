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

//go:build !windows

package layout

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mode(t *testing.T, dir string) os.FileMode {
	t.Helper()
	info, err := os.Stat(dir)
	require.NoError(t, err)
	return info.Mode() & (os.ModePerm | os.ModeSetgid)
}

// Serial: it sets the umask, to show the mode is not the umask's.
func TestMakeOwnerOnlyMakesAndTightensTo0700(t *testing.T) {
	l := macOSUnder(t)
	setUmask(t, 0o022)

	require.NoError(t, MakeOwnerOnly(l, l.Data))
	assert.Equal(t, os.FileMode(0o700), mode(t, l.Data))

	// An earlier build left it open.
	require.NoError(t, os.MkdirAll(l.Cache, 0o755))
	require.NoError(t, os.Chmod(l.Cache, 0o755))
	require.NoError(t, MakeOwnerOnly(l, l.Cache))
	assert.Equal(t, os.FileMode(0o700), mode(t, l.Cache))
}

// Whoever can write a folder can replace what is in it, so an open app folder
// is tightened before a kind in it is made; one already owner-only keeps its
// other bits.
func TestMakeOwnerOnlyTightensAnOpenAppFolderAbove(t *testing.T) {
	l := macOSUnder(t)
	require.NoError(t, os.MkdirAll(l.App, 0o700))
	require.NoError(t, os.Chmod(l.App, 0o775))

	require.NoError(t, MakeOwnerOnly(l, l.Data))
	assert.Equal(t, os.FileMode(0o700), mode(t, l.App))

	require.NoError(t, os.Chmod(l.App, 0o700|os.ModeSetgid))
	require.NoError(t, MakeOwnerOnly(l, l.Data))
	assert.Equal(t, 0o700|os.ModeSetgid, mode(t, l.App))
}

// Linux keeps the logs in the state folder, which is tightened likewise.
func TestMakeOwnerOnlyTightensAnOpenStateFolderAboveTheLogs(t *testing.T) {
	root := t.TempDir()
	l := Of(Linux, true, Roots{LocalData: root, Cache: root, Home: root, State: filepath.Join(root, "state")})
	require.NoError(t, os.MkdirAll(l.State, 0o700))
	require.NoError(t, os.Chmod(l.State, 0o777))

	require.NoError(t, MakeOwnerOnly(l, l.Logs))
	assert.Equal(t, os.FileMode(0o700), mode(t, l.State))
	assert.Equal(t, os.FileMode(0o700), mode(t, l.Logs))
}

func setUmask(t *testing.T, mask int) {
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

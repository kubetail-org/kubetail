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

package cleanup

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortDir is a fresh directory whose socket paths fit sun_path, which
// t.TempDir's may not on macOS.
func shortDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "kt")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestALiveSocketIsRunning(t *testing.T) {
	dir := shortDir(t)
	ln, err := net.Listen("unix", filepath.Join(dir, "kubetail-sidecar-1.sock"))
	require.NoError(t, err)
	defer ln.Close()

	assert.ErrorContains(t, refuseWhileListening([]string{filepath.Join(dir, "missing"), dir}), "quit Kubetail first")
}

// A crashed run leaves its socket file behind, which refuses the dial.
func TestALeftoverSocketIsNotRunning(t *testing.T) {
	dir := shortDir(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "kubetail-sidecar-1.sock"), Net: "unix"})
	require.NoError(t, err)
	ln.SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())

	assert.NoError(t, refuseWhileListening([]string{dir}))
}

func TestOwnedDirsKeepsOnlyThisUsersDirectories(t *testing.T) {
	top := t.TempDir()
	dir := filepath.Join(top, "kubetail-501")
	file := filepath.Join(top, "kubetail-501-aaaaaa")
	link := filepath.Join(top, "kubetail-501-bbbbbb")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.NoError(t, os.Symlink(dir, link))

	assert.Equal(t, []string{dir}, OwnedDirs([]string{dir, file, link, filepath.Join(top, "missing")}))
}

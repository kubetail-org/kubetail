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

package ipc

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staleSocket leaves a socket at path that refuses a dial, as a killed
// sidecar does: bound, then closed without the unlink Close does by default.
func staleSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	require.NoError(t, err)
	ln.SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
}

func TestSweepStaleRemovesASocketThatRefusesTheDial(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	stale := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	staleSocket(t, stale)

	SweepStale(dir, filepath.Join(dir, "kubetail-sidecar-2-0.sock"))

	assert.NoFileExists(t, stale)
}

func TestSweepStaleLeavesALiveSocket(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	live := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	ln, err := net.Listen("unix", live)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	SweepStale(dir, filepath.Join(dir, "kubetail-sidecar-2-0.sock"))

	assert.FileExists(t, live)
}

func TestSweepStaleLeavesItsOwnSocket(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	own := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	staleSocket(t, own)

	SweepStale(dir, own)

	assert.FileExists(t, own)
}

func TestSweepStaleLeavesWhatIsNotASidecarSocket(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	file := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	link := filepath.Join(dir, "kubetail-sidecar-1-1.sock")
	target := filepath.Join(dir, "target.sock")
	staleSocket(t, target)
	require.NoError(t, os.Symlink(target, link))
	other := filepath.Join(dir, "other.sock")
	staleSocket(t, other)

	SweepStale(dir, filepath.Join(dir, "kubetail-sidecar-2-0.sock"))

	assert.FileExists(t, file)
	_, err := os.Lstat(link)
	assert.NoError(t, err, "the link")
	assert.FileExists(t, target)
	assert.FileExists(t, other)
}

// A dial that fails but is not refused may still reach a listener.
func TestSweepStaleLeavesASocketItCannotDial(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	sock := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	staleSocket(t, sock)
	require.NoError(t, os.Chmod(sock, 0))

	SweepStale(dir, filepath.Join(dir, "kubetail-sidecar-2-0.sock"))

	assert.FileExists(t, sock)
}

func TestSweepStaleLeavesASocketItCannotRemove(t *testing.T) {
	dir := filepath.Dir(testEndpoint(t))
	stale := filepath.Join(dir, "kubetail-sidecar-1-0.sock")
	staleSocket(t, stale)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	SweepStale(dir, filepath.Join(dir, "kubetail-sidecar-2-0.sock"))

	assert.FileExists(t, stale)
}

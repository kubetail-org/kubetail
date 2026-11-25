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

package fileguard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// watchOpens is an inotify watch on path for every open, read without blocking.
func watchOpens(t *testing.T, path string) int {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	require.NoError(t, err)
	t.Cleanup(func() { _ = unix.Close(fd) })
	_, err = unix.InotifyAddWatch(fd, path, unix.IN_OPEN)
	require.NoError(t, err)
	return fd
}

// noOpens asserts that nothing opened the watched path. The kernel queues an
// event before the open returns, so an empty queue now is no open, with no
// window to wait out.
func noOpens(t *testing.T, fd int) {
	t.Helper()
	_, err := unix.Read(fd, make([]byte, 4096))
	assert.True(t, errors.Is(err, unix.EAGAIN), "an open was seen: %v", err)
}

// A directory made under a setgid parent keeps the bit Linux gave it, so the
// tree's group still reaches every file made in it. The parent here is in the
// test user's own group; for a user outside it, chmod drops the bit, the
// record's stated residual.
func TestCreateKeepsASetgidDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o700|os.ModeSetgid))
	parent, err := os.Lstat(root)
	require.NoError(t, err)

	require.NoError(t, Create(t.Context(), filepath.Join(root, "a", "x.txt"), nil, 0o022))
	info, err := os.Lstat(filepath.Join(root, "a"))
	require.NoError(t, err)
	assert.Equal(t, os.ModeDir|os.ModeSetgid|0o755, info.Mode())
	assert.Equal(t, gidOf(t, parent), gidOf(t, info))
}

// Neither check opens the file or the directory: an open for writing would
// fire IN_CLOSE_WRITE for a watcher of a file nothing wrote.
func TestReplaceableAndCreatableOpenNothing(t *testing.T) {
	path, info := existing(t)
	fd := watchOpens(t, path)
	require.NoError(t, Replaceable(path, info))
	noOpens(t, fd)

	dir := t.TempDir()
	fd = watchOpens(t, dir)
	require.NoError(t, Creatable(filepath.Join(dir, "a", "x.txt")))
	noOpens(t, fd)
}

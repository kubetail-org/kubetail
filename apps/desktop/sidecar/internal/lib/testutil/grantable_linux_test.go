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

package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exitingTB is fakeTB whose Fatal ends the goroutine, as *testing.T's does, so the
// helper stops at its first failure.
type exitingTB struct{ fakeTB }

func (f *exitingTB) Fatal(args ...any) {
	f.failed = fmt.Sprint(args...)
	runtime.Goexit()
}

// grantableFails runs GrantableDir on its own goroutine and returns what it failed with.
func grantableFails(t *testing.T) string {
	t.Helper()
	f := &exitingTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		GrantableDir(f)
	}()
	Wait(t, done, "GrantableDir to return")
	return f.failed
}

// GrantableDir fails its test, rather than falling back to /tmp, when the cache
// directory cannot hold it. Serial: each case sets the process environment.
func TestGrantableDirFailsWithoutACacheDir(t *testing.T) {
	t.Run("no cache directory", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "")
		assert.Contains(t, grantableFails(t), "$HOME")
	})

	t.Run("a cache path that is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(file, "cache"))
		assert.Contains(t, grantableFails(t), "not a directory")
	})

	t.Run("a cache directory it cannot write", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes a directory whatever its mode")
		}
		dir := filepath.Join(t.TempDir(), "cache")
		require.NoError(t, os.Mkdir(dir, 0o500))
		t.Setenv("XDG_CACHE_HOME", dir)
		assert.Contains(t, grantableFails(t), "permission denied")
	})
}

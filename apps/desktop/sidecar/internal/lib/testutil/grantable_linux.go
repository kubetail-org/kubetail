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
	"os"
	"path/filepath"
	"testing"
)

// GrantableDir is a fresh directory a folder grant can name and a sandboxed
// run can see, its path resolved, removed when t ends. On Linux it is under
// the user's cache directory, since t.TempDir is under /tmp, which every
// sandboxed run mounts its own of and no grant may name.
func GrantableDir(t testing.TB) string {
	t.Helper()
	parent, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(parent, "kubetail-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil { // coverage:ignore the directory was just made, so its path resolves
		t.Fatal(err)
	}
	return dir
}

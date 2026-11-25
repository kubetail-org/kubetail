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

//go:build !linux

package testutil

import (
	"path/filepath"
	"testing"
)

// GrantableDir is a fresh directory a folder grant can name and a sandboxed
// run can see, its path resolved, removed when t ends.
func GrantableDir(t testing.TB) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil { // coverage:ignore t.TempDir was just made, so its path resolves
		t.Fatal(err)
	}
	return dir
}

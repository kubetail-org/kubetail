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

package atomicjson_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/atomicjson"
)

// host.json holds the user's settings, so Save writes the file 0600 and any directory
// it has to create 0700 — whatever umask the process was started with. Windows has no
// POSIX permission bits; the per-user profile ACL carries it there.
func TestSaveIsOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeper")
	p := filepath.Join(dir, "x.json")
	if err := atomicjson.Save(p, doc{A: "v"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 600", perm)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %o, want 700", perm)
	}
}

// The temp file lands beside the target, so a directory the process cannot
// write to fails the Save rather than falling back somewhere writable.
func TestSaveFailsWhenTheDirectoryIsNotWritable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := atomicjson.Save(filepath.Join(dir, "doc.json"), doc{A: "x"}); err == nil {
		t.Fatal("Save into a read-only directory returned no error")
	}
}

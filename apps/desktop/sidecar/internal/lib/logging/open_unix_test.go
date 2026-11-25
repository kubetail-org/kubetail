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

package logging

import (
	"os"
	"path/filepath"
	"testing"
)

// The log holds cluster names and server hostnames. Windows has no mode bits: there the
// inherited profile ACL is the whole protection.
func TestOpenCreatesAnOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(logDir(t), "sidecar.log")

	if _, _, err := open(t, path, false); err != nil {
		t.Fatalf("Open: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %#o, want 0600", got)
	}
}

// A directory other users can reach is not the log's gate, so the log stays on stderr and
// no file is made there.
func TestOpenRefusesADirectoryOthersCanReach(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sidecar.log")

	if _, _, err := open(t, path, false); err == nil {
		t.Fatal("Open = nil error, want one")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Open made the file: %v", err)
	}
}

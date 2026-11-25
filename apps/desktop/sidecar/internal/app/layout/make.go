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

package layout

import (
	"os"
	"runtime"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// MakeOwnerOnly makes dir 0700 and tightens one an earlier build left open.
// When dir is in a Kubetail folder holding it — l's app folder, or the state
// folder Linux keeps the logs in — it first makes that folder owner-only
// unless it already is: whoever can write a folder can replace what is in it.
// On Windows the profile's ACL is the gate, so it only makes the folders: a
// chmod there sets the read-only attribute, which needs a right the ACL may
// not grant.
func MakeOwnerOnly(l Layout, dir string) error {
	if holder := holderOf(l, dir); holder != "" && !isOwnerOnlyDir(holder) {
		if err := ownerOnly(holder); err != nil {
			return err
		}
	}
	return ownerOnly(dir)
}

// holderOf is the Kubetail folder dir sits in, or empty when there is none. l is
// resolved, so names compare as text (Within).
func holderOf(l Layout, dir string) string {
	for _, holder := range []string{l.App, l.State} {
		if holder != "" && holder != dir && realpath.Within(dir, holder) {
			return holder
		}
	}
	return ""
}

func ownerOnly(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil || runtime.GOOS == "windows" {
		return err
	}
	// MkdirAll's mode applies only to the folders it makes.
	return os.Chmod(dir, 0o700)
}

// isOwnerOnlyDir reports a directory whose permission bits are 0700. Windows
// reports every writable directory as 0777, so there it is never one.
func isOwnerOnlyDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir() && info.Mode().Perm() == 0o700
}

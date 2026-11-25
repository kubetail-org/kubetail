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

package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// SearchFolder is the folder a run may search for the PATH entry dir — dir
// canonical — and the folder's info, or the reason it may not: the rules a
// run's own policy holds a folder to, so a folder that passes never stops a
// run from starting. never is the paths no rule opens, and broad (Broad) the
// folders whose Read rule would open too much, each compared as a file. It
// reads the disk.
func SearchFolder(dir string, never, broad []string) (folder string, info os.FileInfo, reason string) {
	if dir == "" {
		return "", nil, "empty"
	}
	// A relative entry, a leading ~ included, would resolve against the
	// working directory, which a run can write.
	if !filepath.IsAbs(dir) {
		return "", nil, "relative"
	}
	// Named as written, as the shell's lookup reads it: a link before a ..
	// leads to the parent of its target.
	folder, err := realpath.KernelPath(dir)
	if err != nil {
		return "", nil, "missing"
	}
	info, err = os.Stat(folder)
	switch {
	case err != nil || !info.IsDir():
		return "", nil, "missing"
	case worldWritable(info):
		// Anyone on the machine could put a program in it.
		return "", nil, "world"
	case realpath.UnderAny(folder, never):
		return "", nil, "never"
	case slices.ContainsFunc(broad, func(b string) bool { return realpath.Under(b, folder) }):
		// Its Read rule would open the whole home, every app's data or the
		// disk. A folder that big is a grant, not a PATH entry.
		return "", nil, "broad"
	case overFixedMount(folder):
		// Command refuses a rule there, and with it the run.
		return "", nil, "fixed"
	case strings.ContainsRune(folder, filepath.ListSeparator):
		// Joined into PATH it would split into other folders.
		return "", nil, "separator"
	}
	return folder, info, ""
}

// Broad is the folders no PATH entry may be or hold: /, and with a
// home, the home and the folders under it that hold every app's data.
func Broad(home string) []string {
	return broadDirs(home)
}

// WithSearch is f with a Read rule for each of folders, the folders a run's
// PATH searches. A Deny on one of those very folders is dropped, since it
// would win the tie: a closed folder the user included opens. No Always path
// is a Files rule, so this opens none. It reads the disk.
func (f FilePolicy) WithSearch(folders []string) FilePolicy {
	deny := slices.DeleteFunc(slices.Clone(f.Deny), func(d string) bool {
		return slices.ContainsFunc(folders, func(folder string) bool { return realpath.Same(folder, d) })
	})
	return FilePolicy{Read: slices.Concat(f.Read, folders), Write: f.Write, Deny: deny}
}

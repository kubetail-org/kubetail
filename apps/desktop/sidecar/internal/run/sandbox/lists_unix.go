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

package sandbox

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Never is the paths no run on this machine can read, whatever its policy
// grants: the Never lists with ~/ under home, /root, the other users' homes
// and, on Linux, the rootless container sockets. The homes and the sockets
// are read at call time, and listing a folder can hang on a network mount, so
// the caller bounds it. A path on or above home, compared canonical, is left
// out, so a home of /root does not deny itself.
func (s *Sandbox) Never(home string) []string {
	paths := slices.Concat(neverPaths(home), []string{"/root"}, otherHomes(home), runtimeNever())
	if home == "" {
		return paths
	}
	return notAbove(home, paths)
}

// notAbove is each of paths that is not on or above home, compared as files:
// a home the disk spells in another case is still home.
func notAbove(home string, paths []string) []string {
	return slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return realpath.Under(home, p) })
}

// NoWrite is the NoWrite lists with ~/ under home: what something outside
// the sandbox runs, which no folder grant may write.
func NoWrite(home string) []string {
	return inHome(home, slices.Concat(sharedLists.NoWrite, platformLists.NoWrite))
}

// neverPaths is the Never lists with ~/ under home.
func neverPaths(home string) []string {
	return inHome(home, slices.Concat(sharedLists.Never, platformLists.Never))
}

// inHome is paths with ~/ replaced by home. With no home it is the absolute
// paths alone: there is no home for a ~/ path to lie under.
func inHome(home string, paths []string) []string {
	var out []string
	for _, p := range paths {
		rel, under := strings.CutPrefix(p, "~/")
		switch {
		case !under:
			out = append(out, p)
		case home != "":
			out = append(out, filepath.Join(home, filepath.FromSlash(rel)))
		}
	}
	return out
}

// otherHomes is each entry of homesParent but notHomes and any on or above
// home (notAbove), so a home nested in an entry keeps that entry. A parent
// that cannot be listed adds nothing.
func otherHomes(home string) []string {
	entries, err := os.ReadDir(homesParent)
	if err != nil { // coverage:ignore homesParent, /home or /Users, exists on every system Kubetail supports
		return nil
	}
	var homes []string
	for _, e := range entries {
		if !slices.Contains(notHomes, e.Name()) {
			homes = append(homes, filepath.Join(homesParent, e.Name()))
		}
	}
	if home == "" {
		return homes
	}
	return notAbove(home, homes)
}

// System is what every sandboxed run on this machine starts from, from one
// look at home. It reads the System folders, each Toolchain folder that
// exists and is not broad (broadDirs), shell's folders (shellReads) and this
// executable at its resolved path, and denies Homebrew's var and the Closed folders. It holds no Read on or inside
// a Never path or another user's home, and none over a fixed mount. A
// location's Env is set when its first folder exists.
func (s *Sandbox) System(home, shell string) System {
	var sys System
	read := systemFolders()
	broad := broadDirs(home)
	if home != "" {
		readable := func(p string) bool {
			_, err := os.Stat(p)
			return err == nil && !isBroad(p, broad)
		}
		for _, l := range slices.Concat(sharedLists.Toolchain, platformLists.Toolchain) {
			folders := inHome(home, l.Read)
			read = append(read, slices.DeleteFunc(slices.Clone(folders), func(p string) bool { return !readable(p) })...)
			if !readable(folders[0]) {
				continue
			}
			for _, name := range slices.Sorted(maps.Keys(l.Env)) {
				sys.Env = append(sys.Env, name+"="+inHome(home, []string{l.Env[name]})[0])
			}
			sys.Asdf = sys.Asdf || l.Env["ASDF_DATA_DIR"] != ""
		}
	}
	for _, p := range shellReads(home, shell, broad) {
		if !slices.Contains(read, p) {
			read = append(read, p)
		}
	}
	read = slices.DeleteFunc(append(read, realpath.Resolved(s.self)), overFixedMount)
	deny := slices.Concat(brewVar, inHome(home, slices.Concat(sharedLists.Closed, platformLists.Closed)))
	sys.Files = FilePolicy{Read: read, Deny: deny}.Outside(slices.Concat(neverPaths(home), otherHomes(home))...)
	return sys
}

// broadDirs is what no toolchain or shell folder may be or hold: /, and with a
// home, the home and appDataDirs under it.
func broadDirs(home string) []string {
	if home == "" {
		return []string{"/"}
	}
	return append(inHome(home, appDataDirs), home, "/")
}

// isBroad reports whether p is or holds one of broad, compared as files.
func isBroad(p string, broad []string) bool {
	return slices.ContainsFunc(broad, func(b string) bool { return realpath.Under(b, p) })
}

// maxHops is how many links the kernel follows in one lookup.
const maxHops = 40

// shellReads is what System reads for shell and for each link on its way to
// the program, which the kernel reads to run it: the folder shellFolder
// names, or the program alone where that folder is broad.
func shellReads(home, shell string, broad []string) []string {
	var reads []string
	p := shell
	for range maxHops {
		if dir := shellFolder(home, p); isBroad(dir, broad) {
			reads = append(reads, p)
		} else {
			reads = append(reads, dir)
		}
		target, err := os.Readlink(p)
		if err != nil {
			break
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(p), target)
		}
		p = target
	}
	return reads
}

// shellFolder is the folder System reads for shell. A folder named bin
// outside the home reads its parent, so a shell under a prefix such as
// /opt/zsh reaches its own share, unless the parent is / or holds the home.
// Under the home the folder is read alone, since a parent there can hold
// every app's data.
func shellFolder(home, shell string) string {
	dir := filepath.Dir(shell)
	underHome := home != "" && realpath.Under(dir, home)
	parent := filepath.Dir(dir)
	holdsHome := home != "" && realpath.Under(home, parent)
	if filepath.Base(dir) == "bin" && !underHome && parent != "/" && !holdsHome {
		return parent
	}
	return dir
}

// systemFolders is the System folders every run on this platform reads.
func systemFolders() []string {
	return slices.Concat(sharedLists.System, platformLists.System)
}

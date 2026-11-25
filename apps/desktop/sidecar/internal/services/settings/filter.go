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

package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// pathDir is one folder filterDir kept.
type pathDir struct {
	Target string // the folder it resolved to
	Shared bool   // writable by a group other than an administrators' one

	info os.FileInfo // the folder the filter checked, which a duplicate is compared with
}

// zones is Zones as the checks read it, each path as the zones list it. A
// folder is compared with them as files (realpath.UnderAny, realpath.Same), never
// by text.
type zones struct {
	never, broad, read, deny, noWrite []string
	home                              string // "" for none
	links                             []realpath.Link
}

func zonesOf(z Zones) zones {
	return zones{never: z.Never, broad: sandbox.Broad(z.Home), read: z.Open.Read, deny: z.Open.Deny, noWrite: z.NoWrite, home: z.Home, links: z.Links}
}

// open reports whether target is under a Read path and no Deny path: every
// run reads it already.
func (z zones) open(target string) bool {
	return realpath.UnderAny(target, z.read) && !realpath.UnderAny(target, z.deny)
}

// filterDir is dir as a pathDir, or the rule that drops it: the sandbox's
// rules for a folder a run may search, then this list's own.
func filterDir(dir string, z zones) (pathDir, string) {
	target, info, reason := sandbox.SearchFolder(dir, z.never, z.broad)
	switch {
	case reason != "":
		return pathDir{}, reason
	case slices.Contains(strings.Split(target, string(filepath.Separator)), "node_modules"):
		// A project's folder holds whatever its last install fetched.
		return pathDir{}, "project"
	}
	return pathDir{Target: target, Shared: shared(info), info: info}, ""
}

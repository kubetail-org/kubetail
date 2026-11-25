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
	"context"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// Hidden is what the sandbox keeps shut under any grant, each path as the
// sandbox lists it: Never, the denied-always list with Kubetail's directories,
// and Closed, the closed folders a grant inside one opens. A file tool skips
// no request for a path in it; it is never made, and a path in it that does
// not exist matches nothing. It is resolved each time it is compared, so a
// link on the list that is retargeted is caught at its target now.
type Hidden struct {
	Never, Closed []string
}

// resolvePaths is realpath.ResolvedAll, or a test's stand-in.
var resolvePaths = realpath.ResolvedAll

// resolved is h with every path's links followed. It reads the disk.
func (h Hidden) resolved() Hidden {
	return Hidden{Never: resolvePaths(h.Never), Closed: resolvePaths(h.Closed)}
}

// WithHidden is f knowing what the sandbox keeps shut: hidden answers it, read
// from a snapshot, so asking touches no disk.
func (f Fence) WithHidden(hidden func() (never, closed []string)) Fence {
	f.hidden = hidden
	return f
}

// Hidden is what f knows the sandbox keeps shut, and false for a fence that
// knows nothing, which skips no request.
func (f Fence) Hidden() (Hidden, bool) {
	if f.hidden == nil {
		return Hidden{}, false
	}
	never, closed := f.hidden()
	return Hidden{Never: never, Closed: closed}, true
}

// Granted is the folder path is under, and false for a path under none or one
// the sandbox keeps shut there. Paths are compared as files (realpath.Under),
// read now, each through its deepest folder that exists. The deepest folder
// covering path answers, a read-write one first where one covers it, since the
// sandbox's deepest rule decides and a read-write grant covers what lies under
// it. Resolving reads the disk, which a dead network mount can hold in a
// syscall no context reaches, so it runs on a goroutine abandoned when ctx
// ends first, and the call then answers false: the tool asks.
func (f Fence) Granted(ctx context.Context, folders []session.Folder, path string) (session.Folder, bool) {
	h, ok := f.Hidden()
	if !ok || len(folders) == 0 {
		return session.Folder{}, false
	}
	type match struct {
		folder session.Folder
		ok     bool
	}
	matched := make(chan match, 1)
	go func() {
		folder, ok := h.resolved().granted(folders, resolvePaths([]string{path})[0])
		matched <- match{folder, ok}
	}()
	select {
	case m := <-matched:
		return m.folder, m.ok
	case <-ctx.Done():
		return session.Folder{}, false
	}
}

// granted is Granted over resolved paths.
func (h Hidden) granted(folders []session.Folder, path string) (session.Folder, bool) {
	var best session.Folder
	found := false
	for _, g := range folders {
		if !realpath.Under(path, g.Path) {
			continue
		}
		if !found || g.Write && !best.Write || g.Write == best.Write && len(g.Path) > len(best.Path) {
			best, found = g, true
		}
	}
	if !found || h.hides(best, path) {
		return session.Folder{}, false
	}
	return best, true
}

// hides reports whether p, resolved, is shut under grant: under a Never path,
// or under a Closed folder that lies inside the grant. One over the grant is
// the folder the grant opens.
func (h Hidden) hides(grant session.Folder, p string) bool {
	if realpath.UnderAny(p, h.Never) {
		return true
	}
	return slices.ContainsFunc(h.Closed, func(c string) bool {
		return !realpath.Same(c, grant.Path) && realpath.Under(c, grant.Path) && realpath.Under(p, c)
	})
}

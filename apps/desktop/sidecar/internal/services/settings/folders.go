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
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// FolderRefusal is why a folder cannot be granted, in the user's words. Rule
// names the check that refused it; for "link", Target is where it leads.
type FolderRefusal struct {
	Rule   string
	Reason string
	Target string
}

func (r FolderRefusal) Error() string { return r.Reason }

// CheckFolder is nil when path can be granted, read, or with write read and
// write, else a FolderRefusal. It reads the disk: path is made canonical, and
// compared as a file with z and pathNoWrite, the PATH folders no write grant
// may reach. A link on or above path, or a spelling the disk is known not to
// use, is refused with the canonical path, so the folder the user reads is
// the folder the sandbox checks.
func CheckFolder(path string, write bool, z Zones, pathNoWrite []string) error {
	return checkFolder(path, write, zonesOf(z), pathNoWrite, false)
}

// CheckStoredFolder is CheckFolder for a grant already stored: one that
// resolves elsewhere than it did has moved, or had a link put in its place,
// since it was granted.
func CheckStoredFolder(path string, write bool, z Zones, pathNoWrite []string) error {
	return checkFolder(path, write, zonesOf(z), pathNoWrite, true)
}

func checkFolder(path string, write bool, z zones, pathNoWrite []string, stored bool) error {
	refuse := func(rule, reason string) error { return FolderRefusal{Rule: rule, Reason: reason} }
	switch {
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		return refuse("absolute", "Name the folder by its full path.")
	case path == string(filepath.Separator):
		return refuse("root", "The root cannot be granted.")
	case sandbox.FixedMount(path):
		return refuse("fixed", "Every sandboxed command has its own "+fixedMountOf(path)+"; it cannot be granted.")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return refuse("exists", "This folder does not exist.")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil { // coverage:ignore the Stat just found the folder; only a removal in between fails this
		return refuse("exists", "This folder does not exist.")
	}
	canon, err := realpath.Canonical([]string{resolved})
	if err != nil { // coverage:ignore the folder was just followed; only a removal in between fails this
		// A run's check refuses a rule it cannot name, and the run with it.
		return refuse("unreadable", "Kubetail cannot read how the disk spells this folder, so it cannot be granted.")
	}
	spelled := canon[0]
	switch {
	case spelled != path && stored:
		return refuse("moved", "This folder has moved or become a link. Grant it again.")
	case resolved != path:
		return FolderRefusal{Rule: "link", Reason: path + " is a link to " + spelled + "; grant " + spelled + " instead.", Target: spelled}
	case spelled != path:
		// A case-insensitive filesystem opens either spelling, and the
		// sandbox's profile matches the one the volume stores.
		return FolderRefusal{Rule: "spelling", Reason: path + " is spelled " + spelled + " on disk; grant " + spelled + " instead.", Target: spelled}
	case realpath.UnderAny(path, z.never):
		return refuse("never", "This folder is never readable in the sandbox.")
	case slices.ContainsFunc(z.deny, func(d string) bool { return realpath.Same(d, path) }):
		// A closed folder's Deny is not outranked by a Read on the same path.
		return refuse("closed", "Grant a folder inside it.")
	case !write:
		return nil
	case z.home != "" && realpath.Same(path, z.home):
		// A command that can write a folder can rename what is under it.
		return refuse("home", "Your home can be granted read-only.")
	}
	for _, l := range z.links {
		if realpath.Under(l.Parent, path) {
			return refuse("link-parent", "Kubetail follows the link "+l.Name+", and a command that can write "+path+" could repoint it. Grant it read-only.")
		}
	}
	if p, ok := overlapping(path, slices.Concat(z.read, absolute(pathNoWrite))); ok {
		return refuse("tool", "Kubetail reads tools from "+p+"; grant it read-only, or a folder beside it.")
	}
	if p, ok := inside(path, z.deny); ok {
		return refuse("over-closed", p+" is closed in the sandbox; grant a folder inside it.")
	}
	if p, ok := overlapping(path, z.noWrite); ok {
		return refuse("code", "Something outside the sandbox runs what is in "+p+"; grant it read-only.")
	}
	if p, ok := inside(path, z.never); ok {
		return refuse("over-never", p+" is never readable in the sandbox, so this folder can be granted read-only.")
	}
	return nil
}

// overlapping is the first of dirs that path is on, under or over, as files.
func overlapping(path string, dirs []string) (string, bool) {
	for _, d := range dirs {
		if realpath.Under(path, d) || realpath.Under(d, path) {
			return d, true
		}
	}
	return "", false
}

// inside is the first of dirs that lies under path, as files, path itself
// left out.
func inside(path string, dirs []string) (string, bool) {
	for _, d := range dirs {
		if !realpath.Same(d, path) && realpath.Under(d, path) {
			return d, true
		}
	}
	return "", false
}

// fixedMountOf is the mount path is on or under: its outermost folder that is
// still a fixed mount.
func fixedMountOf(path string) string {
	for {
		parent := filepath.Dir(path)
		if parent == path || !sandbox.FixedMount(parent) {
			return path
		}
		path = parent
	}
}

// absolute is dirs less each that names no folder: a relative one, or one
// holding a NUL.
func absolute(dirs []string) []string {
	return slices.DeleteFunc(slices.Clone(dirs), func(p string) bool { return !filepath.IsAbs(p) || strings.ContainsRune(p, 0) })
}

// snapshot is what the folder checks read: the zones as the zones function
// listed them, and the home canonical. Every check resolves the zones again,
// so a link on a list that is retargeted after the snapshot is caught at its
// target now; the snapshot saves the zones function's own reads of the disk
// alone.
type snapshot struct {
	zones Zones
	home  string // canonical, or as given when the kernel cannot name it; "" for none
}

// takeSnapshot reads the disk.
func takeSnapshot(z Zones) *snapshot {
	snap := &snapshot{zones: z}
	if z.Home != "" {
		home, _ := realpath.Canonical([]string{z.Home})
		snap.home = home[0]
	}
	return snap
}

// readWithin runs read, which reads the disk, and answers it, or false when
// ctx or bound ends first: a stat on a dead network mount blocks in a syscall
// no context reaches, so read runs on a goroutine left to return whenever the
// filesystem does, and what it answers then is dropped.
func readWithin[T any](ctx context.Context, bound time.Duration, read func() T) (T, bool) {
	done := make(chan T, 1)
	go func() { done <- read() }()
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	select {
	case v := <-done:
		return v, true
	case <-ctx.Done():
		var none T
		return none, false
	}
}

// currentSnapshot is the snapshot, taken on its first use when no read of the
// shell has taken one: zones reads the disk, so it is bounded. Nil when ctx
// or the bound ends first, or on a machine with no sandbox.
func (s *Service) currentSnapshot(ctx context.Context) *snapshot {
	if s.zones == nil {
		return nil
	}
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	if s.snap == nil {
		s.snap, _ = readWithin(ctx, s.diskTimeout, func() *snapshot { return takeSnapshot(s.zones()) })
	}
	return s.snap
}

var (
	noSandboxRefusal = FolderRefusal{Rule: "no-sandbox", Reason: "This machine has no sandbox."}
	timeoutRefusal   = FolderRefusal{Rule: "timeout", Reason: "Kubetail could not check this folder in time."}
)

// CheckFolder is CheckFolder against the snapshot's zones, resolved now, and
// the PATH's NoWrite folders (PathNoWrite). Its disk reads are bounded by
// diskTimeout. On a machine with no sandbox every folder is
// refused. A refusal is a FolderRefusal; any other error is a read that
// failed.
func (s *Service) CheckFolder(ctx context.Context, path string, write bool) error {
	return s.checkWithin(ctx, path, write, false)
}

// CheckStoredFolder is CheckStoredFolder, bounded as CheckFolder is.
func (s *Service) CheckStoredFolder(ctx context.Context, path string, write bool) error {
	return s.checkWithin(ctx, path, write, true)
}

func (s *Service) checkWithin(ctx context.Context, path string, write, stored bool) error {
	if s.zones == nil {
		return noSandboxRefusal
	}
	snap := s.currentSnapshot(ctx)
	if snap == nil {
		return timeoutRefusal
	}
	// Only a write grant is compared with the PATH. A read that fails is an
	// error, not a refusal: whoever asked decides what it costs.
	var noWrite []string
	if write {
		var err error
		if noWrite, err = s.PathNoWrite(ctx); err != nil {
			return err
		}
	}
	err, ok := readWithin(ctx, s.diskTimeout, func() error {
		return s.checkFolder(path, write, zonesOf(snap.zones), noWrite, stored)
	})
	if !ok {
		return timeoutRefusal
	}
	return err
}

// Hidden is what the sandbox keeps shut under any grant, as the zones list it:
// never, the denied-always list with Kubetail's directories, and closed, the
// closed folders. It reads the snapshot and never takes one: with none, no
// folder passed a check either. It touches no disk; the file tools resolve the
// paths when they compare them.
func (s *Service) Hidden() (never, closed []string) {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	if s.snap == nil {
		return nil, nil
	}
	return s.snap.zones.Never, s.snap.zones.Open.Deny
}

// NeverReadable is the denied-always list with Kubetail's directories, as
// Settings draws it; nil on a machine with no sandbox.
func (s *Service) NeverReadable(ctx context.Context) []string {
	snap := s.currentSnapshot(ctx)
	if snap == nil {
		return nil
	}
	return snap.zones.Never
}

// ClosedFolders is the closed folders, as Settings draws them: none can be
// granted, a folder inside one can. Nil on a machine with no sandbox.
func (s *Service) ClosedFolders(ctx context.Context) []string {
	snap := s.currentSnapshot(ctx)
	if snap == nil {
		return nil
	}
	return snap.zones.Open.Deny
}

// WideFolders is the folders a read grant of which reads more than a project:
// the home and each folder over it, canonical, and the platform's own
// (wideDirs). Nil on a machine with no sandbox, and when the zones' snapshot
// runs past the bound.
func (s *Service) WideFolders(ctx context.Context) []string {
	snap := s.currentSnapshot(ctx)
	if snap == nil || snap.home == "" {
		return nil
	}
	var wide []string
	for dir := snap.home; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		wide = append(wide, dir)
	}
	return append(wide, wideDirs...)
}

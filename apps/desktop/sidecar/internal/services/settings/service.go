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
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// Zones is what a PATH folder and a folder grant are judged by: Never, the
// paths no rule opens; Open, what every run reads, whose Read paths are the
// open folders and whose Deny paths close what lies under them again; Home,
// which with its app-data folders no PATH folder may be or hold; NoWrite,
// what something outside the sandbox runs, which no grant may write; and
// Links, the links Kubetail followed to its folders, whose parents no grant may
// write.
type Zones struct {
	Never   []string
	Open    sandbox.FilePolicy
	Home    string
	NoWrite []string
	Links   []realpath.Link
}

// Service is the store, and the login shell's PATH and the zones the PATH
// setting is judged by.
type Service struct {
	*Store
	// zones and resolve are nil on a machine with no sandbox.
	zones   func() Zones
	resolve func(context.Context) ([]string, error)

	// pathMu guards fault, why the last read of the login shell failed, ""
	// once one answers.
	pathMu sync.Mutex
	fault  string
	// diskTimeout is DiskTimeout, or a test's shorter one.
	diskTimeout time.Duration

	// snapMu guards snap, the zones as the folder checks read them.
	snapMu sync.Mutex
	snap   *snapshot
	// checkFolder is the package's checkFolder, or a test's stand-in.
	checkFolder func(path string, write bool, z zones, pathNoWrite []string, stored bool) error
	// writing holds one write of the shell's PATH at a time.
	writing chan struct{}
	// defaults is what $PATH expands to before a read of the shell ever
	// answered: the platform's default folders.
	defaults []string
}

// DiskTimeout bounds each reading of the disk for the zones or a check, as the
// login shell's run is bounded, so a stat on a dead network mount cannot hold
// a launch, a refresh or a check.
const DiskTimeout = 5 * time.Second

// NewService is the store with the login shell's PATH. zones is read at each
// read of the shell; resolve runs the login shell for a refresh; fault is why
// the launch's read failed, "" when it answered. defaults is what $PATH
// expands to before a read of the shell ever answered.
func NewService(store *Store, zones func() Zones, resolve func(context.Context) ([]string, error), defaults []string, fault string) *Service {
	return &Service{Store: store, zones: zones, resolve: resolve, defaults: defaults, fault: fault, diskTimeout: DiskTimeout, checkFolder: checkFolder, writing: make(chan struct{}, 1)}
}

// ErrNoSandbox is a change to the PATH on a machine with no sandbox.
const ErrNoSandbox PathRefusal = "This machine has no sandbox."

// trackedTools are the programs whose first open folder a write of the
// shell's PATH logs a move of, since a startup file that puts a folder first
// chooses which runs.
var trackedTools = []string{"kubectl", "helm"}

// SetShellPath stores dirs, a read of the login shell's PATH, after retaking
// the zones' snapshot, and answers whether it changed the row. Every call
// pings the PATH's watchers, since new zones can change a folder grant's
// answer. The zones are read on a goroutine abandoned when ctx ends or
// diskTimeout passes, since a stat on a dead network mount does not return;
// one that ends first writes nothing. Writes run one at a time, so an older
// read never lands after a newer one.
func (s *Service) SetShellPath(ctx context.Context, dirs []string) (changed bool, err error) {
	if s.zones == nil {
		return false, ErrNoSandbox
	}
	ctx, cancel := context.WithTimeout(ctx, s.diskTimeout)
	defer cancel()
	select {
	case s.writing <- struct{}{}:
		defer func() { <-s.writing }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	snap, ok := readWithin(ctx, s.diskTimeout, func() *snapshot { return takeSnapshot(s.zones()) })
	if !ok {
		return false, ctx.Err()
	}
	before, changed, err := s.writeShellPath(ctx, dirs)
	if err != nil {
		return false, err
	}
	s.snapMu.Lock()
	s.snap = snap
	s.snapMu.Unlock()
	s.db.Notify(appdb.KeySandboxPath)
	if changed {
		// A diagnostic: its stats run after the write, so one that hangs on
		// a dead mount never holds it.
		go logToolMoves(before, dirs, zonesOf(snap.zones).open, statTool)
	}
	return changed, nil
}

// SetPath saves the PATH setting, once the grammar passes it. A machine with
// no sandbox has no run to give it to.
func (s *Service) SetPath(ctx context.Context, value string) error {
	if s.zones == nil {
		return ErrNoSandbox
	}
	return s.Store.SetPath(ctx, value)
}

// RefreshPath is Refresh PATH: the login shell run again, then its PATH
// stored. A read that fails, or answers no PATH, writes nothing, keeps the
// last PATH and answers its reason.
func (s *Service) RefreshPath(ctx context.Context) error {
	if s.resolve == nil {
		return ErrNoSandbox
	}
	path, err := s.resolve(ctx)
	if err == nil && len(path) == 0 {
		err = errors.New("its PATH is empty")
	}
	if err != nil {
		s.setFault(err.Error())
		return PathRefusal("Your shell did not answer: " + err.Error() + ".")
	}
	// The fault stands until the PATH the shell answered is stored.
	if _, err := s.SetShellPath(ctx, path); err != nil {
		return err
	}
	s.setFault("")
	return nil
}

func (s *Service) setFault(fault string) {
	s.pathMu.Lock()
	defer s.pathMu.Unlock()
	s.fault = fault
}

// RunPath is the PATH a run reads once, at its start: Search, the folders it
// searches in order, each still to pass the run's check, and NoWrite, the
// folders no read-write grant may reach.
type RunPath struct {
	Search  []PathFolder
	NoWrite []string
}

// RunPath is the setting expanded over the shell's last PATH, and the NoWrite
// folders, from one read. A refused setting is an error, so no run starts on
// it.
func (s *Service) RunPath(ctx context.Context) (RunPath, error) {
	if s.zones == nil {
		return RunPath{}, ErrNoSandbox
	}
	p, err := s.expandPath(ctx)
	if err != nil {
		return RunPath{}, err
	}
	if p.setting.Refusal != "" {
		return RunPath{}, PathRefusal("Your PATH setting cannot be used: " + p.setting.Refusal + " Fix it in Settings.")
	}
	return RunPath{Search: p.search, NoWrite: p.noWrite()}, nil
}

// PathFolders is the setting and each of its folders as a run would judge
// them now, for Settings. A refused setting, or a machine with no sandbox,
// lists no folder. Its disk reads are bounded by diskTimeout.
func (s *Service) PathFolders(ctx context.Context) (PathSetting, []CheckedFolder, error) {
	if s.zones == nil {
		setting, _, _, err := s.readPath(ctx)
		return setting, nil, err
	}
	// A disk that does not answer still leaves the setting to show and edit:
	// with no folder when the zones run past the bound, each folder
	// unjudged when the checks do.
	p, err := s.expandPath(ctx)
	if err == timeoutRefusal {
		return p.setting, nil, nil
	}
	if err != nil || p.setting.Refusal != "" {
		return p.setting, nil, err
	}
	check := NewRunCheck(p.snap.zones.Open, p.snap.zones.Never, p.snap.zones.Home)
	check.checkFolder = s.checkFolder
	checked, ok := readWithin(ctx, s.diskTimeout, func() []CheckedFolder { return check.Folders(p.search) })
	if !ok {
		checked = make([]CheckedFolder, len(p.search))
		for i, f := range p.search {
			checked[i] = CheckedFolder{PathFolder: f, Reason: timeoutRefusal.Reason}
		}
	}
	return p.setting, checked, nil
}

// PathNoWrite is the folders no read-write grant may reach: every folder of
// the shell's PATH, which the user's terminal runs from whatever the setting
// says, and the typed folders of a setting Kubetail can use.
func (s *Service) PathNoWrite(ctx context.Context) ([]string, error) {
	p, err := s.expandPath(ctx)
	if err != nil {
		return nil, err
	}
	return p.noWrite(), nil
}

// expandedPath is the setting, the shell's last PATH, and the setting's
// folders over it, as one read found them.
type expandedPath struct {
	setting PathSetting
	shell   []string
	// search is nil while the setting is refused.
	search []PathFolder
	// snap is the snapshot whose home ~ and $HOME expanded to; nil while the
	// setting is refused, or when the zones run past the bound.
	snap *snapshot
}

// expandPath reads the setting and the shell's last PATH, or the default
// folders before a read of the shell ever answered, and expands a setting
// Kubetail can use.
func (s *Service) expandPath(ctx context.Context) (expandedPath, error) {
	setting, shell, found, err := s.readPath(ctx)
	if err != nil {
		return expandedPath{}, err
	}
	if !found {
		shell = s.defaults
	}
	p := expandedPath{setting: setting, shell: shell}
	if setting.Refusal != "" {
		return p, nil
	}
	if p.snap = s.currentSnapshot(ctx); p.snap == nil {
		return p, timeoutRefusal
	}
	// The setting passed its grammar, so it expands.
	p.search, _ = ExpandPath(setting.Value, shell, p.snap.home)
	return p, nil
}

// noWrite is every folder of the shell's PATH, then each typed folder.
func (p expandedPath) noWrite() []string {
	out := slices.Clone(p.shell)
	for _, f := range p.search {
		if f.Origin == OriginTyped {
			out = append(out, f.Path)
		}
	}
	return out
}

// PathFault is why the last read of the login shell failed, at launch or on
// refresh, "" once one answers.
func (s *Service) PathFault() string {
	s.pathMu.Lock()
	defer s.pathMu.Unlock()
	return s.fault
}

// statTool is os.Stat, or a test's stand-in.
var statTool = os.Stat

// logToolMoves logs each tracked tool whose first folder, among those open
// reports every run reads, differs between before and after, probing with
// stat. A folder no run reads never runs a tool unasked.
func logToolMoves(before, after []string, open func(string) bool, stat func(string) (os.FileInfo, error)) {
	holds := map[string]bool{}
	first := func(dirs []string, tool string) string {
		for _, dir := range dirs {
			if !filepath.IsAbs(dir) || !open(dir) {
				continue
			}
			key := filepath.Join(dir, tool)
			found, seen := holds[key]
			if !seen {
				info, err := stat(key)
				found = err == nil && !info.IsDir()
				holds[key] = found
			}
			if found {
				return dir
			}
		}
		return ""
	}
	for _, tool := range trackedTools {
		if from, to := first(before, tool), first(after, tool); from != to {
			slog.Info("PATH moved a tool", "tool", tool, "from", from, "to", to)
		}
	}
}

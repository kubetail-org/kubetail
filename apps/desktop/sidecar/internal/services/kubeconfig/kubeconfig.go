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

// Package kubeconfig watches the user's kubeconfig and publishes each reload. A
// leaf: it speaks native kubeconfig vocabulary (*api.Config) and knows nothing
// about cluster records.
package kubeconfig

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"
	"github.com/fsnotify/fsnotify"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// ConfigStateWatch is current-on-subscribe and delivers each reload after that.
// Cancel it through its own Close.
type ConfigStateWatch = *watch.Receiver[*api.Config]

// defaultPollInterval is the backstop, not the latency budget: notifications carry
// the ordinary case, so this only has to cover what they cannot see — a directory
// that became watchable after startup, a dropped event, a notifier that could not be
// created at all. Half an hour of staleness in those cases costs nothing; a tighter
// tick would parse the file all day for nobody.
const defaultPollInterval = 30 * time.Minute

// defaultSettle is the quiet period an edit goes through before it is read. Sized
// well above the gap a writer leaves between truncating a file and filling it
// (microseconds) and well below what a person notices.
const defaultSettle = 50 * time.Millisecond

// Service reloads the kubeconfig and publishes it only when it changed, so an
// unchanged file writes nothing downstream.
//
// Polling is what makes it correct; notifications only make it prompt. Filesystem
// events cannot be relied on for this file — an editor saves by replacing the inode,
// the notifier may not be available at all — so the tick is the floor under every one
// of those, and losing notifications costs latency alone. Subscribers cannot tell
// which source woke a reload, which keeps the whole arrangement inside this file.
//
// A resume is the third input. Timers run on the monotonic clock, which does not
// advance while the host is suspended, so waking can leave the tick most of a period
// still to run — and the events it would have covered are exactly the ones a
// suspended process is most likely to have dropped.
//
// The writers differ, and both shapes matter here: an editor replaces the inode,
// which is why the watch is on the directory; clientcmd (so `kubectl config
// use-context`) rewrites IN PLACE, truncating first, which is why an event settles
// before it is acted on.
type Service struct {
	// explicitPath is the --kubeconfig argument; empty means clientcmd's chain.
	explicitPath string
	// loadingRules is built at Start, not New: it reads KUBECONFIG, which on
	// macOS main sets from the login shell before run.
	loadingRules *clientcmd.ClientConfigLoadingRules
	// interval paces the reload; tests shrink it before Start rather than outwait it.
	interval time.Duration
	// settle is how long events must go quiet before a reload. It exists because a
	// truncated file is not a parse error — a zero-length kubeconfig loads as a valid
	// config with no contexts, and publishing that reads downstream as "every cluster
	// is gone". Waiting out the writer's own gap is what keeps that off the wire.
	settle time.Duration
	// pokeSvc wakes the loop on a resume. Nil is ordinary — the poll covers what it
	// would have caught, a period later.
	pokeSvc *poke.Service
	// newFSWatcher builds the notification layer; a build seam, since the case worth
	// testing is the kernel refusing one, which a test cannot ask for.
	newFSWatcher func() (*fsnotify.Watcher, error)
	// load reads the chain; a seam, since a test has to change a file while the
	// load is under way.
	load func(*clientcmd.ClientConfigLoadingRules) (*api.Config, error)

	hub *watch.Hub[*api.Config]

	mu      sync.RWMutex
	current *api.Config
	// read records that a poll has been attempted. It separates "there is no
	// kubeconfig here" from "the service has not gotten to it yet" — both are the
	// empty config, so the value alone cannot say which. Set even when the load
	// failed: an unreadable kubeconfig is an answer, and the last good config (here,
	// the empty seed) is what stands.
	read bool

	wg sync.WaitGroup
}

// New returns a service for kubeconfigPath, or the standard precedence chain when
// it is empty. Nothing is read, not even the environment, until Start: a
// subscriber attaching before then sees an empty config rather than a nil one.
// pokeSvc may be nil.
func New(kubeconfigPath string, pokeSvc *poke.Service) *Service {
	current := &api.Config{}
	return &Service{
		explicitPath: kubeconfigPath,
		interval:     defaultPollInterval,
		settle:       defaultSettle,
		pokeSvc:      pokeSvc,
		newFSWatcher: fsnotify.NewWatcher,
		load:         (*clientcmd.ClientConfigLoadingRules).Load,
		hub:          watch.New(current),
		current:      current,
	}
}

// Get returns the most recently loaded config — never nil — and whether the service
// has read yet. Before the first read the config is the empty seed, which a caller
// must not observe: it would report every context absent.
func (s *Service) Get() (*api.Config, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current, s.read
}

// WatchConfigState returns a handle carrying the current config, then each reload.
func (s *Service) WatchConfigState() ConfigStateWatch {
	return s.hub.Receiver()
}

// Start reads KUBECONFIG, reloads once, then launches the poll loop and returns
// the func that ends it. The first reload is synchronous so that whoever
// subscribes after Start sees real state: on the empty seed a consumer would
// reconcile a config known to hold nothing. Nothing here can fail.
func (s *Service) Start(context.Context) (func(context.Context) error, error) {
	s.loadingRules = s.newLoadingRules()

	// Watches before the first read, so the two overlap instead of leaving a gap: a
	// change after this is queued as an event, one before it lands in the read below,
	// and one in between arrives twice — which publishes once, since the second
	// reload finds nothing changed. Reading first would drop anything written in
	// that window until the backstop tick, half an hour later.
	paths := s.resolvePaths()
	fsw := s.newNotifier(paths)

	// Subscribed before the first read for the same reason the watch is: a poke in the
	// gap would otherwise wake nothing.
	var pokes <-chan poke.Notification
	cancelPokes := func() {}
	if s.pokeSvc != nil {
		pokes, cancelPokes = s.pokeSvc.ListenForNotifications()
	}

	s.poll()

	stop := make(chan struct{})
	s.wg.Go(func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		defer cancelPokes()
		if fsw != nil {
			defer fsw.Close()
		}

		// events and errs stay nil when there is no notification layer, and a receive
		// on a nil channel blocks forever — so the loop degrades to the ticker alone
		// with no branch of its own.
		var events chan fsnotify.Event
		var errs chan error
		if fsw != nil {
			events, errs = fsw.Events, fsw.Errors
		}

		// Every trigger arms this, and only its expiry reads: a save arrives as several
		// events and the first can find the file still truncated, which parses as a
		// config with no contexts at all — and a tick or a poke can land in that same
		// gap. Waiting for quiet is what keeps a rewrite in progress from publishing as
		// an emptied kubeconfig.
		settle := time.NewTimer(s.settle)
		settle.Stop()
		defer settle.Stop()

		// Re-resolving before each reload is what follows a re-pointed symlink onto
		// its new directory. Only this goroutine touches paths.
		reload := func() {
			paths = s.resolvePaths()
			if fsw != nil {
				syncDirs(fsw, paths)
			}
			s.poll()
		}

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// A quiet period is nothing against the backstop cadence.
				settle.Reset(s.settle)
			case ev := <-events:
				if _, ok := paths[filepath.Clean(ev.Name)]; ok {
					settle.Reset(s.settle)
				}
			case <-settle.C:
				reload()
			case _, ok := <-pokes:
				if !ok {
					// The bus closed ahead of us; the ticker still covers everything.
					pokes = nil
					continue
				}
				settle.Reset(s.settle)
			case err := <-errs:
				slog.Debug("kubeconfig watch error", "err", err)
			}
		}
	})

	// Idempotent, as lifecycle.StartCloser requires: a retry or a double drain anywhere
	// above must not turn shutdown into a close of a closed channel.
	var once sync.Once
	return func(ctx context.Context) error {
		once.Do(func() { close(stop) })
		return drain.WithContext(ctx, s.wg.Wait)
	}, nil
}

// Close ends every outstanding ConfigStateWatch. Separate from the stop func, and after
// it: subscribers must keep reading until the poll loop is joined, or a reload in
// flight is lost rather than delivered.
func (s *Service) Close() error {
	s.hub.Close()
	return nil
}

// newLoadingRules reads KUBECONFIG, the service's one read of the environment,
// and fixes the precedence list for the life of the process.
func (s *Service) newLoadingRules() *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = s.explicitPath
	return rules
}

// newNotifier returns a notifier watching the directories behind paths, or nil when
// the kernel would not give us one at all.
//
// Kept even when nothing is watchable yet — a machine with no ~/.kube has no
// directory to watch until the user first runs kubectl, and each reload retries.
// Discarding it there would mean discovering the config on a tick and then staying
// blind to every edit after it.
//
// nil is an ordinary outcome, not a failure: the poll is what makes the service
// correct, so losing notifications costs latency and nothing else.
func (s *Service) newNotifier(paths watchedPaths) *fsnotify.Watcher {
	fsw, err := s.newFSWatcher()
	if err != nil {
		slog.Info("kubeconfig notifications unavailable, polling only", "err", err)
		return nil
	}
	syncDirs(fsw, paths)
	return fsw
}

// watchedPaths is every file the loop reacts to: each precedence path, plus the file
// it resolves to when it is a symlink.
type watchedPaths map[string]struct{}

// resolvePaths reads the precedence chain and follows any symlink in it. A
// dotfiles-managed kubeconfig is a link whose target lives elsewhere, and an edit
// there touches nothing in the link's own directory — so without the target the
// notification layer never sees the routine case (`kubectl config use-context`
// writes straight through the link).
//
// Recomputed per reload rather than once, so re-pointing the link is picked up: the
// replacement is itself an event on the link, which is already watched.
func (s *Service) resolvePaths() watchedPaths {
	paths := watchedPaths{}
	for _, pathname := range s.loadingRules.GetLoadingPrecedence() {
		clean := filepath.Clean(pathname)
		paths[clean] = struct{}{}
		if target, ok := linkTarget(clean); ok {
			paths[target] = struct{}{}
		}
	}
	return paths
}

// linkTarget names the file pathname points at, from the link's own contents. Reports
// false when pathname is not a symlink. A dangling link still resolves — a half-finished
// dotfiles checkout, a profile not yet materialized — because the target APPEARING is
// the event worth seeing.
//
// The link's contents rather than filepath.EvalSymlinks, which resolves every component:
// on macOS a temp path under /var comes back rooted at /private/var, putting the target
// in a different namespace from the precedence path beside it. Everything here is
// compared by string — syncDirs against the watch list, the loop against an event's name
// — so one path answering to two spellings goes quietly unmatched. The cost is that only
// the first hop of a link chain is followed.
func linkTarget(pathname string) (string, bool) {
	target, err := os.Readlink(pathname)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(pathname), target)
	}
	return filepath.Clean(target), true
}

// syncDirs makes fsw watch exactly the directories holding paths. Directories rather
// than the files: an editor saves by replacing the inode, and a file-level watch
// follows the discarded one.
//
// Runs on every reload, which is what picks up a directory absent at startup and
// follows a symlink re-pointed at another one. Dropping the departed directories
// matters as much as adding: one left behind wakes the loop on every write in it for
// the life of the process, and the kqueue backend holds an fd per file it contains.
//
// A directory that cannot be watched is logged at debug and skipped: at this cadence
// anything louder would repeat for as long as it stays missing, and the poll already
// covers it.
func syncDirs(fsw *fsnotify.Watcher, paths watchedPaths) {
	want := map[string]struct{}{}
	for pathname := range paths {
		want[filepath.Dir(pathname)] = struct{}{}
	}

	for _, dir := range fsw.WatchList() {
		if _, keep := want[dir]; !keep {
			// Errors on a watch the kernel already dropped along with its directory,
			// which is the outcome being asked for.
			_ = fsw.Remove(dir)
		}
	}
	for dir := range want {
		if err := fsw.Add(dir); err != nil {
			slog.Debug("kubeconfig dir unwatchable, polling only for it", "dir", dir, "err", err)
		}
	}
}

// poll reloads the kubeconfig and publishes it if it differs from the last snapshot.
// markRead records the read a failed load still made.
func (s *Service) markRead() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read = true
}

// fileStamp is what a stat says of one file in the precedence chain; the zero
// value is a file that is not there.
type fileStamp struct {
	size    int64
	modTime int64
}

// stamps stats every file in the precedence chain.
func (s *Service) stamps() []fileStamp {
	paths := s.loadingRules.GetLoadingPrecedence()
	out := make([]fileStamp, len(paths))
	for i, pathname := range paths {
		if fi, err := os.Stat(pathname); err == nil {
			out[i] = fileStamp{size: fi.Size(), modTime: fi.ModTime().UnixNano()}
		}
	}
	return out
}

// truncated reports whether any file is an existing zero-length one.
func truncated(stamps []fileStamp) bool {
	for _, st := range stamps {
		if st != (fileStamp{}) && st.size == 0 {
			return true
		}
	}
	return false
}

func (s *Service) poll() {
	before := s.stamps()

	// clientcmd returns an empty config rather than an error when no file is found,
	// which is the right reading: a machine with no kubeconfig tracks no clusters.
	cfg, err := s.load(s.loadingRules)
	if err != nil {
		// Debug, not warn: a hand-edited file is unparseable in the middle of an edit
		// as a matter of course, and the last good config carries on serving. But
		// silence would leave a permanently broken kubeconfig looking like a service
		// that simply stopped noticing.
		slog.Debug("kubeconfig load failed, keeping the last good config", "err", err)
		s.markRead()
		return
	}

	// A file that changed during the load may have been read mid-write: an in-place
	// rewrite truncates first, and a read in that window loads as a valid config with
	// no contexts. The change raises its own event, or the next tick sees it, so this
	// read is dropped and that one publishes.
	if !slices.Equal(before, s.stamps()) {
		s.markRead()
		return
	}

	// A zero-length kubeconfig is not a parse error: it loads as a valid config with no
	// contexts, and publishing that reads downstream as every cluster vanishing. The
	// settle timer keeps a notification out of the writer's truncate window; this keeps
	// a tick or a poke out of it too. An absent kubeconfig already leaves the last good
	// config standing, so an emptied one is treated the same way — and only when the
	// merge has no contexts at all, which leaves a placeholder empty file beside a real
	// kubeconfig publishing as usual.
	if truncated(before) && len(cfg.Contexts) == 0 {
		s.markRead()
		return
	}

	// Compared whole rather than by a projection: the contexts drive cluster discovery,
	// and the cluster and user entries behind them resolve credentials, so every
	// field reaches someone.
	s.mu.Lock()
	s.read = true
	unchanged := reflect.DeepEqual(s.current, cfg)
	if !unchanged {
		s.current = cfg
	}
	s.mu.Unlock()

	if unchanged {
		return
	}
	s.hub.Sender().Send(cfg)
}

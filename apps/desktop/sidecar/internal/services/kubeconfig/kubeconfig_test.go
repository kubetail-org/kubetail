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

package kubeconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// testInterval paces the poll loop in tests, shrunk from the production cadence so
// nothing here has to outwait defaultPollInterval.
const testInterval = 5 * time.Millisecond

// testSettle shrinks the quiet period an edit must go through before a reload.
const testSettle = 2 * time.Millisecond

// newTestService returns an unstarted service and the kubeconfig path it
// reads, inside a temp dir. The path need not exist: an absent kubeconfig is an
// ordinary state.
func newTestService(t *testing.T, interval time.Duration) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	return newServiceAt(t, path, interval), path
}

// newServiceAt is newTestService over a path the caller chose. Both cadences
// are shrunk here so no test has to outwait a production one.
func newServiceAt(t *testing.T, path string, interval time.Duration) *Service {
	t.Helper()
	w := New(path, nil)
	w.interval = interval
	w.settle = testSettle
	// For the tests that drive the loop's methods on an unstarted service; Start
	// builds its own.
	w.loadingRules = w.newLoadingRules()
	return w
}

// start runs the service, stopping and closing it on cleanup.
func start(t *testing.T, w *Service) {
	t.Helper()
	stop, err := w.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stop(context.Background()))
		assert.NoError(t, w.Close())
	})
}

// newStartedService returns a started service over a path inside a temp dir.
func newStartedService(t *testing.T) *Service {
	t.Helper()
	w, _ := newTestService(t, testInterval)
	start(t, w)
	return w
}

// writeKubeconfig writes a kubeconfig at path holding one context per name, each
// with its own cluster and user entry.
func writeKubeconfig(t *testing.T, path string, contexts ...string) {
	t.Helper()
	cfg := api.NewConfig()
	for _, name := range contexts {
		cfg.Contexts[name] = &api.Context{Cluster: name + "-cluster", AuthInfo: name + "-user"}
		cfg.Clusters[name+"-cluster"] = &api.Cluster{Server: "https://" + name + ".invalid"}
		cfg.AuthInfos[name+"-user"] = &api.AuthInfo{}
	}
	require.NoError(t, clientcmd.WriteToFile(*cfg, path))
}

// The service's whole job starts here: a poll that never reads the file leaves every
// consumer looking at the empty seed.
func TestPollLoadsTheConfig(t *testing.T) {
	// An interval far longer than the test, so only Start's own first poll can
	// explain a loaded config.
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")

	start(t, w)

	cfg, read := w.Get()
	assert.True(t, read)
	assert.Contains(t, cfg.Contexts, "prod")
}

// An absent kubeconfig reads as the same empty config as the pre-read seed, so the
// value cannot tell them apart — which is the whole reason Get reports the read. A
// consumer that waits for one it never gets would never observe anything.
func TestAnAbsentKubeconfigStillCountsAsRead(t *testing.T) {
	w, _ := newTestService(t, time.Hour)

	start(t, w)

	cfg, read := w.Get()
	assert.True(t, read, "an absent kubeconfig is an answer, not a pending read")
	assert.Empty(t, cfg.Contexts)
}

func TestNewReadsNothing(t *testing.T) {
	// Constructing must not touch the filesystem — the machine running this has its
	// own kubeconfig, and picking it up would make every other test host-dependent.
	w := New(filepath.Join(t.TempDir(), "config"), nil)

	cfg, read := w.Get()
	require.NotNil(t, cfg, "Get before Start")
	assert.False(t, read, "nothing has been read yet")
	assert.Empty(t, cfg.Contexts)
	assert.Equal(t, defaultPollInterval, w.interval, "New paces itself; only tests override")
}

func TestWatchConfigStateIsCurrentOnSubscribe(t *testing.T) {
	// A consumer subscribes at startup and must not wait out a poll interval for
	// its first snapshot.
	w := New(filepath.Join(t.TempDir(), "config"), nil)
	sub := w.WatchConfigState()
	defer sub.Close()

	cfg := testutil.Recv(t, sub.Chan(), "seed config")
	assert.NotNil(t, cfg)
}

// The reason subscribers exist: an edit has to reach them without anyone asking.
func TestChangedConfigIsPublished(t *testing.T) {
	w, path := newTestService(t, testInterval)
	writeKubeconfig(t, path, "prod")
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	writeKubeconfig(t, path, "prod", "staging")

	cfg := testutil.Recv(t, sub.Chan(), "the config after the edit")
	assert.Contains(t, cfg.Contexts, "staging")
}

// The backstop tick is half an hour, so an edit reaching a subscriber before then
// can only have come from the notification layer. Every test below pins its interval
// at an hour for that reason: there is no timing margin to tune, because the poll
// cannot fire at all.
func TestWriteWakesThePoll(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	writeKubeconfig(t, path, "prod", "staging")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the edit").Contexts, "staging")
}

// The case a file-level watch cannot survive, and the reason the watch is on the
// parent directory: replacing the inode leaves a file watch attached to the old one,
// so it fires once and then goes deaf. `kubectl config` and most editors save this
// way, which makes it the common path rather than an edge case.
func TestAtomicReplaceWakesThePoll(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	replace := func(contexts ...string) {
		t.Helper()
		tmp := path + ".tmp"
		writeKubeconfig(t, tmp, contexts...)
		swapIn(t, tmp, path)
	}

	// Twice, because once proves nothing: the first replace still reaches a
	// file-level watch — it is the SECOND that finds it attached to the discarded
	// inode and silent.
	replace("prod", "staging")
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the first replace").Contexts, "staging")

	replace("prod", "staging", "dev")
	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the second replace").Contexts, "dev")
}

// A machine with no kubeconfig is an ordinary starting state — the sidecar comes up
// before the user has run `kubectl config` even once. Watching the directory is what
// sees the file appear; a watch on a path that does not exist could not be
// established at all.
func TestKubeconfigCreatedAfterStartWakesThePoll(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	writeKubeconfig(t, path, "prod")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after creation").Contexts, "prod")
}

// newSymlinkedService returns an unstarted service whose kubeconfig path is a
// symlink into a second directory — how a dotfiles manager lays it out.
func newSymlinkedService(t *testing.T, interval time.Duration) (*Service, string, string) {
	t.Helper()
	root := t.TempDir()
	kubeDir, dotfiles := filepath.Join(root, "kube"), filepath.Join(root, "dotfiles")
	require.NoError(t, os.MkdirAll(kubeDir, 0o700))
	require.NoError(t, os.MkdirAll(dotfiles, 0o700))

	link, target := filepath.Join(kubeDir, "config"), filepath.Join(dotfiles, "config")
	writeKubeconfig(t, target, "prod")
	require.NoError(t, os.Symlink(target, link))

	return newServiceAt(t, link, interval), link, target
}

// A dotfiles-managed kubeconfig is a symlink, and its target lives in a directory
// nobody would otherwise watch — so an edit there changes nothing in ~/.kube and
// fires no event. `kubectl config use-context` writes through the link exactly this
// way, which makes it the routine case for those machines, not an edge.
func TestSymlinkTargetEditWakesThePoll(t *testing.T) {
	w, _, target := newSymlinkedService(t, time.Hour)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	writeKubeconfig(t, target, "prod", "staging")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the target edit").Contexts, "staging")
}

// Switching a dotfiles profile re-points the link at a different file, in a
// directory that was not watched when the service started. The resolution is
// recomputed per reload rather than captured once, so the new target's directory is
// picked up — otherwise edits to it would be invisible until the backstop.
//
// The poll drives this one, not the notifier: replacing a symlink is invisible to
// kqueue. It registers a directory's entries with O_EVTONLY, which follows the link, so
// unlinking it leaves that descriptor's inode alive and fires no delete — and the
// directory diff behind the watch synthesizes Create alone. This is the case pull-first
// exists for, and the only mechanism that covers it on every platform.
func TestRepointedSymlinkFollowsToTheNewTarget(t *testing.T) {
	w, link, target := newSymlinkedService(t, testInterval)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	// A second profile, in its own directory.
	elsewhere := filepath.Join(filepath.Dir(filepath.Dir(target)), "other")
	require.NoError(t, os.MkdirAll(elsewhere, 0o700))
	moved := filepath.Join(elsewhere, "config")
	writeKubeconfig(t, moved, "work")

	// Swapped in by rename: a remove-then-link leaves a moment with no kubeconfig,
	// and a poll landing in it publishes the empty config ahead of "work".
	tmp := link + ".tmp"
	require.NoError(t, os.Symlink(moved, tmp))
	swapIn(t, tmp, link)
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the config after re-pointing").Contexts, "work")

	// The claim: the new target is followed from here on, not just at the moment of the
	// switch. Which directories that left watched is TestRepointedSymlinkDropsTheOldDirectory's.
	writeKubeconfig(t, moved, "work", "staging")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after editing the new target").Contexts, "staging")
}

// A link whose target does not exist yet — a dotfiles checkout part-way through, or
// a profile not yet materialized. EvalSymlinks fails on it, which must leave the
// link's own path watched rather than the whole set: the target appearing is the
// event worth seeing, and it arrives through the link's directory.
func TestDanglingSymlinkStillSeesItsTargetAppear(t *testing.T) {
	root := t.TempDir()
	kubeDir := filepath.Join(root, "kube")
	require.NoError(t, os.MkdirAll(kubeDir, 0o700))
	link := filepath.Join(kubeDir, "config")
	require.NoError(t, os.Symlink(filepath.Join(kubeDir, "absent"), link))

	w := newServiceAt(t, link, time.Hour)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	writeKubeconfig(t, filepath.Join(kubeDir, "absent"), "prod")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config once the target appeared").Contexts, "prod")
}

// A reload that finds nothing changed must publish nothing: every subscriber treats a
// snapshot as news, and a discovery pass re-lists every Cluster on one. Driven by calling
// poll directly, since the silence in the middle is what is under test — the third
// frame being the edited config is what proves the second poll stayed quiet.
func TestPollDoesNotRepublishAnUnchangedConfig(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	w.poll()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the first read").Contexts, "prod")

	w.poll()

	writeKubeconfig(t, path, "prod", "staging")
	w.poll()
	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the edit").Contexts, "staging")
}

// A dotfiles manager links to a target beside the link rather than by absolute path.
// Readlink hands back what the link literally holds, so a relative one has to be
// joined onto the link's own directory or the watch lands on a path relative to the
// process's working directory.
func TestDanglingRelativeSymlinkResolvesAgainstItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config")
	require.NoError(t, os.Symlink("actual", link))

	target, ok := linkTarget(link)

	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "actual"), target)
}

// The notification layer is optional by design, so the kernel refusing one must leave
// a service that still works — the poll is what makes it correct.
func TestNotificationsUnavailableLeavesThePollWorking(t *testing.T) {
	w, path := newTestService(t, testInterval)
	w.newFSWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("no watchers left") }
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	writeKubeconfig(t, path, "prod")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config the tick found").Contexts, "prod")
}

// Watch errors are reported by the kernel for reasons that pass — a queue overflow, a
// directory going away mid-scan. The loop logs and carries on; ending it would trade a
// transient error for a service that never notices anything again.
func TestAWatchErrorDoesNotEndTheLoop(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	fsw, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	w.newFSWatcher = func() (*fsnotify.Watcher, error) { return fsw, nil }
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	// An unbuffered send, so it returns only once the loop has taken the error — no
	// window to size, and the reload below then proves the loop is still running.
	fsw.Errors <- errors.New("watch overflow")

	writeKubeconfig(t, path, "prod")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after the error").Contexts, "prod")
}

// KUBECONFIG may name several files, which kubectl merges in order — a shared team
// config plus a personal one is the usual shape. Every file in the chain has to be
// watched, not just the first: an edit to any of them changes the merged result.
func TestKubeconfigEnvChainIsMergedAndWatched(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "a"), filepath.Join(root, "b")
	require.NoError(t, os.MkdirAll(first, 0o700))
	require.NoError(t, os.MkdirAll(second, 0o700))

	shared, personal := filepath.Join(first, "config"), filepath.Join(second, "config")
	writeKubeconfig(t, shared, "prod")
	writeKubeconfig(t, personal, "laptop")
	t.Setenv("KUBECONFIG", shared+string(filepath.ListSeparator)+personal)

	// No explicit path: an explicit one overrides the chain, as kubectl's
	// --kubeconfig does.
	w := newServiceAt(t, "", time.Hour)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	cfg := testutil.Recv(t, sub.Chan(), "the merged config")
	require.Contains(t, cfg.Contexts, "prod")
	require.Contains(t, cfg.Contexts, "laptop", "both files merge")

	// The second file lives in its own directory, so this only arrives if the whole
	// chain is watched rather than the first entry. Replaced by rename: written in
	// place, the file is empty for a moment, and with the first file's contexts beside
	// it that window publishes as a config without "laptop".
	tmp := personal + ".tmp"
	writeKubeconfig(t, tmp, "laptop", "staging")
	swapIn(t, tmp, personal)

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config after editing the second file").Contexts, "staging")
}

// A fresh machine has no ~/.kube at all — the app can be installed before the user
// has ever run kubectl. Discovering the config on the backstop tick is fine; staying
// blind afterwards is not, so the notifier is kept even with nothing to watch yet and
// each reload retries the directories.
func TestNotifierSurvivesHavingNothingToWatchYet(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "kube")
	w := New(filepath.Join(dir, "config"), nil)
	w.loadingRules = w.newLoadingRules()

	fsw := w.newNotifier(w.resolvePaths())
	require.NotNil(t, fsw, "keep the notifier: a later reload is what adds the directory")
	defer func() { assert.NoError(t, fsw.Close()) }()
	require.Empty(t, fsw.WatchList(), "nothing is watchable yet")

	require.NoError(t, os.MkdirAll(dir, 0o700))
	syncDirs(fsw, w.resolvePaths())

	assert.Contains(t, fsw.WatchList(), dir, "the reload picks the directory up once it exists")
}

// clientcmd.WriteToFile — what `kubectl config use-context` calls — truncates and
// then writes, so the file is briefly zero length. A zero-length kubeconfig is not a
// parse error: it loads as a VALID config with no contexts, and publishing that tells
// every consumer the user's clusters vanished. The controller reads an absent context
// as IsPresent=false, so a flap there orphans every Cluster record and restores it
// microseconds later.
func TestTruncateWindowIsNotPublished(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	w.settle = 100 * time.Millisecond
	writeKubeconfig(t, path, "prod")
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Contains(t, testutil.Recv(t, sub.Chan(), "the loaded config").Contexts, "prod")

	// The two halves of an in-place rewrite, with the gap between them widened on
	// purpose: a real writer leaves microseconds, too short for the test to depend on
	// losing that race. This is the scenario, not a wait for something to happen.
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	time.Sleep(10 * time.Millisecond)
	writeKubeconfig(t, path, "prod", "staging")

	cfg := testutil.Recv(t, sub.Chan(), "the config after the rewrite")
	assert.Contains(t, cfg.Contexts, "staging", "the truncate window must not reach subscribers")
}

// The settle timer keeps notifications out of the writer's truncate window, but a
// tick or a poke can land in it too — and the file loads there as a valid config with
// no contexts. Pinned on poll directly, on an unstarted service: the window is one
// syscall wide, so reaching it through the loop would be a race the test could only
// lose quietly.
func TestATruncatedFileIsNotPublished(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")
	w.poll()
	cfg, _ := w.Get()
	require.Contains(t, cfg.Contexts, "prod", "the last good config")

	require.NoError(t, os.WriteFile(path, nil, 0o600))
	w.poll()

	cfg, read := w.Get()
	assert.True(t, read)
	assert.Contains(t, cfg.Contexts, "prod", "the truncate window must not replace the last good config")
}

// A writer can truncate the file after the poll has looked at it and fill it again
// after the load has read it, so the load reads the window while neither look sees
// an empty file. The load is rigged to land in that window, and driven by calling
// poll on an unstarted service so the rigged load runs on the test's goroutine alone.
func TestALoadDuringARewriteIsNotPublished(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	writeKubeconfig(t, path, "prod")
	w.poll()
	cfg, _ := w.Get()
	require.Contains(t, cfg.Contexts, "prod", "the last good config")

	w.load = func(rules *clientcmd.ClientConfigLoadingRules) (*api.Config, error) {
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		cfg, err := rules.Load()
		writeKubeconfig(t, path, "prod", "staging")
		return cfg, err
	}
	w.poll()

	cfg, read := w.Get()
	assert.True(t, read)
	assert.Contains(t, cfg.Contexts, "prod", "a read the rewrite overlapped must not replace the last good config")
}

// Re-pointing a symlink adds the new target's directory; the old one has to go, or
// it stays watched for the life of the process — waking the loop on every unrelated
// write in it, and on macOS holding an fd per file it contains.
func TestRepointedSymlinkDropsTheOldDirectory(t *testing.T) {
	w, link, target := newSymlinkedService(t, time.Hour)

	fsw := w.newNotifier(w.resolvePaths())
	require.NotNil(t, fsw)
	defer func() { assert.NoError(t, fsw.Close()) }()
	require.Contains(t, fsw.WatchList(), filepath.Dir(target), "the original target's directory")

	elsewhere := filepath.Join(filepath.Dir(filepath.Dir(target)), "other")
	require.NoError(t, os.MkdirAll(elsewhere, 0o700))
	moved := filepath.Join(elsewhere, "config")
	writeKubeconfig(t, moved, "work")
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(moved, link))

	syncDirs(fsw, w.resolvePaths())

	assert.Contains(t, fsw.WatchList(), elsewhere, "the new target's directory")
	assert.NotContains(t, fsw.WatchList(), filepath.Dir(target), "the old target's directory")
}

// The claim pull-first buys: notifications are allowed to fail. A kubeconfig whose
// directory does not exist yet cannot be watched at all, and the service still has
// to converge — on the tick, which is the thing that makes it correct.
func TestUnwatchableDirStillPolls(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	path := filepath.Join(dir, "config")
	w := newServiceAt(t, path, testInterval)
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	require.Empty(t, testutil.Recv(t, sub.Chan(), "the empty seed").Contexts)

	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeKubeconfig(t, path, "prod")

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the config the tick found").Contexts, "prod")
}

// Tested as a predicate rather than end-to-end: a stray wake publishes nothing (the
// config is unchanged), so the only way to observe one from outside would be to
// instrument the poll for the test's benefit.
func TestOnlyPrecedencePathsWakeThePoll(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	dir := filepath.Dir(path)
	paths := w.resolvePaths()

	assert.Contains(t, paths, path, "the kubeconfig itself")
	assert.NotContains(t, paths, filepath.Join(dir, "cache.db"), "a sibling file")
	assert.NotContains(t, paths, dir, "the watched directory itself")
}

// The link and its target are both watched, so an edit through either path is seen.
func TestResolvePathsFollowsASymlink(t *testing.T) {
	w, link, target := newSymlinkedService(t, time.Hour)

	paths := w.resolvePaths()
	assert.Contains(t, paths, link, "the kubeconfig path itself")
	assert.Contains(t, paths, target, "the file it resolves to")
}

// A kubeconfig is edited by hand, so it is routinely half-written or briefly
// invalid. Treating that as "no clusters" would tear down every connection over a
// typo, so the last good config stands until a readable one replaces it.
func TestUnparseableConfigKeepsTheLastGoodOne(t *testing.T) {
	w, path := newTestService(t, testInterval)
	writeKubeconfig(t, path, "prod")
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the loaded config")

	require.NoError(t, os.WriteFile(path, []byte("{{ not yaml"), 0o600))

	// Negative window over several poll intervals: a poll that adopted the failure
	// would publish the empty config it fell back to.
	select {
	case cfg := <-sub.Chan():
		t.Fatalf("published %v for an unparseable kubeconfig", cfg)
	case <-time.After(10 * testInterval):
	}
	cfg, _ := w.Get()
	assert.Contains(t, cfg.Contexts, "prod", "the last good config must stand")
}

func TestUnchangedConfigPublishesNothing(t *testing.T) {
	w := newStartedService(t)
	sub := w.WatchConfigState()
	defer sub.Close()

	testutil.Recv(t, sub.Chan(), "seed config")

	// A negative assertion, so there is no event to wait for: the window is a
	// multiple of the shrunk cadence, wide enough that a poll publishing
	// unconditionally would have fired several times. Fails the moment a frame
	// arrives rather than at the deadline.
	select {
	case cfg := <-sub.Chan():
		t.Fatalf("unchanged kubeconfig published %v", cfg)
	case <-time.After(10 * testInterval):
	}
}

// Stopping is not closing: a subscriber must still be able to read what the last
// poll published after the loop is joined.
func TestStopLeavesSubscriptionsOpen(t *testing.T) {
	w, _ := newTestService(t, testInterval)
	sub := w.WatchConfigState()
	defer sub.Close()
	stop, err := w.Start(context.Background())
	require.NoError(t, err)
	testutil.Recv(t, sub.Chan(), "seed config")

	require.NoError(t, stop(context.Background()))

	// The loop is already joined, so a close would be visible on this receive right
	// now — no window to wait out.
	select {
	case _, ok := <-sub.Chan():
		assert.True(t, ok, "stop must not close the subscription")
	default:
	}

	require.NoError(t, w.Close())
	testutil.WaitClosed(t, sub.Chan(), "subscription after Close")
}

// lifecycle.StartCloser requires an idempotent stop, so a retry or a double drain above
// must find this one idempotent rather than panicking on a closed channel.
func TestStopIsIdempotent(t *testing.T) {
	w, _ := newTestService(t, testInterval)
	stop, err := w.Start(context.Background())
	require.NoError(t, err)

	require.NoError(t, stop(context.Background()))
	assert.NoError(t, stop(context.Background()))

	assert.NoError(t, w.Close())
}

func TestCloseWithoutStart(t *testing.T) {
	// New is called in cluster.New and Close in service.Close, with Start only in
	// between — a failure between the two must not deadlock on a loop never launched.
	w := New(filepath.Join(t.TempDir(), "config"), nil)
	assert.NoError(t, w.Close())
}

func TestStopJoinsPollLoop(t *testing.T) {
	// A stop that returned while the loop still ran would let a poll publish after
	// the owner considers it drained. The interval is far longer than the test, so
	// the claim is that stop selects on the stop channel rather than waiting a tick.
	w, _ := newTestService(t, time.Hour)
	stop, err := w.Start(context.Background())
	require.NoError(t, err)

	testutil.WaitReturn(t, func() { assert.NoError(t, stop(context.Background())) }, "stop to return")

	assert.NoError(t, w.Close())
}

// A poke is what makes a resume prompt. The kubeconfig lands in a directory that did
// not exist at Start, so nothing is watching it and no event can be mistaken for the
// poke's own wake — which is also the shape the poke is for, a change the notifier
// never saw.
func TestPokeWakesThePoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kube", "config")
	pokeSvc := poke.New()

	w := newServiceAt(t, path, time.Hour)
	w.pokeSvc = pokeSvc
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the seed")

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	writeKubeconfig(t, path, "prod")

	pokeSvc.Poke(poke.SourceHost)

	cfg := testutil.Recv(t, sub.Chan(), "the reload the poke woke")
	assert.Contains(t, cfg.Contexts, "prod")
}

// A closed poke bus must not spin the loop: a receive on a closed channel returns
// immediately, forever. The tick and the notifier carry on without it.
func TestClosedPokeBusLeavesTheLoopServing(t *testing.T) {
	pokeSvc := poke.New()
	stopPoke, err := pokeSvc.Start(context.Background())
	require.NoError(t, err)

	w, path := newTestService(t, testInterval)
	w.pokeSvc = pokeSvc
	start(t, w)

	sub := w.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the seed")

	require.NoError(t, stopPoke(context.Background()))

	writeKubeconfig(t, path, "prod")
	cfg := testutil.Recv(t, sub.Chan(), "a reload after the bus closed")
	assert.Contains(t, cfg.Contexts, "prod")
}

// KUBECONFIG is read at Start, not New: the app starts the service once the
// login shell's exports are in the environment. The first read finds the file
// the variable names, and that file is the one watched.
func TestKubeconfigIsReadAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	w := New("", nil)
	w.interval, w.settle = time.Hour, testSettle
	sub := w.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the seed")

	t.Setenv("KUBECONFIG", path)
	writeKubeconfig(t, path, "prod")
	start(t, w)

	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the first read").Contexts, "prod")

	writeKubeconfig(t, path, "prod", "staging")
	assert.Contains(t, testutil.Recv(t, sub.Chan(), "the edit").Contexts, "staging")
}

// A first read that fails to load marks the service read and publishes nothing —
// the empty seed stands, and the cluster passes run on their tick.
func TestAFirstReadThatFailsMarksTheServiceRead(t *testing.T) {
	w, path := newTestService(t, time.Hour)
	require.NoError(t, os.WriteFile(path, []byte("{{ not yaml"), 0o600))
	sub := w.WatchConfigState()
	defer sub.Close()
	testutil.Recv(t, sub.Chan(), "the seed")

	start(t, w)

	cfg, read := w.Get()
	assert.True(t, read)
	assert.Empty(t, cfg.Contexts)
	testutil.NoRecv(t, sub.Chan(), 10*testSettle, "a publish for a read that failed")
}

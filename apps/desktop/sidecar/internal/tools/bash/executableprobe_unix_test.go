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

package bash

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// writeExecutable writes an executable script at dir/name that runs body.
func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return path
}

// A name is the first regular, executable file of that name in the PATH's
// entries, in order: a file that does not run and a directory are passed over.
func TestAnExecutableResolvesToTheFirstEntry(t *testing.T) {
	base := t.TempDir()
	a, b, c := filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "c")
	require.NoError(t, os.MkdirAll(filepath.Join(a, "kubectl"), 0o755))
	require.NoError(t, os.MkdirAll(b, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(b, "kubectl"), nil, 0o644))
	want := writeExecutable(t, c, "kubectl", "true")
	writeExecutable(t, filepath.Join(base, "d"), "kubectl", "true")

	got, ok := resolveExecutable("kubectl", []string{a, b, c, filepath.Join(base, "d")})
	assert.True(t, ok)
	assert.Equal(t, want, got)

	_, ok = resolveExecutable("helm", []string{a, b, c})
	assert.False(t, ok)
}

// A shim is a file under asdf's or mise's shims folder, named with the
// manager whose resolver finds the binary behind it: asdf's a script, mise's
// a link to mise itself, which lies elsewhere. A #! script elsewhere is no
// shim.
func TestAFileUnderAShimsFolderIsAShim(t *testing.T) {
	home := t.TempDir()
	asdf := writeExecutable(t, filepath.Join(home, ".asdf", "shims"), "kubectl", "exec asdf exec kubectl")
	miseBin := writeExecutable(t, filepath.Join(home, ".local", "bin"), "mise", "exec mise")
	shims := filepath.Join(home, ".local", "share", "mise", "shims")
	require.NoError(t, os.MkdirAll(shims, 0o755))
	mise := filepath.Join(shims, "helm")
	require.NoError(t, os.Symlink(miseBin, mise))
	script := writeExecutable(t, filepath.Join(home, "bin"), "kubectl", "exec kubectl")

	for path, want := range map[string]string{asdf: "asdf", mise: "mise", script: ""} {
		got, ok := shimManager(path, home)
		assert.Equal(t, want, got, path)
		assert.Equal(t, want != "", ok, path)
	}
}

// probingTool is a tool over a fake sandbox whose PATH is one folder of the
// test's own, which it answers.
func probingTool(t *testing.T) (*Tool, *fakeSandboxer, string) {
	t.Helper()
	tl := tool(t)
	fake := &fakeSandboxer{}
	tl.sandboxer = fake
	// Not under /tmp, which a typed folder's check refuses on Linux.
	bin := testutil.GrantableDir(t)
	onPath(tl, bin)
	return tl, fake, bin
}

// onPath makes dirs tl's PATH, each a folder the user typed.
func onPath(tl *Tool, dirs ...string) {
	var folders []settings.PathFolder
	for _, d := range dirs {
		folders = append(folders, typed(d))
	}
	tl.pathList = searching(folders...)
}

// reportOf is the report of name in reports.
func reportOf(t *testing.T, reports []ExecutableReport, name string) ExecutableReport {
	t.Helper()
	i := slices.IndexFunc(reports, func(r ExecutableReport) bool { return r.Name == name })
	require.GreaterOrEqual(t, i, 0, name)
	return reports[i]
}

// kubectl is the curated list's first tool.
var kubectl = settings.CuratedExecutables[0]

// A PATH with no kubectl reports it not found, and runs nothing for it.
func TestAMissingKubectlIsReported(t *testing.T) {
	tl, fake, _ := probingTool(t)

	got := tl.ProbeExecutables(t.Context(), nil)

	require.Len(t, got, len(settings.CuratedExecutables))
	assert.Equal(t, ExecutableReport{Executable: kubectl, Probed: true, Error: "not found on the sandbox's PATH"}, got[0])
	assert.Empty(t, fake.seen())
}

// The probe runs each listed invocation, curated and registered, as the
// resolved binary and its arguments, never through a shell; a binary planted
// beside them is never run.
func TestTheProbeRunsOnlyTheList(t *testing.T) {
	tl, fake, bin := probingTool(t)
	kubectlBin := writeExecutable(t, bin, "kubectl", `echo; echo "Client Version: v1.31.0"`)
	helmBin := writeExecutable(t, bin, "helm", "echo 'Error: no such command' >&2; exit 3")
	k9sBin := writeExecutable(t, bin, "k9s", `echo "k9s $1"`)
	writeExecutable(t, bin, "planted", "true")
	k9s := settings.Executable{Name: "k9s", Invocation: "k9s info"}

	got := tl.ProbeExecutables(t.Context(), []settings.Executable{k9s})

	var ran [][]string
	for _, r := range fake.seen() {
		ran = append(ran, append([]string{r.Shell}, r.Args...))
	}
	assert.Equal(t, [][]string{{kubectlBin, "version", "--client"}, {helmBin, "version"}, {k9sBin, "info"}}, ran)
	assert.Equal(t, ExecutableReport{
		Executable: kubectl, Probed: true, Resolved: kubectlBin, OK: true, Version: "Client Version: v1.31.0",
	}, got[0])
	assert.Equal(t, ExecutableReport{
		Executable: settings.CuratedExecutables[1], Probed: true,
		Resolved: helmBin, Version: "Error: no such command", Error: "Exit code 3",
	}, reportOf(t, got, "helm"))
	assert.Equal(t, ExecutableReport{
		Executable: k9s, Registered: true, Probed: true, Resolved: k9sBin, OK: true, Version: "k9s info",
	}, reportOf(t, got, "k9s"))
	assert.Equal(t, got, tl.LastProbe())
}

// A probe runs no registered executable the shape check refuses, however its
// caller passed it: marked, or malformed and unmarked. The curated kubectl
// still runs beside a registered kubectl.
func TestAProbeRunsNoRefusedExecutable(t *testing.T) {
	tl, fake, bin := probingTool(t)
	kubectlBin := writeExecutable(t, bin, "kubectl", "echo v1")
	writeExecutable(t, bin, "other", "true")
	writeExecutable(t, bin, "k9s", "true")
	registered := []settings.Executable{
		{Name: "kubectl", Invocation: "kubectl version"},
		{Name: "stern", Invocation: "other --version"},
		{Name: "k9s", Invocation: "k9s info", Refusal: "Its own reason."},
	}

	got := tl.ProbeExecutables(t.Context(), registered)

	var ran [][]string
	for _, r := range fake.seen() {
		ran = append(ran, append([]string{r.Shell}, r.Args...))
	}
	assert.Equal(t, [][]string{{kubectlBin, "version", "--client"}}, ran, "the curated kubectl alone")
	assert.True(t, got[0].OK, "the curated kubectl")
	for i, r := range got[len(settings.CuratedExecutables):] {
		assert.NotEmpty(t, r.Refusal, registered[i].Name)
		assert.False(t, r.Probed, registered[i].Name)
		assert.Empty(t, r.Error, registered[i].Name)
	}
}

// A kubectl under asdf's shims is reported as a shim, with the binary asdf's
// resolver names, which runs in the sandbox first; a #! script elsewhere is
// no shim.
func TestAShimResolvesToItsTarget(t *testing.T) {
	tl, fake, bin := probingTool(t)
	// Not under /tmp, which a typed folder's check refuses on Linux.
	tl.home = testutil.GrantableDir(t)
	shims := filepath.Join(tl.home, ".asdf", "shims")
	shim := writeExecutable(t, shims, "kubectl", "echo v1.31.0")
	asdf := writeExecutable(t, bin, "asdf", `echo "/opt/asdf/installs/kubectl/1.31.0/bin/$2"`)
	helm := writeExecutable(t, bin, "helm", "echo v3.16.0")
	onPath(tl, shims, bin)

	got := tl.ProbeExecutables(t.Context(), nil)

	shimmed := reportOf(t, got, "kubectl")
	assert.True(t, shimmed.Shim)
	assert.Equal(t, shim, shimmed.Resolved)
	assert.Equal(t, "/opt/asdf/installs/kubectl/1.31.0/bin/kubectl", shimmed.Target)
	assert.True(t, shimmed.OK)
	assert.False(t, reportOf(t, got, "helm").Shim)
	var ran []string
	for _, r := range fake.seen() {
		ran = append(ran, r.Shell+" "+strings.Join(r.Args, " "))
	}
	assert.Equal(t, []string{asdf + " which kubectl", shim + " version --client", helm + " version"}, ran)
}

// A tool that runs past the probe's bound is stopped and reported as Bash
// reports a timeout. The test fires the deadline once the tool has started,
// so how long the run takes to start never decides it.
func TestAProbeIsBounded(t *testing.T) {
	tl, _, bin := probingTool(t)
	h := newHolder(t)
	c := newClock()
	tl.probe.after = func(d time.Duration) <-chan time.Time {
		assert.Equal(t, 15*time.Second, d)
		return c
	}
	// The tool execs the sleep, so the pid it greets with is the sleep's.
	writeExecutable(t, bin, "kubectl", "exec 3>"+h.path+"; echo started $$ >&3; exec /bin/sleep 60")

	res := make(chan []ExecutableReport, 1)
	go func() { res <- tl.ProbeExecutables(t.Context(), nil) }()
	h.awaitStarted(t)
	c.fire()
	got := testutil.Recv(t, res, "the probe to end")

	assert.False(t, got[0].OK)
	assert.True(t, strings.HasPrefix(got[0].Error, "Command timed out after 15s"), got[0].Error)
	h.awaitGone(t)
}

// Each run's throwaway home and tool home are gone once it ends.
func TestTheProbesWorkspaceIsGone(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", `echo "$HOME" > "$HOME/where"`)
	writeExecutable(t, bin, "helm", "true")

	tl.ProbeExecutables(t.Context(), nil)

	runs := fake.seen()
	require.Len(t, runs, 2)
	var homes []string
	for _, r := range runs {
		for _, kv := range r.Env {
			if home, ok := strings.CutPrefix(kv, "HOME="); ok {
				homes = append(homes, home)
				assert.NoDirExists(t, filepath.Dir(home))
			}
		}
	}
	require.Len(t, homes, 2)
	assert.NotEqual(t, homes[0], homes[1], "each run has a fresh one")
	entries, err := os.ReadDir(tl.tmpDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "-probe-")
	}
}

// The probe's run reads the folders it was handed, the ones granted always,
// as any run reads its session's: here as a Read rule.
func TestAProbeReadsTheAlwaysFoldersAlone(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	granted := testutil.GrantableDir(t)
	tl.SetAlwaysFolders(func(context.Context) ([]session.Folder, error) {
		return []session.Folder{{Path: granted}}, nil
	})

	tl.ProbeExecutables(t.Context(), nil)

	require.Len(t, fake.seen(), 1)
	assert.Contains(t, fake.seen()[0].Policy.Files.Read, granted)
}

// asked is how many callers wait on the probe queued behind the running one.
func (p *executableProbe) asked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.queued == nil {
		return 0
	}
	return p.queued.asked
}

// A second and a third probe asked while the first runs wait for it, then
// share one run, which starts after both asked.
func TestOneProbeRunsAtATime(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	fake.entered, fake.gate = make(chan struct{}, 2), make(chan struct{})
	first := make(chan []ExecutableReport, 1)
	go func() { first <- tl.ProbeExecutables(t.Context(), nil) }()
	testutil.Recv(t, fake.entered, "the first probe's run")

	later := make(chan []ExecutableReport, 2)
	for range 2 {
		go func() { later <- tl.ProbeExecutables(t.Context(), nil) }()
	}
	require.Eventually(t, func() bool { return tl.probe.asked() == 2 }, testutil.Timeout, time.Millisecond)
	assert.Empty(t, fake.seen(), "nothing else starts while the first runs")
	close(fake.gate)

	testutil.Recv(t, first, "the first probe")
	a, b := testutil.Recv(t, later, "the second probe"), testutil.Recv(t, later, "the third probe")
	assert.Len(t, fake.seen(), 2, "one run each for the first and the two that waited")
	assert.Same(t, &a[0], &b[0], "the two that waited share one report")
}

// Close ends a running probe and waits for it.
func TestCloseEndsARunningProbe(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	fake.hold = testutil.NewSignal()
	done := make(chan []ExecutableReport, 1)
	go func() { done <- tl.ProbeExecutables(context.Background(), nil) }()
	fake.hold.Wait(t, "the probe's run")

	require.NoError(t, tl.Close())

	got := testutil.Recv(t, done, "the probe")
	assert.Contains(t, got[0].Error, "could not start")
}

// A probe reading the disk when Close is called does not hold it: the PATH
// is built from System and Never, which a dead network mount can hold, so
// Close returns while the read is still parked, and each report says the
// probe could not start. The read is released only once the test ends.
func TestCloseEndsAProbeReadingTheDisk(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	reading := testutil.NewSignal()
	fake.onSystem, fake.holdSys = func() { reading.Fire() }, make(chan struct{})
	t.Cleanup(func() { close(fake.holdSys) })
	done := make(chan []ExecutableReport, 1)
	go func() { done <- tl.ProbeExecutables(context.Background(), nil) }()
	reading.Wait(t, "the probe's read of the disk")

	require.NoError(t, tl.Close())

	got := testutil.Recv(t, done, "the probe")
	assert.Equal(t, "could not start: context canceled", got[0].Error)
	assert.False(t, got[0].Probed)
}

// A read of the disk that passes the probe's deadline fails that probe alone:
// each report says the disk did not answer, and the next probe runs once the
// disk does. The deadline is shrunk; the hold is the mount's own latency, the
// one the deadline is there to cut, and the assertion waits on nothing.
func TestAProbesDiskReadIsBounded(t *testing.T) {
	assert.Equal(t, 5*time.Second, newExecutableProbe().diskTimeout)
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	tl.probe.diskTimeout = 50 * time.Millisecond
	fake.onSystem, fake.holdSys = func() {}, make(chan struct{})

	got := tl.ProbeExecutables(t.Context(), nil)

	assert.Equal(t, "could not start: the disk did not answer in time", got[0].Error)
	assert.False(t, got[0].Probed, "the executable never ran")

	close(fake.holdSys)
	tl.probe.diskTimeout = diskTimeout // a loaded runner can take 50ms to build a run
	next := tl.ProbeExecutables(t.Context(), nil)
	assert.True(t, next[0].OK, next[0].Error)
}

// The run's build reads the disk again, after the PATH is built, and a read
// that passes the deadline there fails the run the same way: the tool is
// reported as run and could not start, and the next probe runs once the disk
// answers. The hold lets the PATH build's read through and parks the run's;
// only the build's deadline is shrunk, so the reads before it are not cut.
func TestAProbesRunBuildIsBounded(t *testing.T) {
	assert.Equal(t, 5*time.Second, newExecutableProbe().buildTimeout)
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	tl.probe.buildTimeout = 50 * time.Millisecond
	fake.onSystem, fake.holdSys = func() {}, make(chan struct{}, 1)
	fake.holdSys <- struct{}{}

	got := tl.ProbeExecutables(t.Context(), nil)

	assert.Equal(t, "could not start: the disk did not answer in time", got[0].Error)
	assert.True(t, got[0].Probed)
	assert.NotEmpty(t, got[0].Resolved)

	close(fake.holdSys)
	tl.probe.buildTimeout = diskTimeout // a loaded runner can take 50ms to build a run
	next := tl.ProbeExecutables(t.Context(), nil)
	assert.True(t, next[0].OK, next[0].Error)
}

// A run's build that fails on its own, before the deadline, reports its own
// error: a runs directory too long for a socket is one no deadline caused.
func TestAProbesSetupFailureKeepsItsError(t *testing.T) {
	tl, _, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	tl.runsDir = filepath.Join(t.TempDir(), strings.Repeat("x", longestRuns))

	got := tl.ProbeExecutables(t.Context(), nil)

	assert.True(t, strings.HasPrefix(got[0].Error, "could not start: the runs directory"), got[0].Error)
	assert.True(t, got[0].Probed)
}

// A probe whose throwaway directory cannot be made reports why and runs
// nothing: its folder under a file, or one whose lock it cannot take.
func TestAProbeWithNoDirectoryRunsNothing(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	shut := t.TempDir()
	require.NoError(t, os.Chmod(shut, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	for name, tmpDir := range map[string]string{"under a file": filepath.Join(file, "tmp"), "unwritable": shut} {
		t.Run(name, func(t *testing.T) {
			tl, fake, bin := probingTool(t)
			writeExecutable(t, bin, "kubectl", "true")
			tl.tmpDir = tmpDir

			got := tl.ProbeExecutables(t.Context(), nil)

			assert.True(t, strings.HasPrefix(got[0].Error, "could not start: "), got[0].Error)
			assert.Empty(t, fake.seen())
		})
	}
}

// A probe's directory that cannot be removed is left for the next start's
// sweep, and the probe still reports.
func TestAProbeDirectoryThatCannotBeRemovedIsLogged(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root removes any folder")
	}
	tl, _, bin := probingTool(t)
	// HOME is the workspace, two folders under the tmp directory the removal
	// opens.
	writeExecutable(t, bin, "kubectl", `/bin/chmod 0 "${HOME%/*/*}"`)
	t.Cleanup(func() { _ = os.Chmod(tl.tmpDir, 0o700) })
	logs := testutil.CaptureLogs(t)

	got := tl.ProbeExecutables(t.Context(), nil)

	assert.True(t, got[0].OK, got[0].Error)
	assert.Contains(t, logs.String(), "could not remove a probe's directory")
}

// recording is a real sandbox that keeps each Run it was handed.
type recording struct {
	*sandbox.Sandbox
	mu   sync.Mutex
	runs []sandbox.Run
}

func (r *recording) Command(ctx context.Context, run sandbox.Run) (*exec.Cmd, error) {
	r.mu.Lock()
	r.runs = append(r.runs, run)
	r.mu.Unlock()
	return r.Sandbox.Command(ctx, run)
}

// realProbingTool is a tool over this machine's sandbox, its home outside /tmp
// (a Linux run's own) and its PATH one folder of the test's own.
func realProbingTool(t *testing.T) (*Tool, *recording, string) {
	t.Helper()
	tl := tool(t)
	rec := &recording{Sandbox: confining(t)}
	tl.sandboxer = rec
	tl.home = testutil.GrantableDir(t)
	bin := testutil.GrantableDir(t)
	onPath(tl, bin)
	tl.SetAlwaysFolders(nil)
	return tl, rec, bin
}

// The probe's run has no cluster and no network: its policy names no relay
// and no internet, its environment no kubeconfig or proxy, and a listener on
// the loopback outside the sandbox gets no connection from a tool that dials
// it.
func TestTheProbesSandboxHasNoClusterAndNoNetwork(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")
	tl, rec, bin := realProbingTool(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			_ = c.Close()
			accepted <- struct{}{}
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	kubectl := filepath.Join(bin, "kubectl")
	require.NoError(t, os.WriteFile(kubectl, []byte("#!/bin/bash\nexec 3<>/dev/tcp/127.0.0.1/"+port+" && echo connected\n"), 0o755))

	got := tl.ProbeExecutables(t.Context(), nil)[0]

	assert.False(t, got.OK, "the dial fails: %s", got.Version)
	require.NotEmpty(t, rec.runs)
	r := rec.runs[0]
	assert.Empty(t, r.Policy.Network.Relays)
	assert.False(t, r.Policy.Network.Internet)
	for _, kv := range r.Env {
		name, _, _ := strings.Cut(kv, "=")
		assert.NotContains(t, []string{"KUBECONFIG", "HTTP_PROXY", "HTTPS_PROXY"}, name)
	}
	// A negative assertion: the tool has exited, so a connection it made is
	// already in the listener's queue, and the window only lets Accept return.
	select {
	case <-accepted:
		t.Fatal("the probe reached the loopback")
	case <-time.After(100 * time.Millisecond):
	}
}

// Report lays the last probe over the list: a tool the last probe ran with
// the same invocation answers its report, and one listed since, or changed,
// is not probed yet. A tool removed since is gone.
func TestReportOverlaysTheLastProbe(t *testing.T) {
	tl, _, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "echo v1")
	writeExecutable(t, bin, "k9s", "echo k9s")
	unprobed := func(tool settings.Executable) ExecutableReport {
		return ExecutableReport{Executable: tool, Registered: true, Error: "not probed yet"}
	}

	before := tl.Report(nil)
	assert.Equal(t, ExecutableReport{Executable: kubectl, Error: "not probed yet"}, before[0], "before the first probe")

	probed := tl.ProbeExecutables(t.Context(), []settings.Executable{{Name: "k9s", Invocation: "k9s version"}})
	changed := settings.Executable{Name: "k9s", Invocation: "k9s info"}
	added := settings.Executable{Name: "stern", Invocation: "stern --version"}
	got := tl.Report([]settings.Executable{changed, added})

	require.Len(t, got, len(settings.CuratedExecutables)+2)
	assert.Equal(t, probed[0], got[0])
	assert.Equal(t, unprobed(changed), got[len(got)-2], "an invocation changed")
	assert.Equal(t, unprobed(added), got[len(got)-1], "an executable listed since")
}

// StartProbe returns before the probe runs, which LastProbe then answers.
func TestStartProbeRunsInTheBackground(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	fake.entered, fake.gate = make(chan struct{}, 1), make(chan struct{})

	tl.StartProbe(nil)
	testutil.Recv(t, fake.entered, "the probe's run")
	assert.Nil(t, tl.LastProbe(), "it returned while the run waits")
	close(fake.gate)

	require.Eventually(t, func() bool { return tl.LastProbe() != nil }, testutil.Timeout, time.Millisecond)
	assert.True(t, tl.LastProbe()[0].OK)
}

// WatchProbeState sees a probe start, then end with its report.
func TestWatchProbeStateSeesAProbeStartAndEnd(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	fake.entered, fake.gate = make(chan struct{}, 1), make(chan struct{})
	rx := tl.WatchProbeState()
	defer rx.Close()
	states := rx.Chan()
	assert.Equal(t, ProbeState{}, testutil.Recv(t, states, "the state before any probe"))

	tl.StartProbe(nil)
	testutil.Recv(t, fake.entered, "the probe's run")
	assert.Equal(t, ProbeState{Probing: true, Probes: 1}, testutil.Recv(t, states, "the start"))
	close(fake.gate)

	end := testutil.Recv(t, states, "the end")
	assert.False(t, end.Probing)
	assert.Equal(t, 1, end.Probes)
	assert.True(t, end.Last[0].OK)
}

// Where no sandbox confines a run, nothing is probed.
func TestNoSandboxProbesNothing(t *testing.T) {
	tl := tool(t)
	tl.sandboxer = nil

	tl.StartProbe(nil)
	assert.Nil(t, tl.ProbeExecutables(t.Context(), nil))
	assert.Nil(t, tl.LastProbe())
}

// A caller whose context ends stops waiting and gets nil; the probe runs on.
func TestACallerThatLeavesGetsNoReport(t *testing.T) {
	tl, fake, bin := probingTool(t)
	writeExecutable(t, bin, "kubectl", "true")
	fake.entered, fake.gate = make(chan struct{}, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan []ExecutableReport, 1)
	go func() { done <- tl.ProbeExecutables(ctx, nil) }()
	testutil.Recv(t, fake.entered, "the probe's run")

	cancel()
	assert.Nil(t, testutil.Recv(t, done, "the caller"))

	close(fake.gate)
	require.Eventually(t, func() bool { return tl.LastProbe() != nil }, testutil.Timeout, time.Millisecond)
}

// A PATH that cannot be read starts no probe: each report says why.
func TestAProbeOverAPathThatCannotBeReadRunsNothing(t *testing.T) {
	tl, fake, _ := probingTool(t)
	tl.pathList = func(context.Context) (settings.RunPath, error) {
		return settings.RunPath{}, settings.PathRefusal("Your PATH setting cannot be used.")
	}

	got := tl.ProbeExecutables(t.Context(), nil)
	k := reportOf(t, got, kubectl.Name)
	assert.False(t, k.Probed)
	assert.Equal(t, "could not start: Your PATH setting cannot be used.", k.Error)
	assert.Empty(t, fake.seen())
}

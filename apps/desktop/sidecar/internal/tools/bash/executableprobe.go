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

package bash

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// ExecutableReport is one tool the probe checks, as the last probe found it.
type ExecutableReport struct {
	settings.Executable
	Registered bool   // by the user, in Settings
	Probed     bool   // a probe ran it; false for an executable listed since the last, and for one a probe could not start on, whose Error says why
	Resolved   string // the binary on the frozen PATH; "" when none
	Shim       bool   // under a version manager's shims folder
	Target     string // the binary behind a shim; "" when unknown
	OK         bool   // the invocation exited 0
	Version    string // the first non-empty line of its output, cut to maxVersion
	Error      string // why it did not run or did not pass; "" when OK, and for one refused
}

const (
	// probeTimeout bounds one invocation.
	probeTimeout = 15 * time.Second
	// diskTimeout bounds each of a probe's reads of the disk outside Kubetail's
	// directories, so a dead network mount fails the probe rather than holding
	// it and every probe queued behind it.
	diskTimeout = 5 * time.Second
	// probeCapture is the most of an invocation's output kept.
	probeCapture = 1 << 20
	// maxVersion is the most of a version line kept, in characters.
	maxVersion = 200

	notProbedYet = "not probed yet"
	notOnPath    = "not found on the sandbox's PATH"
)

// errDiskTimeout is a read of the disk that passed diskTimeout.
var errDiskTimeout = errors.New("the disk did not answer in time")

// shimDirs are the folders under the home each version manager keeps its
// shims in; the manager's `which <name>` names the binary a shim execs.
var shimDirs = map[string]string{
	"asdf": ".asdf/shims",
	"mise": ".local/share/mise/shims",
}

// executableList is every tool a probe lists, in the report's order: the
// curated tools, then the registered ones, each not probed yet. A registered
// one with no Refusal is checked here, so the probe runs none CheckExecutable
// refuses, whatever its caller passed. A refused one carries its Refusal and
// no error.
func executableList(registered []settings.Executable) []ExecutableReport {
	var list []ExecutableReport
	for _, tool := range settings.CuratedExecutables {
		list = append(list, ExecutableReport{Executable: tool, Error: notProbedYet})
	}
	for _, tool := range registered {
		if tool.Refusal == "" {
			if err := settings.CheckExecutable(tool.Name, tool.Invocation); err != nil {
				tool.Refusal = err.Error()
			}
		}
		report := ExecutableReport{Executable: tool, Registered: true}
		if tool.Refusal == "" {
			report.Error = notProbedYet
		}
		list = append(list, report)
	}
	return list
}

// resolveExecutable is the first regular, executable file named name in path's
// folders, in order: the binary a sandboxed command runs. It looks at that
// one name in each folder and never lists one.
func resolveExecutable(name string, path []string) (string, bool) {
	for _, dir := range path {
		file := filepath.Join(dir, name)
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return file, true
		}
	}
	return "", false
}

// shimManager is the manager whose shims folder under home holds the binary
// at path, or false. Many real tools are scripts, so a #! line alone makes
// nothing a shim. The file's folder is compared and the file is not: a mise
// shim is a link to mise itself, which lies in no shims folder.
func shimManager(path, home string) (string, bool) {
	for manager, dir := range shimDirs {
		if realpath.Under(filepath.Dir(path), filepath.Join(home, dir)) {
			return manager, true
		}
	}
	return "", false
}

// ProbeState is the probe as a watcher sees it: whether one runs, how many
// have started, and the last probe's report, nil before the first. The count
// is how a watcher tells the probe it asked for from the one before it: the
// hub keeps its latest value alone, so a start and its end can arrive as one.
type ProbeState struct {
	Probing bool
	Probes  int
	Last    []ExecutableReport
}

// executableProbe is the Bash tool's probe state.
type executableProbe struct {
	diskTimeout time.Duration // diskTimeout, or a test's shorter one
	// buildTimeout is the run's build's bound, diskTimeout in production;
	// its own field so a test can cut the build alone.
	buildTimeout time.Duration
	// after starts a run's deadline: time.After when nil, or a test's stand-in.
	after func(d time.Duration) <-chan time.Time

	// ctx is what every probe runs on, ended by Close; wg counts the probes
	// running.
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu      sync.Mutex
	running bool
	started int                // probes started
	queued  *probeBatch        // the probe every caller asking while one runs shares
	last    []ExecutableReport // nil before the first probe
	// hub publishes ProbeState on each start and each end, under mu, so a
	// watcher sees the end of a probe the mutation only started.
	hub *watch.Hub[ProbeState]
	tx  *watch.Sender[ProbeState]
}

// probeBatch is one probe, which every caller that asked for it shares.
type probeBatch struct {
	registered []settings.Executable // the last caller's
	asked      int
	done       chan struct{}
	reports    []ExecutableReport
}

func newExecutableProbe() *executableProbe {
	ctx, stop := context.WithCancel(context.Background())
	hub := watch.New(ProbeState{})
	return &executableProbe{
		ctx: ctx, stop: stop, diskTimeout: diskTimeout, buildTimeout: diskTimeout,
		hub: hub, tx: hub.Sender(),
	}
}

// publish sends the probe's state. Called under mu.
func (p *executableProbe) publish() {
	p.tx.Send(ProbeState{Probing: p.running, Probes: p.started, Last: slices.Clone(p.last)}) //nolint:errcheck // Send never blocks, and the hub closes with the probe
}

// ProbeExecutables runs each curated tool's invocation, then each registered
// one's, in the sandbox, one after another, and answers one report per tool
// in that order; nil where no sandbox confines a run, or once ctx ends. One
// probe runs at once: a call while one runs waits for it to end, then shares
// one more run with every other call that waited, which starts after all of
// them asked, on the registered tools the last of them gave. So no call
// answers a report begun before it.
func (t *Tool) ProbeExecutables(ctx context.Context, registered []settings.Executable) []ExecutableReport {
	if t.sandboxer == nil {
		return nil
	}
	batch := t.queueProbe(registered)
	select {
	case <-batch.done:
		return batch.reports
	case <-ctx.Done():
		return nil
	}
}

// StartProbe asks for a probe as ProbeExecutables does and returns at once; Close
// ends it, and WatchProbeState says when it ends.
func (t *Tool) StartProbe(registered []settings.Executable) {
	if t.sandboxer != nil {
		t.queueProbe(registered)
	}
}

// WatchProbeState is a current-on-subscribe receiver of the probe's state, one
// value per start and per end; close it when done.
func (t *Tool) WatchProbeState() *watch.Receiver[ProbeState] {
	return t.probe.hub.Receiver()
}

// queueProbe joins the probe queued behind the running one, starting it when
// none runs, and answers it.
func (t *Tool) queueProbe(registered []settings.Executable) *probeBatch {
	p := t.probe
	p.mu.Lock()
	defer p.mu.Unlock()
	batch := p.queued
	if batch == nil {
		batch = &probeBatch{done: make(chan struct{})}
		p.queued = batch
	}
	batch.registered = registered
	batch.asked++
	if !p.running {
		t.startQueuedProbe()
	}
	return batch
}

// startQueuedProbe runs the queued probe on a goroutine of its own, and the
// one queued behind it once it ends. Called under mu.
func (t *Tool) startQueuedProbe() {
	p := t.probe
	batch := p.queued
	p.queued, p.running = nil, true
	p.started++
	p.publish()
	p.wg.Go(func() {
		batch.reports = t.runProbe(p.ctx, batch.registered)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.last, p.running = batch.reports, false
		close(batch.done)
		if p.queued != nil {
			t.startQueuedProbe()
		} else {
			p.publish()
		}
	})
}

// Close ends a running probe and console command and waits for them, then
// ends every watcher.
func (t *Tool) Close() error {
	p := t.probe
	p.stop()
	p.wg.Wait()
	p.hub.Close()
	t.console.close()
	return nil
}

// LastProbe is the last probe's report; nil before the first.
func (t *Tool) LastProbe() []ExecutableReport {
	t.probe.mu.Lock()
	defer t.probe.mu.Unlock()
	return slices.Clone(t.probe.last)
}

// Report is ReportOver the last probe.
func (t *Tool) Report(registered []settings.Executable) []ExecutableReport {
	return ReportOver(t.LastProbe(), registered)
}

// ReportOver is the tool list as the probe that answered last found each: a
// tool it ran with the same invocation answers that report, and any other is
// not probed yet. So a register or a remove redraws from one answer, and the
// list draws before the first probe.
func ReportOver(last []ExecutableReport, registered []settings.Executable) []ExecutableReport {
	list := executableList(registered)
	for i, unprobed := range list {
		j := slices.IndexFunc(last, func(r ExecutableReport) bool { return r.Executable == unprobed.Executable })
		if j >= 0 {
			list[i] = last[j]
		}
	}
	return list
}

// runProbe is one probe of the tool list, over the PATH a run would search
// now; once ctx ends, or the disk does not answer, each report is not probed
// and says why.
func (t *Tool) runProbe(ctx context.Context, registered []settings.Executable) []ExecutableReport {
	reports := executableList(registered)
	list, err := t.readPath(ctx)
	var path []string
	if err == nil {
		// System stats folders under the home, Never lists the other homes
		// and runPath stats each folder.
		path, err = onDisk(ctx, t.probe.diskTimeout, func() []string {
			sys := t.sandboxer.System(t.home, t.shell)
			path, _ := runPath(list.Search, sys.Files, slices.Concat(t.sandboxer.Never(t.home), t.denied), t.home)
			return path
		})
	}
	for i := range reports {
		if reports[i].Refusal != "" {
			continue
		}
		if err == nil {
			reports[i] = t.probeExecutable(ctx, reports[i], path)
		} else {
			reports[i].Error = "could not start: " + err.Error()
		}
	}
	return reports
}

// onDisk is work's answer, or errDiskTimeout past timeout, or ctx's error if
// ctx ends first. The probe reads the disk outside Kubetail's directories — the
// PATH's folders, a tool's binary — and a dead network mount
// holds a read in a syscall no context reaches, so work runs on a goroutine
// abandoned then: the probe answers, the ones queued behind it run, and Close
// does not wait on the mount.
func onDisk[T any](ctx context.Context, timeout time.Duration, work func() T) (T, error) {
	done := make(chan T, 1)
	go func() { done <- work() }()
	deadline := time.After(timeout)
	select {
	case v := <-done:
		return v, nil
	case <-deadline:
		var zero T
		return zero, errDiskTimeout
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// probingTool fills report with what one run of its invocation answered, the
// binary resolved on path and the rest of the invocation its arguments; for
// a shim, the manager's resolver runs first.
func (t *Tool) probeExecutable(ctx context.Context, report ExecutableReport, path []string) ExecutableReport {
	report.Probed, report.Error = true, ""
	found, err := onDisk(ctx, t.probe.diskTimeout, func() resolution { return resolve(report.Name, path, t.home) })
	if err != nil {
		report.Probed, report.Error = false, "could not start: "+err.Error()
		return report
	}
	if found.bin == "" {
		report.Error = notOnPath
		return report
	}
	report.Resolved, report.Shim = found.bin, found.shim
	if found.resolver != "" {
		// The manager runs under the probe's policy too, so one that cannot
		// read its own data fails like any tool.
		out := t.probeRun(ctx, []string{found.resolver, "which", report.Name})
		if out.ok {
			report.Target = firstLine(out.output)
		}
	}
	out := t.probeRun(ctx, append([]string{found.bin}, strings.Fields(report.Invocation)[1:]...))
	report.OK, report.Error = out.ok, out.err
	report.Version = cut(firstLine(out.output), maxVersion)
	return report
}

// resolution is a tool's binary on the PATH, "" for none; whether it is a
// shim; and for a shim, its manager's resolver, "" where that is not on the
// PATH either.
type resolution struct {
	bin, resolver string
	shim          bool
}

// resolve is name's resolution on path, under home.
func resolve(name string, path []string, home string) resolution {
	bin, ok := resolveExecutable(name, path)
	if !ok {
		return resolution{}
	}
	r := resolution{bin: bin}
	if manager, ok := shimManager(bin, home); ok {
		r.shim = true
		r.resolver, _ = resolveExecutable(manager, path)
	}
	return r
}

// probeResult is what one probe run answered: its output, redacted, whether
// it exited 0, and why not as Bash's first line says it.
type probeResult struct {
	output string
	ok     bool
	err    string
}

// probeRun runs argv once in the sandbox as a Workspace run with no cluster
// and no network, in a throwaway chat directory removed after it.
func (t *Tool) probeRun(ctx context.Context, argv []string) probeResult {
	dir, err := newScratchDir(t.tmpDir, "probe")
	if err != nil {
		return probeResult{err: "could not start: " + err.Error()}
	}
	defer func() {
		if err := removeUnder(string(dir)); err != nil {
			slog.Warn("could not remove a probe's directory; the next start sweeps it", "err", err)
		}
	}()
	if err := makeWorkspace(dir); err != nil { // coverage:ignore the probe's directory was just made, empty and the process's own
		return probeResult{err: "could not start: " + err.Error()}
	}
	// No cluster, no chat and no network: the folders granted always are all
	// the session holds.
	rt := tools.Runtime{Session: session.Session{Folders: t.alwaysFolders}, Dir: dir}
	workspace := tools.WorkspacePath(dir)
	// The run's build reads the disk as runProbe's PATH build does, and
	// abandons the read only when its context ends, so it gets the same bound.
	setup, cancel := context.WithTimeoutCause(ctx, t.probe.buildTimeout, errDiskTimeout)
	sandboxed, err := t.sandboxedRunFor(setup, t.sandboxer, rt, workspace, false, session.NoNetwork)
	// Read before the cancel, which sets a cause of its own; a failure of the
	// build's own keeps its error.
	if errors.Is(err, context.DeadlineExceeded) {
		err = context.Cause(setup)
	}
	cancel()
	if err != nil {
		return probeResult{err: "could not start: " + err.Error()}
	}
	defer sandboxed.end()
	res := run(ctx, spec{
		sandboxedRun: sandboxed, shell: argv[0], args: argv[1:], dir: workspace,
		capture: probeCapture, timeout: probeTimeout, killGrace: killGrace, pipeGrace: pipeGrace,
		hooks: hooks{after: t.probe.after},
	})
	out := probeResult{output: safe.Redact(res.Output), ok: !res.failed()}
	switch {
	case res.Error != "":
		out.err = "could not start: " + res.Error
	case !out.ok:
		out.err = strings.TrimSuffix(headerOf(res, probeTimeout, false), "\n")
	}
	return out
}

// firstLine is s's first line holding more than whitespace, trimmed.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// cut is s cut to n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

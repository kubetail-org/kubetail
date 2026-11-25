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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

const (
	// consoleTimeout bounds a console command, as a chat's call with no
	// timeout of its own is bounded.
	consoleTimeout = DefaultTimeout
	// consoleCapture is the most of a console command's output kept: every
	// frame of its state watch carries the whole of it.
	consoleCapture = 256 << 10
	// consoleTick is how often a running command's new output is published.
	consoleTick = 250 * time.Millisecond
	// consoleSetup bounds building a command's run, which reads the disk (a
	// network mount can hang there) and claims its cluster, before
	// consoleTimeout starts.
	consoleSetup = 15 * time.Second
	// MaxConsoleCommand is the longest command the console runs, in bytes.
	MaxConsoleCommand = 16 << 10
)

var (
	// ErrNoConsole is a console command on a machine where no sandbox
	// confines it: the console runs nothing outside the sandbox.
	ErrNoConsole = errors.New("this machine has no sandbox to run a console command in")
	// ErrConsoleBusy is a console command asked for while one runs.
	ErrConsoleBusy = errors.New("a console command is already running")
	// ErrConsoleClosed is a console command asked for once the tool closed.
	ErrConsoleClosed = errors.New("the console is closed")
	// errConsoleSetup is a run's build that passed consoleSetup.
	errConsoleSetup = errors.New("the sandbox was not ready in time")
)

// ConsoleRefusal is a console command refused for a reason the user reads.
type ConsoleRefusal struct{ Reason string }

func (r *ConsoleRefusal) Error() string { return r.Reason }

// ConsoleRequest is a command the user typed in the Sandbox console: its
// text, the cluster it reaches, "" for none, and whether it has the internet.
type ConsoleRequest struct {
	Command   string
	ClusterID apimeta.ClusterID
	Network   bool
}

// ConsoleRun is the console's last command as a watcher sees it.
type ConsoleRun struct {
	ConsoleRequest
	// Number counts the console's commands since launch, so a watcher tells
	// one run from the next: the hub keeps its latest value alone.
	Number  int
	Running bool
	// Output is stdout and stderr together, in arrival order, up to
	// consoleCapture, redacted; Discarded counts the bytes past it.
	Output    string
	Discarded int
	// ExitCode is how the command exited, set once it ended with one.
	ExitCode *int
	// Notice is the sidecar's line on a run that did not simply exit: Bash's
	// first line for a stop or an unread code, or why it could not start.
	Notice   string
	Duration time.Duration
}

// console is the Bash tool's Sandbox console: one command at a time.
type console struct {
	// ctx is what every command runs on, ended by Close; wg counts the
	// command running.
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup
	// tick paces a running command's publishes: consoleTick, or a test's.
	tick time.Duration
	// setup bounds a run's build: consoleSetup, or a test's.
	setup time.Duration

	mu     sync.Mutex
	run    *ConsoleRun        // nil before the first command
	out    []byte             // the running command's output so far
	sent   int                // how much of out the last publish read
	cancel context.CancelFunc // stops the running command
	hub    *watch.Hub[*ConsoleRun]
	tx     *watch.Sender[*ConsoleRun]
}

func newConsole() *console {
	ctx, stop := context.WithCancel(context.Background())
	hub := watch.New[*ConsoleRun](nil)
	return &console{ctx: ctx, stop: stop, tick: consoleTick, setup: consoleSetup, hub: hub, tx: hub.Sender()}
}

// publish sends a copy of the run with its output as it stands. Called under
// mu. Redact matches a secret only once its line is whole, so output a line
// may still continue (a running command's, or one the cap cut) is sent up to
// its last newline; a line the cap cut counts as discarded.
func (c *console) publish() {
	run := *c.run
	out := c.out
	if run.Running || run.Discarded > 0 {
		out = wholeLines(out)
	}
	if !run.Running {
		run.Discarded += len(c.out) - len(out)
	}
	run.Output = safe.Redact(string(out))
	c.sent = len(c.out)
	c.tx.Send(&run) //nolint:errcheck // Send never blocks, and the hub closes with the console
}

// wholeLines is b up to and with its last newline; empty when it has none.
func wholeLines(b []byte) []byte {
	return b[:bytes.LastIndexByte(b, '\n')+1]
}

// WatchConsoleState is a current-on-subscribe receiver of the console's last
// command, nil before the first; close it when done.
func (t *Tool) WatchConsoleState() *watch.Receiver[*ConsoleRun] {
	return t.console.hub.Receiver()
}

// checkCommand is nil for a command the console runs: one line or many, of
// at most MaxConsoleCommand bytes, holding more than whitespace, valid UTF-8
// and no control character but a tab or a newline.
func checkCommand(command string) error {
	switch {
	case strings.TrimSpace(command) == "":
		return &ConsoleRefusal{"Type a command to run."}
	case len(command) > MaxConsoleCommand:
		return &ConsoleRefusal{"The command is too long to run."}
	case !utf8.ValidString(command) || strings.ContainsFunc(command, func(r rune) bool {
		return unicode.IsControl(r) && r != '\t' && r != '\n'
	}):
		return &ConsoleRefusal{"The command holds a character that cannot be run."}
	}
	return nil
}

// StartConsoleCommand starts req in the sandbox and returns at once with its
// number; WatchConsoleState follows it, and StopConsoleCommand stops it. It runs as a chat's sandboxed command
// would, in a workspace of its own removed after it, reading the folders
// granted always, with req's cluster and the internet when req asks and the
// machine can give it. Nobody is there to ask, so the cluster is read alone,
// and no cloud platform is mounted. It refuses a machine whose sandbox does not
// confine, a command checkCommand refuses, the internet where the machine has
// none, and a second command while one runs.
func (t *Tool) StartConsoleCommand(req ConsoleRequest) (int, error) {
	if t.sandboxer == nil || !t.sandboxer.Confines() {
		return 0, ErrNoConsole
	}
	if err := checkCommand(req.Command); err != nil {
		return 0, err
	}
	network := session.NoNetwork
	if req.Network {
		if available, reason := t.sandboxer.NetworkStatus(); !available {
			return 0, &ConsoleRefusal{"This machine cannot give a sandboxed command the internet: " + reason + "."}
		}
		network = session.NetworkConsole
	}
	c := t.console
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.ctx.Err() != nil:
		return 0, ErrConsoleClosed
	case c.run != nil && c.run.Running:
		return 0, ErrConsoleBusy
	}
	number := 1
	if c.run != nil {
		number = c.run.Number + 1
	}
	c.run = &ConsoleRun{ConsoleRequest: req, Number: number, Running: true}
	c.out = nil
	ctx, cancel := context.WithCancel(c.ctx)
	c.cancel = cancel
	c.publish()
	c.wg.Go(func() {
		defer cancel()
		t.runConsoleCommand(ctx, req, network)
	})
	return number, nil
}

// StopConsoleCommand stops the running command, as a chat's Cancel stops a
// call: command number's, or whichever runs when number is 0. It does nothing
// when none runs, or another, so a stop sent late never ends a newer command.
func (t *Tool) StopConsoleCommand(number int) {
	c := t.console
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != nil && c.run.Running && (number == 0 || number == c.run.Number) {
		c.cancel()
	}
}

// close stops the running command and waits for it, then ends every
// watcher.
func (c *console) close() {
	// Under mu, so no command starts between StartConsoleCommand's check and
	// the wait.
	c.mu.Lock()
	c.stop()
	c.mu.Unlock()
	c.wg.Wait()
	c.hub.Close()
}

// runConsoleCommand runs req under ctx with network, publishing its output
// every tick while it runs and the whole run once it ends.
func (t *Tool) runConsoleCommand(ctx context.Context, req ConsoleRequest, network session.Network) {
	c := t.console
	start := time.Now()
	// The ticker is waited out, so the run's end is the last thing published.
	ended := make(chan struct{})
	var ticker sync.WaitGroup
	ticker.Go(func() { c.publishEvery(c.tick, ended) })
	res := t.consoleResult(ctx, req, network)
	close(ended)
	ticker.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	run := c.run
	run.Running, run.Discarded, run.Duration = false, res.Discarded, time.Since(start)
	switch {
	case res.Error != "" && ctx.Err() != nil:
		// Stopped before the command started.
		run.Notice = "Command cancelled"
	case res.Error != "":
		run.Notice = "could not start: " + safe.String(res.Error)
	default:
		if res.CodeUnknown || res.Stop != stopNone {
			run.Notice = strings.TrimSuffix(headerOf(res, consoleTimeout, network != session.NoNetwork), "\n")
		}
		if !res.CodeUnknown {
			run.ExitCode = &res.ExitCode
		}
	}
	c.publish()
}

// publishEvery publishes the running command every tick until ended closes.
func (c *console) publishEvery(tick time.Duration, ended <-chan struct{}) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			if len(c.out) != c.sent {
				c.publish()
			}
			c.mu.Unlock()
		case <-ended:
			return
		}
	}
}

// consoleResult runs req once in a throwaway chat directory, removed after
// it, handing its output to the console as it comes.
func (t *Tool) consoleResult(ctx context.Context, req ConsoleRequest, network session.Network) result {
	dir, err := newScratchDir(t.tmpDir, "console")
	if err != nil {
		return result{Error: err.Error()}
	}
	defer func() {
		if err := removeUnder(string(dir)); err != nil {
			slog.Warn("could not remove a console command's directory; the next start sweeps it", "err", err)
		}
	}()
	if err := makeWorkspace(dir); err != nil { // coverage:ignore the directory was just made, empty and the process's own
		return result{Error: err.Error()}
	}
	// No chat and nobody to ask: the folders granted always are all the
	// session holds, and the run's grants refuse every change.
	rt := tools.Runtime{
		ClusterID: req.ClusterID,
		Dir:       dir,
		Session:   session.Session{Kind: session.Console, Folders: t.alwaysFolders},
	}
	workspace := tools.WorkspacePath(dir)
	setup, cancel := context.WithTimeoutCause(ctx, t.console.setup, errConsoleSetup)
	sandboxed, err := t.sandboxedRunFor(setup, t.sandboxer, rt, workspace, false, network)
	// Read before the cancel, which sets a cause of its own; a failure of the
	// build's own keeps its error.
	if errors.Is(err, context.DeadlineExceeded) {
		err = context.Cause(setup)
	}
	cancel()
	if err != nil {
		return result{Error: err.Error()}
	}
	defer sandboxed.end()
	return run(ctx, spec{
		sandboxedRun: sandboxed, shell: t.shell, dir: workspace,
		command: wrapper(t.kind, "", req.Command),
		capture: consoleCapture, timeout: consoleTimeout, killGrace: killGrace, pipeGrace: pipeGrace,
		hooks:    hooks{after: t.after},
		onOutput: t.console.write,
	})
}

// write keeps a piece of the running command's output.
func (c *console) write(p []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = append(c.out, p...)
}

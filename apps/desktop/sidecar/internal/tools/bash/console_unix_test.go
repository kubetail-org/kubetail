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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// consoleTool is a tool over a fake sandbox that confines.
func consoleTool(t *testing.T) (*Tool, *fakeSandboxer) {
	t.Helper()
	tl := tool(t)
	fake := &fakeSandboxer{confines: true}
	tl.sandboxer = fake
	t.Cleanup(func() { _ = tl.Close() })
	return tl, fake
}

// consoleEnd starts req and answers the run once it ends, every frame
// before it in frames.
func consoleEnd(t *testing.T, tl *Tool, req ConsoleRequest) (end ConsoleRun, frames []ConsoleRun) {
	t.Helper()
	rx := tl.WatchConsoleState()
	defer rx.Close()
	_, err := tl.StartConsoleCommand(req)
	require.NoError(t, err)
	for {
		run := testutil.Recv(t, rx.Chan(), "a console frame")
		if run == nil {
			continue
		}
		frames = append(frames, *run)
		if !run.Running {
			return *run, frames
		}
	}
}

// A command runs in the sandbox and ends with its output, its exit code and
// how long it ran; its first frame names it, running.
func TestAConsoleCommandRunsInTheSandbox(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)

	end, frames := consoleEnd(t, tl, ConsoleRequest{Command: "echo out; echo err >&2; exit 3"})

	assert.Equal(t, ConsoleRun{ConsoleRequest: ConsoleRequest{Command: "echo out; echo err >&2; exit 3"}, Number: 1, Running: true}, frames[0])
	assert.Equal(t, "out\nerr\n", end.Output)
	require.NotNil(t, end.ExitCode)
	assert.Equal(t, 3, *end.ExitCode)
	assert.Empty(t, end.Notice)
	assert.Positive(t, end.Duration)
	require.Len(t, fake.seen(), 1)
	assert.False(t, fake.seen()[0].Policy.Network.Internet)
	assert.Empty(t, fake.seen()[0].Policy.Network.Relays, "no cluster")
}

// Each command counts on from the last, and starts in a workspace of its own,
// which is gone once it ends.
func TestEachConsoleCommandHasAWorkspaceOfItsOwn(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)

	first, _ := consoleEnd(t, tl, ConsoleRequest{Command: "pwd"})
	second, _ := consoleEnd(t, tl, ConsoleRequest{Command: "pwd"})

	assert.Equal(t, 1, first.Number)
	assert.Equal(t, 2, second.Number)
	assert.NotEqual(t, first.Output, second.Output)
	for _, out := range []string{first.Output, second.Output} {
		dir := strings.TrimSuffix(out, "\n")
		assert.True(t, strings.HasPrefix(dir, tl.tmpDir+string(filepath.Separator)), dir)
		assert.Contains(t, dir, "-console-")
		assert.NoDirExists(t, dir)
	}
}

// A command reads the folders granted always, as a chat's does.
func TestAConsoleCommandReadsTheAlwaysFolders(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)
	granted := testutil.GrantableDir(t)
	tl.SetAlwaysFolders(func(context.Context) ([]session.Folder, error) { return []session.Folder{{Path: granted}}, nil })

	consoleEnd(t, tl, ConsoleRequest{Command: "true"})

	require.Len(t, fake.seen(), 1)
	assert.Contains(t, fake.seen()[0].Policy.Files.Read, granted)
}

// A command with Network ticked has the internet where the machine gives it.
func TestAConsoleCommandWithNetworkHasTheInternet(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "true", Network: true})

	assert.True(t, end.Network)
	require.Len(t, fake.seen(), 1)
	assert.True(t, fake.seen()[0].Policy.Network.Internet)
}

// A command on a cluster that is gone could not start, and says why.
func TestAConsoleCommandOnAGoneClusterCouldNotStart(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)
	tl.clusterSvc = fakeService{clusters: map[apimeta.ClusterID]*cluster.Cluster{}}

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo ran", ClusterID: "7"})

	assert.Nil(t, end.ExitCode)
	assert.Equal(t, "could not start: "+errClusterGone.Error(), end.Notice)
	assert.Empty(t, fake.seen())
}

// A command the sandbox could not start says why.
func TestAConsoleCommandTheSandboxRefusesCouldNotStart(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)
	fake.cmdErr = &ConsoleRefusal{"refused by the policy"}

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo ran"})

	assert.Nil(t, end.ExitCode)
	assert.Equal(t, "could not start: refused by the policy", end.Notice)
}

// A run whose build hangs, as on a dead network mount, could not start once
// its bound is out, and the console takes the next command.
func TestAConsoleCommandsSetupIsBounded(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 15*time.Second, newConsole().setup)
	tl, fake := consoleTool(t)
	tl.console.setup = 50 * time.Millisecond
	fake.onSystem, fake.holdSys = func() {}, make(chan struct{})

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo ran"})

	assert.Equal(t, "could not start: the sandbox was not ready in time", end.Notice)
	assert.Empty(t, fake.seen())

	close(fake.holdSys)
	tl.console.setup = consoleSetup // a loaded runner can take 50ms to build a run
	next, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo ran"})
	assert.Equal(t, "ran\n", next.Output)
}

// A command's directory that cannot be made could not start.
func TestAConsoleCommandWithNoDirectoryCouldNotStart(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	tl.tmpDir = filepath.Join(file, "tmp")

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo ran"})

	assert.True(t, strings.HasPrefix(end.Notice, "could not start: "), end.Notice)
	assert.Empty(t, fake.seen())
}

// A command's directory that cannot be removed is left for the next start's
// sweep, and the command still ends.
func TestAConsoleDirectoryThatCannotBeRemovedIsLogged(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	if os.Getuid() == 0 {
		t.Skip("root removes any folder")
	}
	tl, _ := consoleTool(t)
	t.Cleanup(func() { _ = os.Chmod(tl.tmpDir, 0o700) })
	logs := testutil.CaptureLogs(t)

	// The workspace is one folder under the tmp directory the removal opens.
	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: `/bin/chmod 0 "${PWD%/*/*}"`})

	require.NotNil(t, end.ExitCode)
	assert.Zero(t, *end.ExitCode, end.Output)
	assert.Contains(t, logs.String(), "could not remove a console command's directory")
}

// Output reaches a watcher while the command still runs, and a second
// command is refused meanwhile. A stop ends it as a chat's Cancel does.
func TestARunningConsoleCommandStreamsAndStops(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)
	tl.console.tick = time.Millisecond
	h := newHolder(t)
	rx := tl.WatchConsoleState()
	defer rx.Close()

	number, err := tl.StartConsoleCommand(ConsoleRequest{Command: "echo first; printf partial; " + h.command("; wait")})
	require.NoError(t, err)
	h.awaitStarted(t)
	for {
		run := testutil.Recv(t, rx.Chan(), "the first line")
		if run != nil && run.Output == "first\n" {
			assert.True(t, run.Running)
			break
		}
	}
	_, err = tl.StartConsoleCommand(ConsoleRequest{Command: "echo second"})
	assert.ErrorIs(t, err, ErrConsoleBusy)

	tl.StopConsoleCommand(number)
	for {
		run := testutil.Recv(t, rx.Chan(), "the end")
		if !run.Running {
			assert.Equal(t, "Command cancelled (exit code "+strconv.Itoa(*run.ExitCode)+")", run.Notice)
			assert.Equal(t, "first\npartial", run.Output, "the last line, whole once the command ends")
			break
		}
	}
	h.awaitGone(t)
	tl.StopConsoleCommand(0) // none runs: nothing happens
}

// A stop while the command's run is still being built ends it as cancelled,
// never as a command that could not start.
func TestAConsoleCommandStoppedBeforeItStartsIsCancelled(t *testing.T) {
	t.Parallel()

	tl, fake := consoleTool(t)
	fake.hold = testutil.NewSignal()
	rx := tl.WatchConsoleState()
	defer rx.Close()

	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: "echo ran"})
	require.NoError(t, err)
	fake.hold.Wait(t, "the command's start")
	tl.StopConsoleCommand(0)

	for {
		run := testutil.Recv(t, rx.Chan(), "the end")
		if run != nil && !run.Running {
			assert.Equal(t, "Command cancelled", run.Notice)
			assert.Nil(t, run.ExitCode)
			break
		}
	}
}

// A command past its deadline is stopped and says so.
func TestAConsoleCommandIsBounded(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)
	c := newClock()
	tl.after = func(d time.Duration) <-chan time.Time {
		assert.Equal(t, consoleTimeout, d)
		return c
	}
	h := newHolder(t)
	rx := tl.WatchConsoleState()
	defer rx.Close()

	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: h.command("; wait")})
	require.NoError(t, err)
	h.awaitStarted(t)
	c.fire()
	for {
		run := testutil.Recv(t, rx.Chan(), "the end")
		if run != nil && !run.Running {
			assert.True(t, strings.HasPrefix(run.Notice, "Command timed out after 120s"), run.Notice)
			break
		}
	}
	h.awaitGone(t)
}

// Close stops a running command and waits for it, then ends every watcher.
func TestCloseEndsARunningConsoleCommand(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)
	h := newHolder(t)
	rx := tl.WatchConsoleState()
	defer rx.Close()
	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: h.command("; wait")})
	require.NoError(t, err)
	h.awaitStarted(t)

	require.NoError(t, tl.Close())

	run := tl.console.run
	assert.False(t, run.Running)
	assert.True(t, strings.HasPrefix(run.Notice, "Command cancelled"), run.Notice)
	for range rx.Chan() {
	}
	h.awaitGone(t)
}

// Output is redacted, and capped: what passes the cap is counted.
func TestConsoleOutputIsRedactedAndCapped(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "echo 'Authorization: Bearer abc.def.ghi'; head -c 300000 /dev/zero"})

	assert.NotContains(t, end.Output, "abc.def.ghi")
	// The cap cut the line of zeros, which counts whole as discarded.
	assert.Equal(t, "Authorization: [redacted]\n", end.Output)
	assert.Equal(t, 300000, end.Discarded)
}

// Under the machine's sandbox, a command cannot read a file outside what the
// sandbox opens, and its output shows the refusal.
func TestTheSandboxRefusesAConsoleCommandsRead(t *testing.T) {
	t.Parallel()

	tl, _ := consoleTool(t)
	tl.sandboxer = confining(t)
	tl.home = testutil.GrantableDir(t)
	onPath(tl)
	secret := filepath.Join(testutil.GrantableDir(t), "secret")
	require.NoError(t, os.WriteFile(secret, []byte("the secret\n"), 0o600))

	end, _ := consoleEnd(t, tl, ConsoleRequest{Command: "/bin/cat " + secret})

	require.NotNil(t, end.ExitCode, end.Notice)
	assert.NotZero(t, *end.ExitCode)
	assert.NotContains(t, end.Output, "the secret")
	assert.Contains(t, end.Output, secret, "cat names the file it could not read")
}

// Under the machine's sandbox, a command on a cluster reads it, and its
// change is refused: nobody is there to approve it.
func TestAConsoleCommandChangesNoCluster(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t)
	tl := proxyTool(t, &fakeLease{serverUID: "uid-1", conn: api.connection()})
	tl.sandboxer = confining(t)
	t.Cleanup(func() { _ = tl.Close() })

	read, _ := consoleEnd(t, tl, ConsoleRequest{Command: clientLine(), ClusterID: "7"})
	change, _ := consoleEnd(t, tl, ConsoleRequest{Command: clientLine("delete"), ClusterID: "7"})

	assert.Contains(t, read.Output, "web-1\n")
	assert.Equal(t, []string{"Bearer users-own"}, api.credentials(), "the read alone reached the cluster")
	assert.Contains(t, change.Output, "kubetail: this sandbox reads the cluster and changes nothing.")
}

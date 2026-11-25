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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// shells is every kind a snapshot is taken for.
var shells = []string{"bash", "zsh"}

// profileTool is the tool over the machine's kind, whose login shell reads rc
// from a home of the test's own. A machine without that shell skips.
func profileTool(t *testing.T, kind, rc string) *Tool {
	t.Helper()
	path, err := exec.LookPath(kind)
	if err != nil {
		t.Skip("no " + kind + " on this machine")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", path)
	t.Setenv("BASH_ENV", "")
	os.Unsetenv("ZDOTDIR")
	switch kind {
	case "bash":
		// A login bash reads .bash_profile, which sources .bashrc the way most do.
		require.NoError(t, os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(". ~/.bashrc\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte(rc), 0o600))
	case "zsh":
		require.NoError(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0o600))
	}
	tl, ok := New(Paths{ShellDir: t.TempDir(), TmpDir: t.TempDir()}, 0, nil, nil, nil, nil, Cloud{})
	require.True(t, ok)
	require.Equal(t, kind, tl.kind)
	tl.home = t.TempDir()
	return tl
}

// snapshotOf takes tl's snapshot and returns what it wrote.
func snapshotOf(t *testing.T, tl *Tool) string {
	t.Helper()
	tl.takeSnapshot(t.Context())
	require.NotEmpty(t, tl.snapshot, "no snapshot was written")
	b, err := os.ReadFile(tl.snapshot)
	require.NoError(t, err)
	return string(b)
}

const profile = `echo 'welcome to the shell'
greet() { echo "hi from $1"; }
alias ll='ls -l'
export PATH="/opt/kubetail-test/bin:$PATH"
`

// The snapshot holds the profile's functions, aliases and PATH, and none of what
// its rc files print.
func TestTheSnapshotHoldsTheProfile(t *testing.T) {
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			snap := snapshotOf(t, profileTool(t, kind, profile))
			assert.True(t, strings.Contains(snap, "greet"), "missing %q", "greet")
			assert.True(t, strings.Contains(snap, "ll="), "missing %q", "ll=")
			assert.True(t, strings.Contains(snap, "/opt/kubetail-test/bin"), "missing %q", "/opt/kubetail-test/bin")
			assert.False(t, strings.Contains(snap, "welcome"), "holds %q", "welcome")
		})
	}
}

// The snapshot's login shell runs in the sandbox: it reads the whole home, the
// denied-always list included, writes nothing but a TMPDIR under the tool's,
// reaches no network, and holds nothing the sandbox refuses to pass.
func TestTheSnapshotIsTakenInTheSandbox(t *testing.T) {
	tl := profileTool(t, "bash", profile)
	sb := &fakeSandboxer{never: []string{"/never"}}
	tl.sandboxer = sb
	tl.denied = []string{t.TempDir(), t.TempDir(), t.TempDir()}
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("AWS_PROFILE", "work")

	assert.Contains(t, snapshotOf(t, tl), "greet")

	runs := sb.seen()
	require.Len(t, runs, 1)
	p := runs[0].Policy
	assert.Empty(t, p.Always.Deny)
	assert.Equal(t, tl.denied, p.Always.Kubetail)
	assert.Equal(t, "/", p.Files.Read[0])
	require.Len(t, p.Always.Write, 1)
	assert.Equal(t, tl.tmpDir, filepath.Dir(p.Always.Write[0]))
	assert.Zero(t, p.Network)
	assert.Contains(t, runs[0].Env, "TMPDIR="+p.Always.Write[0])
	for _, kv := range runs[0].Env {
		name, _, _ := strings.Cut(kv, "=")
		assert.False(t, sandbox.Unpassable(name), name)
	}
}

// The options that say how the snapshot's own shell was started are not
// replayed: monitor above all, which would move a command's background jobs out
// of its process group. Nor are global aliases, which expand anywhere in a line.
func TestTheSnapshotLeavesOutStartupOptionsAndGlobalAliases(t *testing.T) {
	snap := snapshotOf(t, profileTool(t, "zsh", "setopt monitor\nalias -g G='| grep'\n"))
	for _, option := range []string{"interactive", "login", "monitor", "zle", "shinstdin"} {
		assert.False(t, strings.Contains(snap, "setopt "+option+"\n"), "holds %q", "setopt "+option+"\n")
	}
	assert.False(t, strings.Contains(snap, "alias -g"), "holds %q", "alias -g")
	assert.False(t, strings.Contains(snap, "| grep"), "holds %q", "| grep")
}

// bash's read-only options are not replayed either.
func TestTheBashSnapshotLeavesOutReadOnlyOptions(t *testing.T) {
	snap := snapshotOf(t, profileTool(t, "bash", ""))
	assert.False(t, strings.Contains(snap, "login_shell"), "holds %q", "login_shell")
	assert.False(t, strings.Contains(snap, "restricted_shell"), "holds %q", "restricted_shell")
	assert.True(t, strings.Contains(snap, "shopt -s expand_aliases\n"), "missing %q", "shopt -s expand_aliases\n")
}

// The dump runs after the profile, so the profile's aliases and functions apply
// to its text. Each shadowing name here prints broken2 when it runs; the
// definitions only ever show the unexpanded $((1+1)).
func TestTheDumpIgnoresTheProfilesNames(t *testing.T) {
	shadows := map[string]string{
		"bash": `alias builtin='echo broken$((1+1)); true '
alias printf='echo broken$((1+1))'
printf() { echo broken$((1+1)); }
alias() { echo broken$((1+1)); }
declare() { echo broken$((1+1)); }
shopt() { echo broken$((1+1)); }
`,
		"zsh": `alias builtin='echo broken$((1+1)); true '
alias -g o='broken$((1+1))'
alias -g l='broken$((1+1))'
printf() { echo broken$((1+1)); }
alias() { echo broken$((1+1)); }
setopt() { echo broken$((1+1)); }
`,
	}
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			snap := snapshotOf(t, profileTool(t, kind, profile+shadows[kind]))
			assert.True(t, strings.Contains(snap, "greet"), "missing greet")
			assert.True(t, strings.Contains(snap, "/opt/kubetail-test/bin"), "missing the PATH entry")
			assert.False(t, strings.Contains(snap, "broken2"), "a shadowing name ran inside the dump")
		})
	}
}

// The snapshot runs before every command, so it is the owner's alone, and
// read-only: nothing appends to it by accident.
func TestTheSnapshotIsReadOnly(t *testing.T) {
	tl := profileTool(t, "bash", profile)
	snapshotOf(t, tl)
	info, err := os.Stat(tl.snapshot)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
	info, err = os.Stat(filepath.Dir(tl.snapshot))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// stand is a process standing in for one the shims protect: a sleep the test
// owns, so a shim that fails to refuse kills it rather than the test.
func stand(t *testing.T, seconds string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", seconds)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// pkillEchoes is whether this machine's pkill takes procps' -e. BSD pkill
// (macOS) rejects it as a usage error, exit 2, where no match is exit 1.
func pkillEchoes() bool {
	var exit *exec.ExitError
	err := exec.Command("pkill", "-e", "-f", "kubetail-matches-no-process").Run()
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// alive is whether cmd's process is still running.
func alive(cmd *exec.Cmd) bool {
	return cmd.Process.Signal(syscall.Signal(0)) == nil
}

// kill and pkill refuse the sidecar and the host, whatever the profile aliased
// them to, and pass everything else through.
func TestTheKillShimsRefuseKubetail(t *testing.T) {
	rt := testRuntime(t)
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("no pgrep on this machine")
	}
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			tl := profileTool(t, kind, "alias kill='kill -9'\nalias pkill='pkill -9'\n")
			snapshotOf(t, tl)
			sidecar, host := stand(t, "31337"), stand(t, "31338")
			tl.env = []string{
				"KUBETAIL=1",
				"KUBETAIL_SIDECAR_PID=" + strconv.Itoa(sidecar.Process.Pid),
				"KUBETAIL_DESKTOP_PID=" + strconv.Itoa(host.Process.Pid),
			}
			for _, line := range []string{
				"kill $KUBETAIL_SIDECAR_PID",
				"kill -9 $KUBETAIL_SIDECAR_PID",
				"kill -s KILL $KUBETAIL_DESKTOP_PID",
				"pkill -f 'sleep 31337'",
				"pkill -9 -f 'sleep 31338'",
				"pkill --signal KILL -f 'sleep 3133[78]'",
				"pkill -e -f 'sleep 31337'",
				"pkill -q 1 -f 'sleep 31338'",
			} {
				text, isError := tl.Run(t.Context(), rt, command(line))
				assert.True(t, isError, line)
				assert.Equal(t, "Exit code 1\nrefused: that would stop Kubetail\n", text, line)
			}
			// pgrep rejects a cluster holding pkill's -e, and cannot answer.
			text, isError := tl.Run(t.Context(), rt, command("pkill -ef 'sleep 31337'"))
			assert.True(t, isError)
			assert.Equal(t, "Exit code 1\nrefused: pgrep could not check what that would stop\n", text)
			assert.True(t, alive(sidecar), "the sidecar's stand-in was killed")
			assert.True(t, alive(host), "the host's stand-in was killed")

			// Each Wait blocks until the command stops other, so a failed one
			// ends the subtest and leaves other to stand's cleanup.
			other := stand(t, "31339")
			text, isError = tl.Run(t.Context(), rt, command("kill "+strconv.Itoa(other.Process.Pid)))
			require.False(t, isError, text)
			_ = other.Wait()
			other = stand(t, "31339")
			// The bracket keeps the pattern from matching this command's own line.
			text, isError = tl.Run(t.Context(), rt, command("pkill -f 'sleep 3133[9]'"))
			require.False(t, isError, text)
			_ = other.Wait()
			if pkillEchoes() {
				other = stand(t, "31339")
				text, isError = tl.Run(t.Context(), rt, command("pkill -e -f 'sleep 3133[9]'"))
				require.False(t, isError, text)
				_ = other.Wait()
			}

			// A job spec is the builtin's alone; zsh's command kill is /bin/kill.
			// %% rather than %1: under zsh's eval the job is not number one.
			// macOS's bash 3.2 reports the job's end on stderr before the status.
			text, _ = tl.Run(t.Context(), rt, command("sleep 60 & kill %%; wait $!; echo $?"))
			assert.True(t, strings.HasSuffix(text, "143\n"), "kill %% stops the job: %q", text)

			// With no pgrep to ask, pkill is left to fail as pkill does.
			text, _ = tl.Run(t.Context(), rt, command("PATH=/nowhere; pkill -f x; echo $?"))
			assert.NotContains(t, text, "refused")
			assert.True(t, strings.HasSuffix(text, "127\n"), text)
		})
	}
}

// A profile under kshoptionprint, where setopt lists every option beside on or
// off, still replays as the options it had: a command runs and says so.
func TestTheZshSnapshotReadsOptionsUnderKshOptionPrint(t *testing.T) {
	rt := testRuntime(t)
	tl := profileTool(t, "zsh", "setopt kshoptionprint\n")
	snap := snapshotOf(t, tl)
	assert.NotContains(t, snap, " off\n")
	text, isError := tl.Run(t.Context(), rt, command("echo ran"))
	assert.False(t, isError, text)
	assert.Equal(t, "ran\n", text)
}

// A profile that hangs, or prints past the cap, costs the snapshot and nothing
// else: the file holds the shims alone and a command runs. The shrunk timeout is
// the assertion, not a wait: only it can end the hanging shell.
func TestAFailedSnapshotOffersTheToolAnyway(t *testing.T) {
	rt := testRuntime(t)
	for name, rc := range map[string]string{
		"hangs":  "sleep 300\n",
		"floods": "awk 'BEGIN { printf \"%2000s\", \"\" }'\nsleep 300\n",
	} {
		t.Run(name, func(t *testing.T) {
			tl := profileTool(t, "bash", rc)
			tl.snapTimeout, tl.snapLimit = 250*time.Millisecond, 1000
			snap := snapshotOf(t, tl)
			assert.Equal(t, shims, snap)
			text, isError := tl.Run(t.Context(), rt, command("echo ok"))
			assert.False(t, isError)
			assert.Equal(t, "ok\n", text)
		})
	}
}

// A command runs with the profile's functions, aliases and PATH, as it would in
// the user's terminal.
func TestACommandSeesTheProfile(t *testing.T) {
	rt := testRuntime(t)
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			tl := profileTool(t, kind, profile)
			snapshotOf(t, tl)
			text, isError := tl.Run(t.Context(), rt, command(`greet you; ll -d /; echo "$PATH"`))
			require.False(t, isError, text)
			lines := strings.Split(text, "\n")
			assert.Equal(t, "hi from you", lines[0])
			assert.True(t, strings.HasPrefix(lines[1], "d"), "ll is ls -l: %q", lines[1])
			assert.True(t, strings.HasPrefix(lines[2], "/opt/kubetail-test/bin:"), lines[2])
		})
	}
}

// A relative PATH entry means the directory the profile ended in, not the home a
// command runs in, and an empty one is dropped.
func TestTheSnapshotResolvesARelativePathEntry(t *testing.T) {
	rt := testRuntime(t)
	rc := `mkdir -p ~/tools/bin
printf '#!/bin/sh\necho found\n' > ~/tools/bin/kubetail-tool
chmod +x ~/tools/bin/kubetail-tool
cd ~/tools
export PATH="bin:$PATH:"
`
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			tl := profileTool(t, kind, rc)
			snapshotOf(t, tl)
			text, isError := tl.Run(t.Context(), rt, command(`kubetail-tool; echo "$PATH"`))
			require.False(t, isError, text)
			lines := strings.Split(text, "\n")
			assert.Equal(t, "found", lines[0])
			assert.True(t, strings.HasSuffix(strings.SplitN(lines[1], ":", 2)[0], "/tools/bin"), lines[1])
			assert.False(t, strings.HasSuffix(lines[1], ":"), lines[1])
			assert.False(t, strings.Contains(lines[1], "::"), lines[1])
		})
	}
}

// A command's background job still dies with its group under a zsh profile: the
// snapshot never replays monitor, which would move the job into a group of its
// own. zsh refuses monitor without a terminal, so this guards a run that has
// one.
func TestRunKillsWhatACommandLeftBehindUnderZsh(t *testing.T) {
	rt := testRuntime(t)
	tl := profileTool(t, "zsh", "setopt monitor\n")
	snapshotOf(t, tl)
	h := newHolder(t)
	text, isError := tl.Run(t.Context(), rt, command(h.command("")))
	assert.False(t, isError, text)
	h.awaitStarted(t)
	h.awaitGone(t)
}

// heldLaunch stands in for the login shell: it answers with dump once release
// closes, or fails when its context ends first.
func heldLaunch(release <-chan struct{}, dump string) func(context.Context) ([]byte, string, int) {
	return func(ctx context.Context) ([]byte, string, int) {
		select {
		case <-release:
			return []byte(snapshotStart + dump + snapshotEnd), "", 0
		case <-ctx.Done():
			return nil, "timeout", -1
		}
	}
}

// A call that arrives while the snapshot is being taken waits for it, and runs
// with the profile; a cancelled call stops waiting. The hook fires as a call
// begins to wait, which is when the test lets the snapshot land.
func TestACallWaitsForTheSnapshot(t *testing.T) {
	rt := testRuntime(t)
	tl := tool(t)
	release := make(chan struct{})
	tl.launch = heldLaunch(release, "greet() { echo hi; }\n")
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })

	ctx, cancel := context.WithCancel(t.Context())
	tl.onSnapshotWait = cancel
	_, isError := tl.Run(ctx, rt, command("true"))
	assert.True(t, isError, "a cancelled call stops waiting")

	tl.onSnapshotWait = func() { close(release) }
	text, isError := tl.Run(t.Context(), rt, command("greet"))
	assert.False(t, isError, text)
	assert.Equal(t, "hi\n", text)

	tl.onSnapshotWait = func() { t.Error("a call waited on a snapshot already taken") }
	text, isError = tl.Run(t.Context(), rt, command("greet"))
	assert.False(t, isError, text)
	assert.Equal(t, "hi\n", text)
}

// A dump that arrives without its frame is no dump: the file holds the shims
// alone.
func TestAnUnframedDumpLeavesTheShimsAlone(t *testing.T) {
	tl := tool(t)
	tl.launch = func(context.Context) ([]byte, string, int) {
		return []byte("greet() { echo hi; }\n"), "", 0
	}
	assert.Equal(t, shims, snapshotOf(t, tl))
}

// A relative dir the working directory cannot resolve is an error, since a
// command runs in home and a relative path would not find the file. A removed
// working directory is the one way to make that resolution fail.
func TestWriteSnapshotNeedsAnAbsolutePath(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(gone, 0o700))
	t.Chdir(gone)
	require.NoError(t, os.Remove(gone))
	_, err := writeSnapshot("shell", "f() { :; }\n")
	assert.Error(t, err)
}

// A snapshot that cannot be written is left out, and a command runs without it.
func TestASnapshotThatCannotBeWrittenIsLeftOut(t *testing.T) {
	rt := testRuntime(t)
	tl := tool(t)
	tl.scripts = filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(tl.scripts, nil, 0o600))
	tl.launch = heldLaunch(closed(), "greet() { echo hi; }\n")
	tl.takeSnapshot(t.Context())
	assert.Empty(t, tl.snapshot)
	text, isError := tl.Run(t.Context(), rt, command("echo ok"))
	assert.False(t, isError)
	assert.Equal(t, "ok\n", text)
}

func closed() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// A tool whose snapshot was never started runs at once.
func TestACallDoesNotWaitForASnapshotNeverStarted(t *testing.T) {
	rt := testRuntime(t)
	tl := tool(t)
	tl.onSnapshotWait = func() { t.Error("a call waited on a snapshot nobody started") }
	text, isError := tl.Run(t.Context(), rt, command("echo ok"))
	assert.False(t, isError)
	assert.Equal(t, "ok\n", text)
}

// Stopping the snapshot ends the shell and writes nothing.
func TestStoppingTheSnapshotWritesNothing(t *testing.T) {
	tl := tool(t)
	tl.launch = heldLaunch(make(chan struct{}), "")
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	require.NoError(t, stop(t.Context()))
	require.NoError(t, stop(t.Context()), "stop is idempotent")
	assert.Empty(t, tl.snapshot)
	_, err = os.Stat(filepath.Join(tl.scripts, "snapshot.sh"))
	assert.True(t, os.IsNotExist(err))
}

// Stopping the snapshot kills and reaps a login shell stuck in its rc files
// before it returns, so no shell outlives the sidecar. The rc names its own pid
// before it hangs, and the test waits for that file rather than for time.
func TestStoppingTheSnapshotReapsTheShell(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "shell.pid")
	tl := profileTool(t, "bash", "echo $$ > '"+pidFile+"'\nsleep 300\n")
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	var pid int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	}, testutil.Timeout, 10*time.Millisecond)

	require.NoError(t, stop(t.Context()))
	assert.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH, "the shell outlived the stop")
}

// A function the profile defined under extglob parses only with extglob on, and
// a snapshot that fails to parse stops sourcing there, losing everything after
// it: the options are restored ahead of the functions.
func TestAnExtglobFunctionSurvivesTheSnapshot(t *testing.T) {
	rt := testRuntime(t)
	rc := "shopt -s extglob\nisnum() { case \"$1\" in +([0-9])) echo num ;; *) echo not ;; esac; }\n" + profile
	tl := profileTool(t, "bash", rc)
	snapshotOf(t, tl)
	text, isError := tl.Run(t.Context(), rt, command(`isnum 42; greet you; type kill | head -1`))
	assert.False(t, isError, text)
	assert.Equal(t, "num\nhi from you\nkill is a function\n", text)
}

// The snapshot is written in the shell directory the tool is handed.
func TestTheSnapshotIsInItsShellDirectory(t *testing.T) {
	t.Setenv("SHELL", "")
	shell := filepath.Join(t.TempDir(), "shell")
	tl, ok := New(Paths{ShellDir: shell}, 0, nil, nil, nil, nil, Cloud{})
	if !ok {
		t.Skip("no bash found on this machine")
	}
	tl.home = t.TempDir()
	tl.takeSnapshot(t.Context())
	assert.FileExists(t, filepath.Join(shell, "snapshot.sh"))
}

// A sandboxed run sources no snapshot and its policy does not read one: a
// function the profile defined is not there.
func TestASandboxedRunSourcesNoSnapshot(t *testing.T) {
	rt := testRuntime(t)
	tl := tool(t)
	tl.launch = heldLaunch(closed(), "greet() { echo hi; }\n")
	tl.takeSnapshot(t.Context())
	require.NotEmpty(t, tl.snapshot)
	fake := &fakeSandboxer{}
	tl.sandboxer = fake

	text, isError := tl.Run(t.Context(), rt, command("type greet >/dev/null 2>&1 && echo found || echo none"))

	require.False(t, isError, text)
	assert.Equal(t, "none\n", text)
	runs := fake.seen()
	require.Len(t, runs, 1)
	assert.NotContains(t, runs[0].Args[1], tl.snapshot)
	assert.NotContains(t, runs[0].Policy.Always.Read, tl.snapshot)
}

// A sandboxed call and a sandboxed background call run while the snapshot is
// still being taken: neither waits for it.
func TestASandboxedRunDoesNotWaitForTheSnapshot(t *testing.T) {
	rt := testRuntime(t)
	tl := tool(t)
	tl.launch = heldLaunch(make(chan struct{}), "")
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	tl.sandboxer = &fakeSandboxer{}
	tl.onSnapshotWait = func() { t.Error("a sandboxed run waited on the snapshot") }

	text, isError := tl.Run(t.Context(), rt, command("echo ran"))
	require.False(t, isError, text)
	assert.Equal(t, "ran\n", text)

	tasks := newFakeTasks(t)
	text, isError = tl.Run(t.Context(), tools.Runtime{Dir: rt.Dir, Tasks: tasks}, background("true"))
	require.False(t, isError, text)
	require.Len(t, tasks.started, 1)
	assert.Equal(t, 0, tasks.started[0].Wait().Code)
}

// On a machine with a sandbox no snapshot is taken at start. The first runs
// outside it take it once, and every one of them waits for it.
func TestTheSnapshotIsTakenOnTheFirstRunOutside(t *testing.T) {
	rt := testRuntime(t)
	rt.Session.Outside = true
	tl := tool(t)
	tl.sandboxer = &fakeSandboxer{}
	release := make(chan struct{})
	var launches atomic.Int32
	held := heldLaunch(release, "greet() { echo hi; }\n")
	tl.launch = func(ctx context.Context) ([]byte, string, int) {
		launches.Add(1)
		return held(ctx)
	}
	waiting := make(chan struct{}, 2)
	tl.onSnapshotWait = func() { waiting <- struct{}{} }
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stop(context.Background()) })
	assert.Nil(t, tl.ready, "no snapshot is started at start")

	texts := make(chan string, 2)
	for range 2 {
		go func() {
			text, _ := tl.Run(t.Context(), rt, command("greet"))
			texts <- text
		}()
	}
	testutil.Recv(t, waiting, "the first run to wait")
	testutil.Recv(t, waiting, "the second run to wait")
	close(release)

	assert.Equal(t, "hi\n", testutil.Recv(t, texts, "the first run"))
	assert.Equal(t, "hi\n", testutil.Recv(t, texts, "the second run"))
	assert.Equal(t, int32(1), launches.Load())
}

// The stop cancels a snapshot a run outside started and returns once its
// shell is reaped; with none started it returns at once.
func TestTheStopReapsASnapshotStartedLate(t *testing.T) {
	rt := testRuntime(t)
	rt.Session.Outside = true
	tl := tool(t)
	tl.sandboxer = &fakeSandboxer{}
	launched := testutil.NewSignal()
	var reaped atomic.Bool
	tl.launch = func(ctx context.Context) ([]byte, string, int) {
		launched.Fire()
		<-ctx.Done()
		reaped.Store(true)
		return nil, "timeout", -1
	}
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = tl.Run(t.Context(), rt, command("true"))
	}()
	launched.Wait(t, "the snapshot to start")

	require.NoError(t, stop(t.Context()))
	assert.True(t, reaped.Load(), "the stop returned before the shell was reaped")
	testutil.WaitClosed(t, done, "the run")

	idle := tool(t)
	idle.sandboxer = &fakeSandboxer{}
	stop, err = idle.StartSnapshot(t.Context())
	require.NoError(t, err)
	assert.NoError(t, stop(t.Context()))
}

// Once the stop has returned, a first run outside the sandbox starts no
// shell and runs without a snapshot.
func TestNoSnapshotStartsAfterTheStop(t *testing.T) {
	rt := testRuntime(t)
	rt.Session.Outside = true
	tl := tool(t)
	tl.sandboxer = &fakeSandboxer{}
	tl.launch = func(context.Context) ([]byte, string, int) {
		t.Error("a login shell started after the stop")
		return nil, "timeout", -1
	}
	stop, err := tl.StartSnapshot(t.Context())
	require.NoError(t, err)
	require.NoError(t, stop(t.Context()))

	text, isError := tl.Run(t.Context(), rt, command("echo ok"))

	require.False(t, isError, text)
	assert.Equal(t, "ok\n", text)
}

// The snapshot's shell sees the process's environment, as a command outside the
// sandbox does: only the PATH resolution's is scrubbed.
func TestTheSnapshotKeepsTheProcessEnvironment(t *testing.T) {
	for _, kind := range shells {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("KUBETAIL_TEST_PROCESS_VAR", "kept")
			tl := profileTool(t, kind, `printf %s "$KUBETAIL_TEST_PROCESS_VAR" > ~/seen`+"\n")
			snapshotOf(t, tl)
			seen, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "seen"))
			require.NoError(t, err)
			assert.Equal(t, "kept", string(seen))
		})
	}
}

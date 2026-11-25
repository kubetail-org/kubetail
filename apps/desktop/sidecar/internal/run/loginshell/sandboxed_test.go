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

//go:build unix

package loginshell

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempDirIn makes each run's TMPDIR under root, removed by its cleanup.
func tempDirIn(root string) TempDir {
	return func() (string, func(), error) {
		dir, err := os.MkdirTemp(root, "1-*")
		return dir, func() { _ = os.RemoveAll(dir) }, err
	}
}

// With no sandbox the shell is a plain command in the home, with the
// environment as given and never nil, and Launch leaves the command's
// environment and directory as start set them.
func TestWithoutASandboxTheShellRunsAsBefore(t *testing.T) {
	start := In(nil, nil, nil, nil)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cmd, cleanup, err := start(t.Context(), "/bin/sh", []string{"-c", "true"}, []string{"GIVEN=yes"})
	require.NoError(t, err)
	cleanup()
	assert.Equal(t, []string{"/bin/sh", "-c", "true"}, cmd.Args)
	assert.Equal(t, home, cmd.Dir)
	assert.Equal(t, []string{"GIVEN=yes"}, cmd.Env)
	assert.Nil(t, cmd.SysProcAttr)

	cmd, cleanup, err = start(t.Context(), "/bin/sh", nil, nil)
	require.NoError(t, err)
	cleanup()
	assert.NotNil(t, cmd.Env)
	assert.Empty(t, cmd.Env)

	dir := t.TempDir()
	cleaned := false
	own := func(_ context.Context, name string, args, _ []string) (*exec.Cmd, func(), error) {
		c := exec.Command(name, args...)
		c.Dir, c.Env = dir, []string{"OWN=yes"}
		return c, func() { cleaned = true }, nil
	}
	shell := writeScript(t, "shell", "#!/bin/sh\nprintf '%s|%s|%s|' \"$(/bin/pwd)\" \"$OWN\" \"$GIVEN\"\nprintf END\nsleep 300\n")
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	out, f := Launch(ctx, own, shell, nil, []string{"GIVEN=yes"}, maxOutputBytes, func(buf []byte, _ int) bool {
		return bytes.HasSuffix(buf, []byte("END"))
	})
	require.Nil(t, f)
	assert.Equal(t, resolvedDir(t, dir)+"|yes||END", string(out))
	assert.True(t, cleaned, "Launch cleans up once the shell has exited")
}

// resolvedDir is dir as /bin/pwd prints it: macOS's temporary directories sit
// under a link.
func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return r
}

// fakeCommander records each Run it is handed and builds it unconfined, or
// answers err, or waits for ctx to end and answers its error.
type fakeCommander struct {
	err     error
	waitCtx bool
	runs    []sandbox.Run
}

func (f *fakeCommander) Command(ctx context.Context, r sandbox.Run) (*exec.Cmd, error) {
	f.runs = append(f.runs, r)
	if f.waitCtx {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	cmd := exec.CommandContext(ctx, r.Shell, r.Args...)
	cmd.Dir, cmd.Env = r.Dir, r.Env
	return cmd, nil
}

// launchMarker launches a shell through start that leaves a file behind when
// it runs, and answers the fault and whether the file is there.
func launchMarker(t *testing.T, ctx context.Context, start Start) (*Fault, bool) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "ran")
	shell := writeScript(t, "shell", "#!/bin/sh\n: > '"+marker+"'\necho END\n")
	_, f := Launch(ctx, start, shell, nil, nil, maxOutputBytes, func(buf []byte, _ int) bool {
		return bytes.Contains(buf, []byte("END"))
	})
	_, err := os.Stat(marker)
	return f, err == nil
}

// A policy the sandbox refuses fails the run: the shell never starts.
func TestAPolicyTheSandboxRefusesIsAFault(t *testing.T) {
	start := In(&fakeCommander{err: errors.New("refused")}, nil, nil, tempDirIn(t.TempDir()))

	f, ran := launchMarker(t, t.Context(), start)

	require.NotNil(t, f)
	assert.Equal(t, reasonSandboxRefused, f.Reason)
	assert.Equal(t, -1, f.ExitCode)
	assert.False(t, ran)
}

// A TMPDIR that cannot be made fails the run before the sandbox is asked.
func TestAScratchThatCannotBeMadeIsAFault(t *testing.T) {
	sb := &fakeCommander{}
	start := In(sb, nil, nil, tempDirIn(filepath.Join(t.TempDir(), "gone")))

	f, ran := launchMarker(t, t.Context(), start)

	require.NotNil(t, f)
	assert.Equal(t, reasonNoScratch, f.Reason)
	assert.False(t, ran)
	assert.Empty(t, sb.runs)
}

// A sandbox that answers the context's error once it has ended is a shell
// that ran out of time, not a refused policy.
func TestAStartCutShortIsATimeout(t *testing.T) {
	start := In(&fakeCommander{waitCtx: true}, nil, nil, tempDirIn(t.TempDir()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	f, ran := launchMarker(t, ctx, start)

	require.NotNil(t, f)
	assert.Equal(t, reasonTimeout, f.Reason)
	assert.False(t, ran)
}

// With no home to start in, the shell starts at the root. Serial: it unsets
// this process's HOME.
func TestAShellWithNoHomeStartsAtTheRoot(t *testing.T) {
	t.Setenv("HOME", "")
	sb := &fakeCommander{}
	start := In(sb, nil, nil, tempDirIn(t.TempDir()))

	_, cleanup, err := start(t.Context(), "/bin/sh", nil, nil)
	require.NoError(t, err)
	cleanup()
	require.Len(t, sb.runs, 1)
	assert.Equal(t, "/", sb.runs[0].Dir)
}

func TestMain(m *testing.M) {
	// A sandboxed run's forwarder and shell launcher are this binary.
	if code, ok := sandbox.Main(os.Args); ok {
		testutil.ExitHelper(code)
	}
	os.Exit(m.Run())
}

// confining is this machine's sandbox, or testutil.RequireSandbox's verdict.
func confining(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	sb, status, err := sandbox.Probe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sb.Close() })
	if sb == nil || !sb.Confines() {
		testutil.RequireSandbox(t, "no sandbox: "+status.Reason)
	}
	return sb
}

// fixture is a fake home and Kubetail's three directories, outside /tmp: every
// Linux run mounts a private /tmp, so a fixture under it would be invisible
// inside the run whatever the policy said.
type fixture struct {
	root, home, tmpDir string
	kubetail           []string // data, cache, runtime
}

// newFixture makes the fixture, sets HOME to its home and runs the login
// shell as /bin/sh, which as a login shell reads ~/.profile.
func newFixture(t *testing.T) fixture {
	t.Helper()
	root := testutil.GrantableDir(t)
	f := fixture{root: root, home: filepath.Join(root, "home")}
	for _, name := range []string{"data", "cache", "runtime"} {
		f.kubetail = append(f.kubetail, filepath.Join(root, name))
	}
	f.tmpDir = filepath.Join(f.kubetail[1], "tmp")
	for _, dir := range append([]string{f.home, f.tmpDir}, f.kubetail...) {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	t.Setenv("HOME", f.home)
	defer func(find func(context.Context) (string, *Fault)) { t.Cleanup(func() { findShell = find }) }(findShell)
	findShell = func(context.Context) (string, *Fault) { return "/bin/sh", nil }
	return f
}

// write writes contents to name under the fixture's root, making its folder.
func (f fixture) write(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

// profile makes script the home's ~/.profile.
func (f fixture) profile(t *testing.T, script string) {
	t.Helper()
	f.write(t, "home/.profile", script)
}

// start is In over sb with deny and the fixture's directories.
func (f fixture) start(sb Commander, deny []string) Start {
	return In(sb, deny, f.kubetail, tempDirIn(f.tmpDir))
}

// resolvedPath is the PATH the login shell resolves through start.
func resolvedPath(t *testing.T, start Start) []string {
	t.Helper()
	path, err := Path(t.Context(), start)
	require.NoError(t, err)
	return path
}

// appendContents is a line of ~/.profile that appends to PATH a folder named
// for what file holds, or for nothing when it cannot be read. file is expanded
// by the shell.
func appendContents(file string) string {
	return "PATH=\"$PATH:/read-$(cat \"" + file + "\" 2>/dev/null)\"\n"
}

// launchOutput launches /bin/sh as an interactive login shell through start
// with env, as the snapshot does, and answers what command printed between
// the markers. The machine's own profile may print first.
func launchOutput(t *testing.T, start Start, env []string, command string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testDeadline)
	defer cancel()
	const begin, end = "KUBETAIL_BEGIN\n", "KUBETAIL_END\n"
	out, f := Launch(ctx, start, "/bin/sh", InteractiveLogin("echo KUBETAIL_BEGIN; "+command+"; echo KUBETAIL_END"), env, maxOutputBytes, func(buf []byte, _ int) bool {
		return bytes.Contains(buf, []byte(end))
	})
	require.Nil(t, f)
	_, text, _ := strings.Cut(string(out), begin)
	text, _, _ = strings.Cut(text, end)
	return text
}

// The login shell reads nothing in Kubetail's directories, and does read a file
// beside them, so the script is shown to have run.
func TestTheLoginShellCannotReadKubetailsDirectories(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	var script strings.Builder
	for i, dir := range f.kubetail {
		script.WriteString(appendContents(f.write(t, filepath.Base(dir)+"/secret", "kubetail"+strconv.Itoa(i))))
	}
	script.WriteString(appendContents(f.write(t, "control", "control")))
	f.profile(t, script.String())

	path := resolvedPath(t, f.start(sb, nil))

	assert.Contains(t, path, "/read-control")
	for i := range f.kubetail {
		assert.NotContains(t, path, "/read-kubetail"+strconv.Itoa(i))
	}
}

// The resolution's shell reads nothing on the denied-always list, and does
// read the rest of the home.
func TestTheResolutionCannotReadTheNeverList(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	f.profile(t, appendContents(f.write(t, "home/.ssh/id_ed25519", "key"))+appendContents(f.write(t, "home/notes", "notes")))

	path := resolvedPath(t, f.start(sb, sb.Never(f.home)))

	assert.Contains(t, path, "/read-notes")
	assert.NotContains(t, path, "/read-key")
}

// The snapshot's shell reads the whole home, the denied-always list included.
func TestTheSnapshotsShellReadsTheHome(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	key := f.write(t, "home/.ssh/id_ed25519", "key")
	f.profile(t, "")

	out := launchOutput(t, f.start(sb, nil), []string{"HOME=" + f.home}, "cat '"+key+"'")

	assert.Equal(t, "key", out)
}

// What the sandbox refuses to pass is dropped from the shell's environment,
// so the shell still starts.
func TestANeverListVariableIsDropped(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	f.profile(t, "")
	env := []string{"HOME=" + f.home, "SSH_AUTH_SOCK=/tmp/agent.sock", "AWS_PROFILE=work", "ENV=" + f.write(t, "env.sh", ""), "KEPT=yes"}

	out := launchOutput(t, f.start(sb, nil), env, "/usr/bin/env")

	assert.Contains(t, out, "KEPT=yes\n")
	for _, name := range []string{"SSH_AUTH_SOCK=", "AWS_PROFILE=", "ENV="} {
		assert.NotContains(t, "\n"+out, "\n"+name)
	}
}

// recordingCommander builds each run through the real sandbox and keeps the
// Run and the command it built.
type recordingCommander struct {
	sb   *sandbox.Sandbox
	runs []sandbox.Run
	cmds []*exec.Cmd
}

func (r *recordingCommander) Command(ctx context.Context, run sandbox.Run) (*exec.Cmd, error) {
	cmd, err := r.sb.Command(ctx, run)
	r.runs = append(r.runs, run)
	r.cmds = append(r.cmds, cmd)
	return cmd, err
}

// The login shell has no network, so on macOS no trust daemon either: the
// profile names none.
func TestTheLoginShellLooksUpNoTrustDaemon(t *testing.T) {
	f := newFixture(t)
	rec := &recordingCommander{sb: confining(t)}
	f.profile(t, "")

	resolvedPath(t, f.start(rec, nil))

	require.Len(t, rec.runs, 1)
	assert.Zero(t, rec.runs[0].Policy.Network)
	for _, arg := range rec.cmds[0].Args {
		assert.NotContains(t, arg, "com.apple.trustd.agent")
	}
}

// The login shell writes its TMPDIR, and the folder is gone once the shell has
// exited.
func TestTheLoginShellWritesItsScratch(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	f.profile(t, "echo scratch > \"$TMPDIR/f\"\n"+appendContents("$TMPDIR/f"))

	path := resolvedPath(t, f.start(sb, nil))

	assert.Contains(t, path, "/read-scratch")
	t.Run("TestTheLoginShellsScratchIsGone", func(t *testing.T) {
		entries, err := os.ReadDir(f.tmpDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}

// The login shell writes nothing in the home.
func TestTheLoginShellCannotWriteTheHome(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	written := filepath.Join(f.home, "written")
	f.profile(t, "echo x > '"+written+"' 2>/dev/null\nPATH=\"$PATH:/marker\"\n")

	path := resolvedPath(t, f.start(sb, nil))

	assert.Contains(t, path, "/marker")
	assert.NoFileExists(t, written)
}

// A profile line that redirects into the read-only home does not end the
// login shell, which a POSIX sh does to a non-interactive one: the shell runs
// interactive, so the resolution keeps what the profile set before it. A POSIX
// sh still abandons the rest of the file, so nothing after it is asserted.
func TestAProfileThatWritesTheHomeStillResolves(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	f.profile(t, "PATH=\"$PATH:/before\"\n: > \"$HOME/.local/state/marker\" 2>/dev/null || true\n")

	path := resolvedPath(t, f.start(sb, nil))

	assert.Contains(t, path, "/before")
	assert.NoDirExists(t, filepath.Join(f.home, ".local"))
}

// The login shell cannot reach a listener on the machine's loopback.
func TestTheLoginShellHasNoNetwork(t *testing.T) {
	sb := confining(t)
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("no /bin/bash to dial with")
	}
	f := newFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan struct{}, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			conn.Close()
			accepted <- struct{}{}
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	f.profile(t, "PATH=\"$PATH:/tried\"\n"+
		"/bin/bash -c 'exec 3<>/dev/tcp/127.0.0.1/"+port+"' 2>/dev/null && PATH=\"$PATH:/connected\"\n")

	path := resolvedPath(t, f.start(sb, nil))

	assert.Contains(t, path, "/tried")
	assert.NotContains(t, path, "/connected")
	assert.Empty(t, accepted)
}

// Launch puts the sandboxed shell in a session of its own, and its kill
// reaches a child the script left behind.
func TestLaunchSetsTheSession(t *testing.T) {
	f := newFixture(t)
	rec := &recordingCommander{sb: confining(t)}
	f.profile(t, "")
	marker := strconv.Itoa(100000 + rand.IntN(900000))

	out := launchOutput(t, f.start(rec, nil), []string{"HOME=" + f.home}, "sleep "+marker+" & echo started")

	assert.Equal(t, "started\n", out)
	require.Len(t, rec.cmds, 1)
	require.NotNil(t, rec.cmds[0].SysProcAttr)
	assert.True(t, rec.cmds[0].SysProcAttr.Setsid)
	require.Eventually(t, func() bool {
		return exec.Command("pgrep", "-f", "sleep "+marker).Run() != nil
	}, testutil.Timeout, 10*time.Millisecond, "the child outlived the kill")
}

// A nushell that cannot write its state under a read-only home exits, and
// the resolution answers that, within its time.
func TestAShellThatCannotWriteItsStateFallsBack(t *testing.T) {
	sb := confining(t)
	f := newFixture(t)
	nu := f.write(t, "bin/nu", "#!/bin/sh\n"+
		"mkdir -p \"$HOME/.config/nushell\" 2>/dev/null && : > \"$HOME/.config/nushell/history.txt\" 2>/dev/null || exit 1\n"+
		"echo 'unexpected'\n")
	require.NoError(t, os.Chmod(nu, 0o700))
	findShell = func(context.Context) (string, *Fault) { return nu, nil }

	_, err := Path(t.Context(), f.start(sb, nil))

	var fault *Fault
	require.ErrorAs(t, err, &fault)
	assert.Equal(t, reasonShellExited, fault.Reason)
	assert.Equal(t, 1, fault.ExitCode)
}

// The shell starts with no Azure credential, but keeps AZURE_CONFIG_DIR,
// which says where the user's az keeps its login.
func TestTheShellKeepsAzuresConfigDir(t *testing.T) {
	got := sandboxedEnv([]string{
		"AZURE_CONFIG_DIR=/home/ana/.azure-work", "AZURE_CLIENT_SECRET=s", "ARM_CLIENT_SECRET=s", "KEPT=yes",
	}, "/scratch")
	assert.Equal(t, []string{"AZURE_CONFIG_DIR=/home/ana/.azure-work", "KEPT=yes", "TMPDIR=/scratch"}, got)
}

// The sandboxed shell starts from the process's environment, so a real AWS key
// the user exported stays out of it, though a cloud platform's mount may set
// the same names in a command's run.
func TestTheSandboxedShellDropsAWSCredentials(t *testing.T) {
	env := sandboxedEnv([]string{
		"PATH=/usr/bin", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "AWS_REGION=eu-west-1",
	}, "/tmp/scratch")

	assert.Equal(t, []string{"PATH=/usr/bin", "TMPDIR=/tmp/scratch"}, env)
}

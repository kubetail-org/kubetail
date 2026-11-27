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

package sandbox

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// paramsOf is a profile's -D arguments, by name.
func paramsOf(t *testing.T, args []string) map[string]string {
	t.Helper()
	require.Zero(t, len(args)%2, args)
	params := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		require.Equal(t, "-D", args[i])
		name, value, ok := strings.Cut(args[i+1], "=")
		require.True(t, ok, args[i+1])
		params[name] = value
	}
	return params
}

// nameOf is the one parameter of the kind prefix names whose value is p.
func nameOf(t *testing.T, params map[string]string, prefix, p string) string {
	t.Helper()
	var names []string
	for name, value := range params {
		if value == p && strings.HasPrefix(name, prefix+"_") {
			names = append(names, name)
		}
	}
	require.Len(t, names, 1, "one %s parameter names %s: %v", prefix, p, params)
	return names[0]
}

// ruleAt is where the text first reads the parameter name.
func ruleAt(t *testing.T, text, name string) int {
	t.Helper()
	i := strings.Index(text, `(param "`+name+`")`)
	require.GreaterOrEqual(t, i, 0, "no rule reads %s", name)
	return i
}

// profileRun is a run over a stand-in machine for the profile's own tests: a
// home whose ~/.cargo is a toolchain folder, one System folder outside it,
// and Kubetail's three directories holding the workspace, the TMPDIR and the
// run's own directory. Its policy is the Workspace policy on s.
func profileRun(t *testing.T, s *Sandbox) (Run, string) {
	t.Helper()
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "home/.cargo/bin", "tools/bin", "data/chats/c/workspace", "cache/tmp/1-a", "runtime/runs/1-a")
	home := filepath.Join(base, "home")
	addToolchain(t, "~/.cargo")
	addRoot(t, d[1])
	env := []string{"PATH=" + d[0] + string(filepath.ListSeparator) + d[1] + string(filepath.ListSeparator) + "/usr/bin"}
	kubetail := []string{filepath.Join(base, "data"), filepath.Join(base, "cache"), filepath.Join(base, "runtime")}
	return Run{
		Shell: "/bin/sh",
		Dir:   d[2],
		Env:   env,
		Policy: Policy{
			Files:  s.System(home, "/bin/sh").Files.Outside(kubetail...),
			Always: AlwaysPolicy{Deny: s.Never(home), Kubetail: kubetail, Read: []string{d[4]}, Write: []string{d[2], d[3]}},
		},
	}, base
}

// ruleNamed is where the text first reads the rule whose format and value are
// given, its %s the rule's parameter.
func ruleNamed(t *testing.T, text string, params map[string]string, format, p string) int {
	t.Helper()
	for name, value := range params {
		if value == p && strings.HasPrefix(name, "RULE_") {
			if i := strings.Index(text, fmt.Sprintf(format, name)); i >= 0 {
				return i
			}
		}
	}
	require.Failf(t, "no rule", "%s for %s in %v", format, p, params)
	return -1
}

// The rule shapes each kind compiles to.
const (
	readRule  = `(allow file-read* (subpath (param "%s")))`
	writeRule = `(allow file-read* file-write* (subpath (param "%s")))`
	denyRule  = `(deny file-read* file-write* (subpath (param "%s")))`
)

// Every path reaches the profile as a parameter, and its text holds only names
// Kubetail made: a path holding a quote or a parenthesis is data, never code.
func TestTheProfileWritesNoPathIntoItsText(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)
	odd := mkdirs(t, base, "data/we\"ird)\n(allow default)")[0]
	r.Policy.Always.Write[0], r.Dir = odd, odd

	text, args := s.profile(r, compiledRules(t, r.Policy))

	assert.NotContains(t, text, base)
	assert.NotContains(t, text, "(allow default)")
	for _, m := range []string{markerRules, markerAncestors, markerNetwork} {
		assert.NotContains(t, text, m)
	}
	params := paramsOf(t, args)
	ruleNamed(t, text, params, writeRule, odd)
	for name := range params {
		assert.Regexp(t, `^[A-Z]+(_[A-Z]+)*(_[0-9]+)?$`, name)
	}
}

// The System folders that exist, the toolchain folders and the sidecar's own
// executable are read and not written; the workspace and the TMPDIR are read
// and written, the run's directory read.
func TestTheProfileNamesWhatARunReads(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)

	text, args := s.profile(r, compiledRules(t, r.Policy))
	params := paramsOf(t, args)

	reads := []string{realpath.Resolved("/bin/sh"), filepath.Join(base, "home", ".cargo"), filepath.Join(base, "tools", "bin")}
	for _, p := range platformLists.System {
		if _, err := os.Stat(p); err == nil {
			reads = append(reads, realpath.Resolved(p))
		}
	}
	for _, p := range reads {
		i := ruleNamed(t, text, params, readRule, p)
		assert.Less(t, i, ruleNamed(t, text, params, `(deny file-write* (subpath (param "%s")))`, p), p)
	}
	for _, p := range r.Policy.Always.Write {
		ruleNamed(t, text, params, writeRule, p)
	}
	ruleNamed(t, text, params, readRule, r.Policy.Always.Read[0])
}

// Seatbelt checks a file's real path, so every path is resolved, but for the
// socket, which is named both as given and as resolved.
func TestAProfilePathIsResolved(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)
	runDir := r.Policy.Always.Read[0]
	link := filepath.Join(base, "ws-link")
	require.NoError(t, os.Symlink(r.Policy.Always.Write[0], link))
	sockLink := filepath.Join(base, "runtime-link")
	require.NoError(t, os.Symlink(runDir, sockLink))
	r.Policy.Always.Write[0], r.Dir = link, link
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "proxy.sock"), nil, 0o600))
	socket := filepath.Join(sockLink, "proxy.sock")
	r.Policy.Network.Relays = []Relay{{Port: 4321, Socket: socket}}

	text, args := s.profile(r, compiledRules(t, r.Policy))
	params := paramsOf(t, args)

	ruleNamed(t, text, params, writeRule, filepath.Join(base, "data", "chats", "c", "workspace"))
	assert.Equal(t, socket, params["SOCKET"])
	assert.Equal(t, filepath.Join(runDir, "proxy.sock"), params["SOCKET_RESOLVED"])
}

// Denied are the Always paths and Files Denies a read takes in — here the
// credential paths in a tree, /etc's secrets and Homebrew's var — after the
// read that holds them and before the run's own, since a later rule wins.
// Kubetail's directories and the other homes, which no read takes in, are left
// out.
func TestTheProfileDeniesBetweenTheReadsAndTheRunsOwn(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)
	brew := mkdirs(t, base, "tools/bin/var")[0]
	r.Policy.Files.Deny = []string{brew}

	text, args := s.profile(r, compiledRules(t, r.Policy))
	params := paramsOf(t, args)

	cargo := filepath.Join(base, "home", ".cargo")
	denied := []string{filepath.Join(cargo, "credentials"), filepath.Join(cargo, "credentials.toml"), brew}
	for _, p := range platformLists.Never {
		if !strings.HasPrefix(p, "~/") {
			denied = append(denied, p)
		}
	}
	var names []string
	for name, v := range params {
		if strings.HasPrefix(name, "RULE_") && strings.Contains(text, fmt.Sprintf(denyRule, name)) {
			names = append(names, v)
		}
	}
	assert.ElementsMatch(t, denied, names, "no Deny a read does not take in")
	for _, p := range denied {
		i := ruleNamed(t, text, params, denyRule, p)
		assert.Less(t, ruleNamed(t, text, params, readRule, filepath.Dir(p)), i, p)
		assert.Less(t, i, ruleNamed(t, text, params, writeRule, r.Policy.Always.Write[0]))
		assert.Less(t, i, ruleNamed(t, text, params, readRule, r.Policy.Always.Read[0]))
	}
}

// Each of the run's own Write paths is followed by a denial of its root's own
// entry, so the allow cannot outrank it; a Files Write rule gets none.
func TestTheProfileKeepsARunsOwnRootsInPlace(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)
	extra := mkdirs(t, base, "extra")[0]
	r.Policy.Files.Write = append(r.Policy.Files.Write, extra)

	text, args := s.profile(r, compiledRules(t, r.Policy))
	params := paramsOf(t, args)

	deny := `(deny file-write-unlink file-write-create (literal (param "%s")))`
	for _, p := range r.Policy.Always.Write {
		name := nameOf(t, params, "RULE", realpath.Resolved(p))
		assert.Contains(t, text, fmt.Sprintf(writeRule, name)+"\n"+fmt.Sprintf(deny, name))
	}
	name := nameOf(t, params, "RULE", extra)
	assert.NotContains(t, text, fmt.Sprintf(deny, name))
}

// A credential path is compared at its target against every read, the System
// folders among them: ~/.aws linked under /usr is denied there.
func TestTheProfileDeniesALinkedCredentialPathUnderARoot(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)
	require.NoError(t, os.Symlink("/usr/share", filepath.Join(base, "home", ".aws")))

	text, args := s.profile(r, compiledRules(t, r.Policy))

	ruleNamed(t, text, paramsOf(t, args), denyRule, "/usr/share")
}

// Every ancestor of what a run reads can be statted, after the denials and the
// fixed rules, so the workspace's own resolves though the data directory
// holding it is denied.
func TestTheProfileLetsEveryAncestorBeStattedAfterTheDenials(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, base := profileRun(t, s)

	text, args := s.profile(r, compiledRules(t, r.Policy))
	params := paramsOf(t, args)

	lastDenial := strings.LastIndex(text, "(deny file-read* file-write*")
	fixed := strings.Index(text, `(allow file-read-data (literal "/"))`)
	require.Greater(t, fixed, lastDenial)
	for _, p := range []string{"/", base, filepath.Join(base, "data"), filepath.Join(base, "data", "chats", "c"), filepath.Dir(realpath.Resolved("/bin/sh"))} {
		name := nameOf(t, params, "UP", p)
		assert.Contains(t, text, `(allow file-read-metadata (literal (param "`+name+`")))`)
		assert.Greater(t, ruleAt(t, text, name), fixed)
	}
}

// A run with no relay has no network rule at all; one with a relay may bind
// and reach its port and reach its socket, and nothing else.
func TestOnlyARunWithASocketHasNetwork(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/sh"}
	r, _ := profileRun(t, s)

	text, args := s.profile(r, compiledRules(t, r.Policy))
	assert.NotContains(t, text, "network")
	assert.NotContains(t, paramsOf(t, args), "SOCKET")

	r.Policy.Network.Relays = []Relay{{Port: 4321, Socket: filepath.Join(r.Policy.Always.Read[0], "proxy.sock")}}
	text, _ = s.profile(r, compiledRules(t, r.Policy))
	assert.Contains(t, text, `(allow network-bind network-inbound (local tcp4 "localhost:4321"))`)
	assert.Contains(t, text, `(remote tcp4 "localhost:4321")`)
	assert.Contains(t, text, `(remote unix-socket (path-literal (param "SOCKET")))`)
	assert.Contains(t, text, `(remote unix-socket (path-literal (param "SOCKET_RESOLVED")))`)
	assert.Equal(t, 2, strings.Count(text, "(allow network"), "one bind rule and one outbound rule")
}

func TestARefusedServiceMatchesByNameOrPrefix(t *testing.T) {
	t.Parallel()

	assert.True(t, refused("com.apple.dnssd.service"))
	assert.True(t, refused("com.apple.lsd.mapdb"))
	assert.True(t, refused("com.apple.pasteboard.1"))
	assert.False(t, refused("com.apple.dnssd.service.other"))
	assert.False(t, refused("com.apple.system.logger"))
}

// The profile's Mach allowlist names no refused service, and no rule lets
// cfprefsd serve another app's preferences to the run.
func TestTheProfileNamesNoRefusedService(t *testing.T) {
	t.Parallel()

	names := regexp.MustCompile(`global-name "([^"]+)"`).FindAllStringSubmatch(profileText, -1)
	require.NotEmpty(t, names)
	for _, n := range names {
		assert.False(t, refused(n[1]), n[1])
	}
	assert.NotContains(t, profileText, "user-preference")
	assert.False(t, slices.ContainsFunc(names, func(n []string) bool { return n[1] == "" }))

	// The one exception: a run with the internet reaches the resolver and the
	// trust daemon, which every other run is refused.
	var internet []string
	for _, n := range regexp.MustCompile(`global-name "([^"]+)"`).FindAllStringSubmatch(internetRules, -1) {
		internet = append(internet, n[1])
	}
	assert.Equal(t, []string{"com.apple.dnssd.service", "com.apple.trustd.agent"}, internet)
	for _, n := range internet {
		assert.True(t, refused(n), n)
	}
}

// launcher writes a stand-in for sandbox-exec: script, run with the
// launcher's arguments.
func launcher(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sandbox-exec")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700))
	return path
}

// passThrough is a launcher that drops the profile's arguments and runs the
// command after them, as sandbox-exec does once the profile is applied.
const passThrough = `while [ "$1" = -p ] || [ "$1" = -D ]; do shift 2; done
exec "$@"
`

func TestWithoutSandboxExecThereIsNoSandbox(t *testing.T) {
	t.Parallel()

	s, v := probe(t.Context(), filepath.Join(t.TempDir(), "sandbox-exec"), testutil.Timeout)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "sandbox-exec not found"}, v)
}

// A probe with no home to read or no directory to probe in has no sandbox, and
// says which.
func TestAProbeWithoutItsDirectoriesSaysWhich(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	path := launcher(t, passThrough)

	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	s, v := probe(t.Context(), path, testutil.Timeout)
	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "cannot make a directory to probe in"}, v)

	t.Setenv("HOME", "")
	s, v = probe(t.Context(), path, testutil.Timeout)
	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "cannot find the home directory"}, v)
}

// With no descriptor free under the open-files limit, no port can be taken.
// Serial: it lowers this process's limit, to the three standard streams.
func TestAPortThatCannotBeTakenFails(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &own))
	low := own
	low.Cur = 3
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &low))
	_, err := (&Sandbox{}).Port()
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &own))

	assert.Error(t, err)
}

// With no descriptor free, the probe has no lifeline to hand a run, so no
// sandbox. Serial: it lowers this process's open-files limit.
func TestAProbeWithNoDescriptorFreeHasNoSandbox(t *testing.T) {
	path := launcher(t, passThrough)
	openFilesLimit(t, 3)

	s, v := probe(t.Context(), path, testutil.Timeout)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "cannot make the runs' lifeline"}, v)
}

// A profile the launcher refuses fails the probe with its stderr's first line.
func TestAProbeThatFailsSaysWhy(t *testing.T) {
	t.Parallel()

	path := launcher(t, "echo 'sandbox_apply: Operation not permitted' >&2\necho more >&2\nexit 71\n")

	s, v := probe(t.Context(), path, testutil.Timeout)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "sandbox_apply: Operation not permitted"}, v)
}

// A launcher that runs true under the profile is a sandbox that confines,
// and gives a run the internet wherever it is.
func TestAProbeThatRunsIsASandbox(t *testing.T) {
	t.Parallel()

	s, v := probe(t.Context(), launcher(t, passThrough), testutil.Timeout)

	require.NotNil(t, s)
	closing(t, s)
	assert.True(t, v.Available)
	assert.True(t, s.Confines())
	assert.True(t, v.NetworkAvailable)
	available, reason := s.NetworkStatus()
	assert.True(t, available)
	assert.Empty(t, reason)
}

// A probe that runs out of time keeps the sandbox, so a slow start leaves no
// command unconfined. The launcher hangs on purpose: the probe's own timeout
// ends it.
func TestAProbeThatTimesOutKeepsTheSandbox(t *testing.T) {
	// It waits on the package's boundedCalls, which every probe adds to, so this test stays serial.
	t.Cleanup(boundedCalls.Wait)
	s, v := probe(t.Context(), launcher(t, "exec sleep 60\n"), 10*time.Millisecond)

	require.NotNil(t, s)
	closing(t, s)
	assert.Equal(t, State{Available: true, Reason: "Seatbelt, unconfirmed: the probe did not finish within 10ms", NetworkAvailable: true}, v)
	assert.True(t, s.Confines())
}

// The command is sandbox-exec with the run's profile, then the run's argv: the
// forwarder, with sandbox-shell its child, which execs the shell.
func TestTheCommandIsSandboxExecOverTheProfile(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := &Sandbox{self: "/bin/kubetail-sidecar", launcher: "/usr/bin/sandbox-exec"}
	r, _ := profileRun(t, s)
	r.Args = []string{"-c", "exit 0"}

	cmd := command(t, s, t.Context(), r)

	text, params := s.profile(r, compiledRules(t, r.Policy))
	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Path)
	assert.Equal(t, slices.Concat([]string{"/usr/bin/sandbox-exec", "-p", text}, params,
		[]string{"/bin/kubetail-sidecar", InitCommand, "--", "/bin/kubetail-sidecar", ShellCommand, "--", "/bin/sh", "-c", "exit 0"}), cmd.Args)
	assert.Equal(t, r.Dir, cmd.Dir)
	assert.Equal(t, r.Env, cmd.Env)

	r.Policy.Network.Relays = []Relay{{Port: 4321, Socket: "/run/p.sock"}}
	cmd = command(t, s, t.Context(), r)
	name, args := s.argv(r)
	assert.Equal(t, append([]string{name}, args...), cmd.Args[len(cmd.Args)-len(args)-1:])
}

// Every run starts as this executable's forwarder, with this executable as
// sandbox-shell its child: a run with a socket carries it, and one with
// limits carries them to sandbox-shell.
func TestEveryRunStartsAsTheForwarder(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/bin/kubetail-sidecar"}
	r := Run{Shell: "/bin/sh", Args: []string{"-c", "--port 1"}}

	name, args := s.argv(r)
	require.Equal(t, "/bin/kubetail-sidecar", name)
	assert.Equal(t, []string{InitCommand, "--", "/bin/kubetail-sidecar", ShellCommand, "--", "/bin/sh", "-c", "--port 1"}, args)

	r.Policy.Network.Relays = []Relay{{Port: 6443, Socket: "/run/p.sock"}}
	_, args = s.argv(r)
	assert.Equal(t, []string{
		InitCommand, "--socket", "/run/p.sock", "--port", "6443", "--", "/bin/kubetail-sidecar", ShellCommand, "--", "/bin/sh", "-c", "--port 1",
	}, args)

	r.Policy.Network.Relays = nil
	r.Policy.Limits = Limits{CPUSeconds: 7, OpenFiles: 64, Processes: 512}
	_, args = s.argv(r)
	assert.Equal(t, []string{
		InitCommand, "--", "/bin/kubetail-sidecar", ShellCommand, "--cpu", "7", "--cpu-grace", "5", "--files", "64", "--processes", "513", "--", "/bin/sh", "-c", "--port 1",
	}, args)
}

// macOS has no memory limit to set, so a run asking for one is refused rather
// than run without it.
func TestAMemoryLimitIsRefused(t *testing.T) {
	t.Parallel()

	r := Run{Shell: "/bin/sh", Policy: Policy{Limits: Limits{MemoryBytes: 1 << 34, OpenFiles: 64}}}

	cmd, err := (&Sandbox{self: "/bin/sh", launcher: "/usr/bin/true"}).Command(t.Context(), r)

	assert.Nil(t, cmd)
	assert.ErrorIs(t, err, errNoMemoryLimit)
}

// A profile that hangs, as one resolving a path on a stalled network mount
// does, holds the command only until its context ends.
func TestACommandWhoseProfileHangsIsRefusedWhenItsContextEnds(t *testing.T) {
	// It waits on the package's boundedCalls, which every probe adds to, so this test stays serial.
	release, started := make(chan struct{}), make(chan struct{})
	// Cleanups run last first, so the build is released before it is waited on.
	t.Cleanup(boundedCalls.Wait)
	t.Cleanup(func() { close(release) })
	s := &Sandbox{self: "/bin/sh", launcher: "/usr/bin/true"}
	s.buildProfile = func(Run, []rule) (string, []string) {
		close(started)
		<-release
		return "", nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	type made struct {
		cmd *exec.Cmd
		err error
	}
	done := make(chan made, 1)

	go func() {
		cmd, err := s.Command(ctx, Run{Shell: "/bin/sh"})
		done <- made{cmd, err}
	}()
	testutil.Wait(t, started, "the profile build to start")
	cancel()

	m := testutil.Recv(t, done, "Command to return while the build hangs")
	assert.Nil(t, m.cmd)
	assert.ErrorIs(t, m.err, context.Canceled)
}

// The port a run's kubeconfig dials is a free loopback one, which the
// forwarder can take.
func TestThePortIsAFreeLoopbackOne(t *testing.T) {
	t.Parallel()

	port, err := (&Sandbox{}).Port()
	require.NoError(t, err)

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	_ = ln.Close()
}

// The test binary answers these when a run starts it as a process that tries
// to leave its group (leaveGroup), as the process left behind (linger), to
// send a byte (send), to read a process's arguments (procArgs), or as a
// process that waits for its stdin to close (holdEnv). init runs them before
// TestMain, which knows nothing of macOS.
const (
	leaveEnv    = "KUBETAIL_SANDBOX_TEST_LEAVE"
	lingerEnv   = "KUBETAIL_SANDBOX_TEST_LINGER"
	homeFileEnv = "KUBETAIL_SANDBOX_TEST_HOME_FILE"
	listenerEnv = "KUBETAIL_SANDBOX_TEST_LISTENER"
	sendEnv     = "KUBETAIL_SANDBOX_TEST_SEND"
	procArgsEnv = "KUBETAIL_SANDBOX_TEST_PROCARGS"
	holdEnv     = "KUBETAIL_SANDBOX_TEST_HOLD"
)

func init() {
	if call := os.Getenv(leaveEnv); call != "" {
		os.Exit(leaveGroup(call))
	}
	if dir := os.Getenv(lingerEnv); dir != "" {
		os.Exit(linger(dir))
	}
	if target := os.Getenv(sendEnv); target != "" {
		os.Exit(send(target))
	}
	if pid := os.Getenv(procArgsEnv); pid != "" {
		os.Exit(procArgs(pid))
	}
	if os.Getenv(holdEnv) != "" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
}

// procArgs prints pid's arguments and environment as ps reads them, through
// kern.procargs2, or why it cannot.
func procArgs(pid string) int {
	n, err := strconv.Atoi(pid)
	var raw []byte
	if err == nil {
		raw, err = unix.SysctlRaw("kern.procargs2", n)
	}
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Println(strings.ReplaceAll(string(raw), "\x00", "\n"))
	return 0
}

// send dials target, "<network> <address>", and writes a byte, printing why
// either fails.
func send(target string) int {
	network, addr, _ := strings.Cut(target, " ")
	c, err := net.Dial(network, addr)
	if err == nil {
		_, err = c.Write([]byte("x"))
		_ = c.Close()
	}
	if err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}

// leaveGroup makes call, setsid or setpgid, and prints what it answered.
// Started by a shell, it leads no group, so outside the sandbox either succeeds.
func leaveGroup(call string) int {
	var err error
	if call == "setsid" {
		_, err = syscall.Setsid()
	} else {
		err = syscall.Setpgid(0, 0)
	}
	fmt.Println(err)
	return 0
}

// linger waits for a byte on dir's go FIFO, which the test writes once it has
// killed the run's group, then tries the stand-in home and a loopback listener
// and writes what it got.
func linger(dir string) int {
	f, err := os.Open(filepath.Join(dir, "go"))
	if err != nil {
		return 1
	}
	_, err = io.ReadFull(f, make([]byte, 1))
	_ = f.Close()
	if err != nil {
		return 1
	}
	home, network := "read", "reached"
	if _, err := os.ReadFile(os.Getenv(homeFileEnv)); err != nil {
		home = "denied"
	}
	if c, err := net.Dial("tcp", os.Getenv(listenerEnv)); err != nil {
		network = "refused"
	} else {
		_ = c.Close()
	}
	result := filepath.Join(dir, "result")
	if err := os.WriteFile(result+".tmp", []byte(home+" "+network), 0o600); err != nil {
		return 1
	}
	return errCode(os.Rename(result+".tmp", result))
}

func errCode(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// machineRun is a run over a stand-in machine, as the Bash tool builds one: a
// home, and Kubetail's data, cache and runtime directories holding the
// workspace, the run's TMPDIR, the kubectl cache, the snapshot and the run's
// directory. Its base is in the per-user temp directory, passed as /var/…
// unresolved as the runtime directory is, and short, so a socket fits.
type machineRun struct {
	Run
	base, home, data, cache, runtime, runDir string
	ws, snapshot, tmp, kubectl, socket       string
	port                                     int
	internet                                 bool
}

// on is m as it runs on s, its policy the Workspace policy: System less what
// lies in Kubetail's directories, the Never paths and Kubetail's directories
// denied, the run's own paths inside them, and with a cluster one relay.
func (m *machineRun) on(s *Sandbox) Run {
	r := m.Run
	kubetail := []string{m.data, m.cache, m.runtime}
	r.Policy = Policy{
		Files: s.System(m.home, r.Shell).Files.Outside(kubetail...),
		Always: AlwaysPolicy{
			Deny: s.Never(m.home), Kubetail: kubetail,
			Read: []string{m.snapshot, m.runDir}, Write: []string{m.ws, m.tmp, m.kubectl},
		},
	}
	if m.socket != "" {
		r.Policy.Network.Relays = []Relay{{Port: m.port, Socket: m.socket}}
	}
	r.Policy.Network.Internet = m.internet
	return r
}

func standIn(t *testing.T) *machineRun {
	t.Helper()
	base, err := os.MkdirTemp("", "kst")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	m := &machineRun{base: base}
	m.home, m.data, m.cache, m.runtime = filepath.Join(base, "home"), filepath.Join(base, "data"), filepath.Join(base, "cache"), filepath.Join(base, "runtime")
	d := mkdirs(t, base, "home", "data/chats/c/workspace", "cache/tmp/1-a", "cache/kubectl/c/s", "runtime/runs/1-a", "runtime/shell")
	ws, tmp, kubectl := d[1], d[2], d[3]
	m.runDir = d[4]
	snapshot := filepath.Join(d[5], "snapshot.sh")
	require.NoError(t, os.WriteFile(snapshot, []byte("true\n"), 0o400))
	SeedTmpDir(tmp)
	m.ws, m.snapshot, m.tmp, m.kubectl = ws, snapshot, tmp, kubectl
	m.Run = Run{
		Shell: "/bin/sh",
		Dir:   ws,
		Env:   []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + ws, "TMPDIR=" + tmp},
	}
	return m
}

// withCluster gives the run a socket, serving HTTP that answers ok, and a
// free port for its forwarder.
func (m *machineRun) withCluster(t *testing.T) {
	t.Helper()
	m.socket = filepath.Join(m.runDir, "proxy.sock")
	serveHTTP(t, "unix", m.socket, "ok")
	m.port = freePort(t)
	m.Env = append(m.Env, "PORT="+strconv.Itoa(m.port), "SOCKET="+m.socket)
}

// serveHTTP answers body to every request on a listener of network at addr,
// and answers the address it listens on.
func serveHTTP(t *testing.T, network, addr, body string) string {
	t.Helper()
	ln, err := net.Listen(network, addr)
	require.NoError(t, err)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }),
		ReadHeaderTimeout: testutil.Timeout,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// sh runs script under the sandbox with env added, and answers its output and
// whether it exited 0.
func sh(t *testing.T, s *Sandbox, r Run, script string, env ...string) (string, bool) {
	t.Helper()
	out, ok, _ := shWithin(t, s, r, testutil.Timeout, script, env...)
	return out, ok
}

// shWithin is sh bounded by d, and answers whether d ran out. The output
// leaves out coverWarning.
func shWithin(t *testing.T, s *Sandbox, r Run, d time.Duration, script string, env ...string) (string, bool, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	r.Args = []string{"-c", script}
	r.Env = slices.Concat(r.Env, env)
	out, err := command(t, s, ctx, r).CombinedOutput()
	return withoutCoverWarning(out), err == nil, ctx.Err() != nil
}

// logDenials logs what the sandbox refused since start, which tells a slow
// refusal from a run that something let through and left waiting.
func logDenials(t *testing.T, start time.Time) {
	t.Helper()
	out, err := exec.Command("/usr/bin/log", "show", "--style", "compact",
		"--start", start.Format(time.DateTime),
		"--predicate", `sender == "Sandbox"`).CombinedOutput()
	t.Logf("sandbox denials since %s (err %v):\n%s", start.Format(time.DateTime), err, out)
}

// write makes a file holding "secret" under dir.
func write(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("secret"), 0o600))
	return path
}

// These are the listed programs the profile's rules and Mach services answer
// to. A Go binary is among them: a run with a cluster starts the test binary
// as its forwarder.
func TestTheListedProgramsRun(t *testing.T) {
	t.Parallel()

	s := confining(t)
	hasTools := exec.Command("xcode-select", "-p").Run() == nil
	for _, p := range []struct{ name, script string }{
		{"sh", "/bin/sh -c true"},
		{"bash", "/bin/bash -c true"},
		{"zsh", "/bin/zsh -c true"},
		{"perl", "/usr/bin/perl -e 1"},
		{"python3", "/usr/bin/python3 -c pass"},
		{"git", "/usr/bin/git --version"},
		{"awk", "awk 'BEGIN { exit 0 }'"},
		{"sed", "echo a | sed s/a/b/"},
		{"grep", "echo a | grep -q a"},
		{"sort", "printf 'b\\na\\n' | sort"},
		{"tar", `tar -cf "$TMPDIR/t.tar" -C "$HOME" .`},
		{"mktemp", `f=$(mktemp) && case "$f" in "$TMPDIR"*) ;; *) exit 1 ;; esac`},
		{"curl", `curl -sS --fail "http://127.0.0.1:$PORT/"`},
	} {
		t.Run(p.name, func(t *testing.T) {
			bound := testutil.Timeout
			if p.name == "git" || p.name == "python3" {
				if !hasTools {
					t.Skip("no Command Line Tools: /usr/bin/" + p.name + " only offers to install them")
				}
				// A user who has run the tool has it in xcrun's cache, which
				// the run starts with. On a fresh runner the first launch
				// resolves the Command Line Tools and can take tens of
				// seconds, and the run after it some of that again.
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				start := time.Now()
				out, err := exec.CommandContext(ctx, "/bin/sh", "-c", p.script).CombinedOutput()
				t.Logf("unconfined warm-up took %s", time.Since(start))
				require.NoError(t, err, "unconfined: %s", out)
				bound = time.Minute
			}
			m := standIn(t)
			m.withCluster(t)

			out, ok, late := shWithin(t, s, m.on(s), bound, p.script)

			assert.False(t, late, "did not end within %s", bound)
			assert.True(t, ok, out)
		})
	}
}

func TestTheHomeIsUnreadable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	file := write(t, m.home, "secret")

	out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+file)
	assert.False(t, ok, out)
	out, ok = sh(t, s, m.on(s), `ls "$F"`, "F="+m.home)
	assert.False(t, ok, out)
}

func TestACredentialPathInsideAReadableTreeIsUnreadable(t *testing.T) {
	// addToolchain changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	bin := mkdirs(t, m.home, ".cargo/bin")[0]
	require.NoError(t, os.WriteFile(filepath.Join(bin, "tool"), []byte("#!/bin/sh\necho tool\n"), 0o700))
	creds := write(t, m.home, ".cargo/credentials.toml")
	addToolchain(t, "~/.cargo")
	m.Env[0] = "PATH=" + bin + ":/usr/bin:/bin"

	out, ok := sh(t, s, m.on(s), "tool")
	assert.True(t, ok, out)
	assert.Equal(t, "tool\n", out)
	out, ok = sh(t, s, m.on(s), `cat "$F"`, "F="+creds)
	assert.False(t, ok, out)
}

// ~/.aws linked into ~/tools is unreadable at its target, which the ~/tools
// toolchain folder takes in.
func TestALinkedCredentialPathIsUnreadable(t *testing.T) {
	// addToolchain changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	bin := mkdirs(t, m.home, "tools/bin")[0]
	secret := write(t, m.home, "tools/aws/credentials")
	require.NoError(t, os.Symlink(filepath.Dir(secret), filepath.Join(m.home, ".aws")))
	addToolchain(t, "~/tools")
	m.Env[0] = "PATH=" + bin + ":/usr/bin:/bin"

	out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+secret)

	assert.False(t, ok, out)
}

// Homebrew's var is denied inside a tree that takes it in; a stand-in plays it.
func TestHomebrewsVarIsDenied(t *testing.T) {
	// It changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	brew := mkdirs(t, m.base, "brew")[0]
	db := write(t, brew, "var/db")
	readme := write(t, brew, "readme")
	keepLists(t)
	brewVar = []string{filepath.Join(brew, "var")}
	addRoot(t, brew)

	out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+db)
	assert.False(t, ok, out)
	out, ok = sh(t, s, m.on(s), `cat "$F"`, "F="+readme)
	assert.True(t, ok, out)
}

// /etc is read whole and its secret files are not, whatever their mode: a
// stand-in plays it, and the real /etc/hosts still reads.
func TestEtcSecretsStayHidden(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	etc := filepath.Join(realpath.Resolved(m.base), "etc")
	hosts := write(t, etc, "hosts")
	require.NoError(t, os.WriteFile(hosts, []byte("hosts-read"), 0o644))
	key := write(t, etc, "ssh/ssh_host_ed25519_key")
	shadow := write(t, etc, "shadow")
	for _, f := range []string{key, shadow} {
		require.NoError(t, os.Chmod(f, 0o644))
	}
	addRoot(t, etc)
	keepLists(t)
	platformLists.Never = append(slices.Clone(platformLists.Never), filepath.Join(etc, "ssh"), shadow)

	out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+hosts)
	assert.True(t, ok, out)
	assert.Equal(t, "hosts-read", out)
	for _, f := range []string{key, shadow} {
		out, ok = sh(t, s, m.on(s), `cat "$F"`, "F="+f)
		assert.False(t, ok, out)
	}

	out, ok = sh(t, s, m.on(s), "cat /etc/hosts >/dev/null && echo read")
	assert.True(t, ok, out)
	assert.Equal(t, "read\n", out)
}

// A folder on the sidecar's PATH opens nothing: only the lists decide what a
// run reads.
func TestAFolderOnThePathIsNotRead(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	bin := mkdirs(t, m.home, "notes/bin")[0]
	require.NoError(t, os.WriteFile(filepath.Join(bin, "tool"), []byte("#!/bin/sh\necho tool\n"), 0o700))
	notes := write(t, m.home, "notes/todo")
	m.Env[0] = "PATH=" + bin + ":/usr/bin:/bin"

	out, ok := sh(t, s, m.on(s), "tool")
	assert.False(t, ok, out)
	out, ok = sh(t, s, m.on(s), `cat "$F"`, "F="+notes)
	assert.False(t, ok, out)
	out, ok = sh(t, s, m.on(s), `ls "$F"`, "F="+filepath.Dir(notes))
	assert.False(t, ok, out)
}

// A Read of the home leaves the Closed folders shut, and a Read of a folder
// inside one opens that folder.
func TestAGrantOfTheHomeLeavesClosedFoldersShut(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	notes := write(t, m.home, "notes")
	private := write(t, m.home, "Documents/private")
	project := write(t, m.home, "Documents/project/main.go")
	r := m.on(s)
	r.Policy.Files.Read = append(r.Policy.Files.Read, m.home)

	out, ok := sh(t, s, r, `cat "$F"`, "F="+notes)
	assert.True(t, ok, out)
	for _, f := range []string{private, project} {
		out, ok = sh(t, s, r, `cat "$F"`, "F="+f)
		assert.False(t, ok, out)
	}

	r.Policy.Files.Read = append(r.Policy.Files.Read, filepath.Dir(project))
	out, ok = sh(t, s, r, `cat "$F"`, "F="+project)
	assert.True(t, ok, out)
	out, ok = sh(t, s, r, `cat "$F"`, "F="+private)
	assert.False(t, ok, out)
}

// Each Toolchain location runs a program from its first folder, with its
// variables set and pointing into the home.
func TestEachToolchainLocationRunsAProgram(t *testing.T) {
	t.Parallel()

	s := confining(t)
	for _, l := range sharedLists.Toolchain {
		t.Run(l.Name, func(t *testing.T) {
			m := standIn(t)
			dir := inHome(m.home, l.Read[:1])[0]
			if filepath.Base(dir) != "bin" {
				dir = filepath.Join(dir, "bin")
			}
			require.NoError(t, os.MkdirAll(dir, 0o700))
			prog := filepath.Join(dir, "kubetail-tool")
			require.NoError(t, os.WriteFile(prog, []byte("#!/bin/sh\necho tool-ran\n"), 0o700))
			r := m.on(s)
			r.Env = append(r.Env, s.System(m.home, r.Shell).Env...)
			script := `"$P"`
			for name := range l.Env {
				script += `; printf '%s\n' "$` + name + `"`
			}

			out, ok := sh(t, s, r, script, "P="+prog)

			require.True(t, ok, out)
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			assert.Equal(t, "tool-ran", lines[0])
			require.Len(t, lines, 1+len(l.Env))
			for _, v := range lines[1:] {
				assert.True(t, realpath.Within(v, m.home), "%s is not under the home", v)
			}
		})
	}
}

func TestKubetailsDirectoriesAreUnreadable(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	assert.True(t, strings.HasPrefix(m.base, "/var/"), "the run's paths are passed unresolved: %s", m.base)
	for _, f := range []string{write(t, m.data, "app.db"), write(t, m.cache, "kubestore/c.db"), write(t, m.runtime, "host.sock"), write(t, m.runtime, "shell/other")} {
		out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+f)
		assert.False(t, ok, "%s: %s", f, out)
	}
	for _, f := range []string{
		write(t, m.ws, "f"), m.snapshot, write(t, m.runDir, "kubeconfig"),
		write(t, m.tmp, "f"), write(t, m.kubectl, "f"),
	} {
		out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+f)
		assert.True(t, ok, "%s: %s", f, out)
	}
}

func TestASiblingRunsKubeconfigIsUnreadable(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	m.withCluster(t)
	sibling := mkdirs(t, m.runtime, "runs/1-b")[0]
	kubeconfig := write(t, sibling, "kubeconfig")
	socket := filepath.Join(sibling, "proxy.sock")
	serveHTTP(t, "unix", socket, "sibling")

	out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+kubeconfig)
	assert.False(t, ok, out)
	out, _ = sh(t, s, m.on(s), `curl -sS --unix-socket "$F" http://x/`, "F="+socket)
	assert.NotContains(t, out, "sibling")
}

func TestTheTempDirectoriesAreDenied(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	for _, base := range []string{"", "/tmp"} {
		dir, err := os.MkdirTemp(base, "kst-outside")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		file := write(t, dir, "f")

		out, ok := sh(t, s, m.on(s), `cat "$F"`, "F="+file)
		assert.False(t, ok, "%s: %s", file, out)
		out, ok = sh(t, s, m.on(s), `echo x > "$D/new"`, "D="+dir)
		assert.False(t, ok, "%s: %s", dir, out)
		assert.NoFileExists(t, filepath.Join(dir, "new"))
	}
}

func TestOnlyTheWorkspaceAndTheCacheAreWritten(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	m := standIn(t)
	for _, dir := range []string{m.ws, m.tmp, m.kubectl} {
		out, ok := sh(t, s, m.on(s), `echo x > "$D/new"`, "D="+dir)
		assert.True(t, ok, "%s: %s", dir, out)
	}
	for _, dir := range []string{m.home, filepath.Dir(m.ws), m.runDir, filepath.Dir(m.snapshot), m.base} {
		out, ok := sh(t, s, m.on(s), `echo x > "$D/new"`, "D="+dir)
		assert.False(t, ok, "%s: %s", dir, out)
		assert.NoFileExists(t, filepath.Join(dir, "new"))
	}
}

// A run cannot remove, rename or replace the root of a path it writes, so it
// cannot leave a link there for the next run's profile to resolve. What lies
// inside stays its own.
func TestARunCannotReplaceItsWorkspace(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	for _, script := range []string{
		`cd /; rm -rf "$W" && ln -s "$D" "$W"`,
		`cd /; rmdir "$W"`,
		`cd /; mv "$W" "$O/moved"`,
		`mkdir "$O/x" && perl -e 'rename($ARGV[0], $ARGV[1]) or die "$!\n"' "$O/x" "$W"`,
		`ln -s "$D" "$O/l" && perl -e 'rename($ARGV[0], $ARGV[1]) or die "$!\n"' "$O/l" "$W"`,
	} {
		m := standIn(t)
		for _, pair := range [][2]string{{m.ws, m.kubectl}, {m.kubectl, m.ws}} {
			w, other := pair[0], pair[1]
			out, ok := sh(t, s, m.on(s), script, "W="+w, "D="+m.data, "O="+other)
			assert.False(t, ok, "%s on %s: %s", script, w, out)
			info, err := os.Lstat(w)
			require.NoError(t, err, script)
			assert.True(t, info.IsDir(), "%s on %s left %v", script, w, info.Mode())
		}
	}

	m := standIn(t)
	for _, w := range []string{m.ws, m.kubectl} {
		out, ok := sh(t, s, m.on(s), `mkdir "$W/a" && echo x > "$W/a/f" && mv "$W/a" "$W/b" && rm -rf "$W/b" && ln -s /usr "$W/l" && rm "$W/l"`, "W="+w)
		assert.True(t, ok, "%s: %s", w, out)
	}
}

func TestAnAncestorCanBeStatted(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	parent := filepath.Dir(m.ws)

	out, ok := sh(t, s, m.on(s), `stat "$D"`, "D="+parent)
	assert.True(t, ok, out)
	out, ok = sh(t, s, m.on(s), `ls "$D"`, "D="+parent)
	assert.False(t, ok, out)
}

func TestTheEnvironmentIsExactlyGiven(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.Shell = "/usr/bin/env"

	out, err := command(t, s, t.Context(), m.on(s)).Output()

	require.NoError(t, err)
	assert.ElementsMatch(t, m.Env, strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"))
}

// The keychain file is readable inside, so a lookup that fails is the Mach
// denial's.
func TestTheKeychainIsDenied(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	keychain := filepath.Join(m.ws, "k.keychain")
	service := "kubetail-test-" + rand.Text()
	security := func(args ...string) error { return exec.Command("security", args...).Run() }
	require.NoError(t, security("create-keychain", "-p", "pw", keychain))
	// create-keychain adds it to the user's search list; delete-keychain takes it off.
	t.Cleanup(func() { _ = security("delete-keychain", keychain) })
	require.NoError(t, security("add-generic-password", "-a", "kubetail", "-s", service, "-w", "secret", keychain))
	require.NoError(t, security("find-generic-password", "-s", service, keychain), "found outside")

	// A negative assertion bounded at a minute: a lookup that reached the
	// Keychain would answer at once, and the Mach denial is immediate too, but
	// security is a real process and its refusal on a loaded Intel runner has
	// taken over fifteen seconds.
	start := time.Now()
	out, ok, late := shWithin(t, s, m.on(s), time.Minute, `security find-generic-password -s "$S" "$K"`, "S="+service, "K="+keychain)
	if late {
		logDenials(t, start)
	}

	assert.False(t, late, "the lookup did not end within a minute")
	assert.False(t, ok, out)
}

func TestNoAppIsOpenedOrDriven(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	pasteboard := func(stdin string) (string, error) {
		copyCmd := exec.Command("pbcopy")
		copyCmd.Stdin = strings.NewReader(stdin)
		if err := copyCmd.Run(); err != nil {
			return "", err
		}
		out, err := exec.Command("pbpaste").Output()
		return string(out), err
	}
	prev, _ := exec.Command("pbpaste").Output()
	marker := "kubetail-test-" + rand.Text()
	if got, err := pasteboard(marker); err != nil || got != marker {
		t.Skip("no pasteboard to test against outside the sandbox")
	}
	t.Cleanup(func() { _, _ = pasteboard(string(prev)) })

	// Each is a negative assertion bounded at a minute: a profile that let
	// the Apple Event through would wait on a privacy prompt, which never ends,
	// while a refusal on a loaded Intel runner can take over fifteen seconds.
	run := func(script string) (string, bool) {
		start := time.Now()
		out, ok, late := shWithin(t, s, m.on(s), time.Minute, script)
		if late {
			logDenials(t, start)
		}
		assert.False(t, late, "%s did not end within a minute", script)
		return out, ok
	}
	// AppleScript answers an application's own name from its bundle, and
	// terms need Finder's dictionary, so the query is raw codes Finder alone
	// can answer.
	for _, script := range []string{
		"open -g https://example.invalid/",
		`osascript -e 'tell application "Finder" to get «property pnam» of «property sdsk»'`,
		"echo inside | pbcopy",
	} {
		out, ok := run(script)
		assert.False(t, ok, "%s: %s", script, out)
	}
	out, _ := run("pbpaste")
	assert.NotContains(t, out, marker)
	got, err := exec.Command("pbpaste").Output()
	require.NoError(t, err)
	assert.Equal(t, marker, string(got), "pbcopy inside changed nothing")
}

// cfprefsd reads another app's preferences for its caller unless the profile
// refuses user-preference-read.
func TestNoServiceReadsForTheRun(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	domain := "org.kubetail.kubetail.test." + strings.ToLower(rand.Text())
	require.NoError(t, exec.Command("defaults", "write", domain, "k", "kubetail-value").Run())
	t.Cleanup(func() { _ = exec.Command("defaults", "delete", domain).Run() })
	got, err := exec.Command("defaults", "read", domain, "k").Output()
	require.NoError(t, err)
	require.Equal(t, "kubetail-value\n", string(got))

	out, ok := sh(t, s, m.on(s), `defaults read "$D" k`, "D="+domain)

	assert.False(t, ok, out)
	assert.NotContains(t, out, "kubetail-value")
}

// A run reads neither the arguments nor the environment of a process outside
// it, through kern.procargs2. The profile refuses the read only for a process
// in another process group, so this pins that Command starts the run in a
// session of its own. The target is this binary, since macOS 27 withholds an
// Apple binary's environment whatever the profile says.
func TestAnotherProcessesLaunchStringsAreRefused(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	arg := "kubetail-arg-" + rand.Text()
	env := "kubetail-env-" + rand.Text()
	hold, release, err := os.Pipe()
	require.NoError(t, err)
	other := exec.Command(os.Args[0], arg)
	other.Env = []string{holdEnv + "=1", "KUBETAIL_SANDBOX_TEST_SECRET=" + env}
	other.Stdin = hold
	require.NoError(t, other.Start())
	require.NoError(t, hold.Close())
	t.Cleanup(func() { _ = release.Close(); _ = other.Process.Kill(); _ = other.Wait() })
	pid := strconv.Itoa(other.Process.Pid)
	read := exec.Command(os.Args[0])
	read.Env = []string{procArgsEnv + "=" + pid}
	outside, err := read.Output()
	require.NoError(t, err)
	require.Contains(t, string(outside), arg)
	require.Contains(t, string(outside), env)

	// ps is setuid, which no run can start, so the test binary reads what ps
	// would.
	out, ok := sh(t, s, m.on(s), procArgsEnv+`="$P" "$BIN"`, "P="+pid, "BIN="+os.Args[0])

	assert.False(t, ok, out)
	assert.NotContains(t, out, arg, "the arguments are not read")
	assert.NotContains(t, out, env, "the environment is not read")
}

// setuidPrograms are the programs that exist to escalate, which the profile
// refuses to start.
var setuidPrograms = []string{"/usr/bin/sudo", "/usr/bin/su", "/usr/bin/login", "/usr/libexec/security_authtrampoline"}

func TestTheProfileRefusesSetuidPrograms(t *testing.T) {
	t.Parallel()

	text, _ := (&Sandbox{self: "/bin/kubetail-sidecar"}).profile(Run{}, nil)
	allow, deny := strings.Index(text, "(allow process-exec)\n"), strings.Index(text, "(deny process-exec\n")
	require.NotEqual(t, -1, deny, text)
	assert.Less(t, allow, deny)
	for _, p := range setuidPrograms {
		assert.Contains(t, text[deny:], `(literal "`+p+`")`)
	}

	s := confining(t)
	m := standIn(t)
	for _, p := range setuidPrograms {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out, _ := sh(t, s, m.on(s), `"$P" </dev/null; echo "exit=$?"`, "P="+p)
		assert.Contains(t, out, "Operation not permitted", p)
		assert.Contains(t, out, "exit=126", p)
	}
}

// macOS refuses to start a setuid program under any profile, so ps and top,
// setuid root and off the profile's list, are refused as well.
func TestEverySetuidProgramIsRefused(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)

	for _, p := range []string{"/bin/ps", "/usr/bin/top"} {
		out, _ := sh(t, s, m.on(s), `"$P" </dev/null; echo "exit=$?"`, "P="+p)
		assert.Contains(t, out, "Operation not permitted", p)
		assert.Contains(t, out, "exit=126", p)
	}
}

// sample examines another process through its task port, which the profile
// refuses: inside it fails with the line it prints when the port is refused,
// so a sample that cannot start or read a file does not pass.
func TestATaskPortIsRefused(t *testing.T) {
	t.Parallel()

	other := exec.Command("sleep", "60")
	require.NoError(t, other.Start())
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	pid := strconv.Itoa(other.Process.Pid)
	if out, err := exec.Command("/usr/bin/sample", pid, "1").CombinedOutput(); err != nil {
		t.Skip("sample cannot examine a process outside the sandbox: ", string(out))
	}
	s := confining(t)
	m := standIn(t)

	out, ok := sh(t, s, m.on(s), `/usr/bin/sample "$P" 1`, "P="+pid)

	assert.False(t, ok, out)
	assert.Contains(t, out, "cannot examine process "+pid)
}

func TestAPublicNameDoesNotResolve(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	if _, err := net.LookupHost("example.com"); err != nil {
		t.Skip("example.com does not resolve outside the sandbox: ", err)
	}

	// curl tells the outcomes apart itself: a lookup that left the machine
	// would answer, or end in "Resolving timed out" at --max-time.
	out, ok := sh(t, s, m.on(s), "curl -sS --max-time 5 http://example.com/")

	assert.False(t, ok, out)
	assert.Contains(t, out, "Could not resolve host")
}

func TestOnlyTheRunsPortAndSocketAreReached(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.withCluster(t)
	other := serveHTTP(t, "tcp", "127.0.0.1:0", "other")
	otherSocket := filepath.Join(m.ws, "o.sock")
	serveHTTP(t, "unix", otherSocket, "other")

	// Under coverage the forwarder, this test binary, warns as it exits, since
	// its GOCOVERDIR is outside the sandbox, so curl's line is ended and read
	// alone.
	out, ok := sh(t, s, m.on(s), `curl -sS "http://127.0.0.1:$PORT/" && echo`)
	assert.True(t, ok, out)
	assert.Equal(t, "ok", firstLine(out))
	out, ok = sh(t, s, m.on(s), `curl -sS --unix-socket "$SOCKET" http://x/ && echo`)
	assert.True(t, ok, out)
	assert.Equal(t, "ok", firstLine(out))
	out, _ = sh(t, s, m.on(s), `curl -sS "http://$A/"`, "A="+other)
	assert.NotContains(t, out, "other")
	out, _ = sh(t, s, m.on(s), `curl -sS --unix-socket "$F" http://x/`, "F="+otherSocket)
	assert.NotContains(t, out, "other")
}

// The port is reached over TCP on 127.0.0.1 alone, the endpoint the forwarder
// holds: UDP or IPv6 at that number could be another service's.
func TestThePortIsReachedOverIPv4TCPAlone(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.withCluster(t)
	port := strconv.Itoa(m.port)
	for _, target := range []string{"udp4 127.0.0.1:" + port, "tcp6 [::1]:" + port, "udp6 [::1]:" + port} {
		out, ok := sh(t, s, m.on(s), sendEnv+`="$T" "$BIN"`, "T="+target, "BIN="+os.Args[0])

		assert.False(t, ok, target)
		assert.Contains(t, out, "operation not permitted", target)
	}
	out, ok := sh(t, s, m.on(s), sendEnv+`="$T" "$BIN"`, "T=tcp4 127.0.0.1:"+port, "BIN="+os.Args[0])
	assert.True(t, ok, out)
}

func TestARunWithoutAClusterHasNoNetwork(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	addr := serveHTTP(t, "tcp", "127.0.0.1:0", "reached")

	out, _ := sh(t, s, m.on(s), `curl -sS "http://$A/"`, "A="+addr)

	assert.NotContains(t, out, "reached")
}

func TestAPathIsAParameter(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	ws := mkdirs(t, m.data, "chats/we\"ird)\n(allow default)/workspace")[0]
	m.ws, m.Dir = ws, ws
	file := write(t, m.home, "secret")

	out, ok := sh(t, s, m.on(s), "echo x > f && cat f")
	assert.True(t, ok, out)
	assert.Equal(t, "x\n", out)
	out, ok = sh(t, s, m.on(s), `cat "$F"`, "F="+file)
	assert.False(t, ok, out)
}

func TestATakenPortFailsTheRun(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.withCluster(t)
	held, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(m.port)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Close() })

	m.Args = []string{"-c", "echo started"}
	out, err := command(t, s, t.Context(), m.on(s)).CombinedOutput()

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, initFailed, exit.ExitCode())
	assert.Contains(t, string(out), "sandbox-init: cannot listen on 127.0.0.1:"+strconv.Itoa(m.port))
	assert.NotContains(t, string(out), "started")
}

// The profile refuses setsid and setpgid, so a process cannot take itself out
// of the run's group.
func TestAProcessCannotLeaveItsGroup(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	for _, call := range []string{"setsid", "setpgid"} {
		outside, err := exec.Command("/bin/sh", "-c", leaveEnv+`="$1" "$0"`, os.Args[0], call).CombinedOutput()
		require.NoError(t, err, string(outside))
		require.Equal(t, "<nil>\n", string(outside), "%s succeeds outside", call)

		out, ok := sh(t, s, m.on(s), leaveEnv+`="$C" "$BIN"`, "C="+call, "BIN="+os.Args[0])

		assert.True(t, ok, out)
		assert.Equal(t, "operation not permitted\n", out, call)
	}
}

// macOS has no PID namespace, and Seatbelt cannot refuse posix_spawn's
// POSIX_SPAWN_SETSID: a process spawned that way outlives the group's kill,
// and stays under the run's profile.
func TestAProcessThatLeavesTheGroupStaysConfined(t *testing.T) {
	t.Parallel()

	s := confining(t)
	if exec.Command("xcode-select", "-p").Run() != nil {
		t.Skip("no Command Line Tools: /usr/bin/python3 only offers to install them")
	}
	m := standIn(t)
	fifo := filepath.Join(m.ws, "go")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	listener := serveHTTP(t, "tcp", "127.0.0.1:0", "reached")
	m.Env = append(m.Env, homeFileEnv+"="+write(t, m.home, "secret"), listenerEnv+"="+listener, "BIN="+os.Args[0], "W="+m.ws)
	// lingerEnv is set in the command, since the forwarder is this binary too
	// and would linger itself. Its output goes to /dev/null, or the run's pipe
	// stays open behind it.
	m.Args = []string{"-c", `export ` + lingerEnv + `="$W"; exec /usr/bin/python3 -c 'import os, sys
null = [(os.POSIX_SPAWN_OPEN, fd, "/dev/null", os.O_WRONLY, 0) for fd in (1, 2)]
pid = os.posix_spawn(sys.argv[1], [sys.argv[1]], os.environ, file_actions=null, setsid=True)
open("pid", "w").write(str(pid))' "$BIN"`}

	cmd := command(t, s, t.Context(), m.on(s))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	raw, err := os.ReadFile(filepath.Join(m.ws, "pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(raw))
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	// Opening the FIFO waits for linger to open it too. The write end stays
	// open until the test ends: a reader macOS wakes after the last writer has
	// closed goes back to waiting in open, and never returns.
	opened := make(chan *os.File, 1)
	go func() {
		if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			opened <- f
		}
	}()
	f := testutil.Recv(t, opened, "the process left behind opens its FIFO")
	t.Cleanup(func() { _ = f.Close() })
	_, err = f.Write([]byte("x"))
	require.NoError(t, err)
	result := filepath.Join(m.ws, "result")
	require.Eventually(t, func() bool { _, err := os.Stat(result); return err == nil }, testutil.Timeout, 10*time.Millisecond)
	got, err := os.ReadFile(result)
	require.NoError(t, err)
	assert.Equal(t, "denied refused", string(got))
}

// A run started with a controlling terminal still cannot open it. script(1)
// gives its command a terminal of its own, as a sidecar started from one has.
func TestATerminalIsUnreachable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	opened := `: < /dev/tty && echo opened || echo refused`
	outside, err := exec.Command("/usr/bin/script", "-q", "/dev/null", "/bin/sh", "-c", opened).CombinedOutput()
	require.NoError(t, err, string(outside))
	require.Contains(t, string(outside), "opened", "script gives its command a terminal")

	m.Args = []string{"-c", opened}
	run := command(t, s, t.Context(), m.on(s))
	cmd := exec.CommandContext(t.Context(), "/usr/bin/script", slices.Concat([]string{"-q", "/dev/null"}, run.Args)...)
	cmd.Dir, cmd.Env, cmd.ExtraFiles = run.Dir, run.Env, run.ExtraFiles
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), "refused")
	assert.NotContains(t, string(out), "opened")
}

// inlined is a profile's text with each parameter's value in place of its
// name, one line per rule, less the ancestor rules above the fixture.
func (f fixture) inlined(t *testing.T, text string, args []string) []string {
	t.Helper()
	for name, value := range paramsOf(t, args) {
		text = strings.ReplaceAll(text, `(param "`+name+`")`, strconv.Quote(value))
	}
	return slices.DeleteFunc(strings.Split(strings.TrimSuffix(text, "\n"), "\n"), f.aboveFixture)
}

// The profile over the fixture is the golden's, with a cluster and without.
func TestTheCompiledProfileMatchesTheGolden(t *testing.T) {
	// It changes the package's lists, so this test stays serial.
	f := newFixture(t)
	keepLists(t)
	brewVar = f.brewVar
	s := &Sandbox{self: f.self}
	for name, r := range map[string]Run{
		"cluster": f.run(s, true), "no-cluster": f.run(s, false), "internet": f.withInternet(s, f.run(s, true)),
	} {
		t.Run(name, func(t *testing.T) {
			text, args := s.profile(r, compiledRules(t, r.Policy))
			f.golden(t, "profile_darwin_"+name+".golden", f.inlined(t, text, args))
		})
	}
}

// A macOS run resolves through mDNSResponder, so one naming a resolv.conf is
// refused.
func TestAMacOSRunNamesNoResolver(t *testing.T) {
	// newFixture changes the package's lists, so this test stays serial.
	f := newFixture(t)
	s := &Sandbox{self: f.self, launcher: "/usr/bin/sandbox-exec"}
	r := f.withInternet(s, f.run(s, false))
	assert.Empty(t, r.Policy.Network.Resolver)
	r.Policy.Network.Resolver = filepath.Join(f.runDir, "resolv.conf")
	// The fixture's memory limit is refused first.
	r.Policy.Limits.MemoryBytes = 0

	cmd, err := s.Command(t.Context(), r)

	assert.Nil(t, cmd)
	assert.ErrorIs(t, err, errNoResolver)
}

// A policy that fails Check answers its error and no command.
func TestAPolicyThatFailsCheckStartsNothing(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/bin/sh", launcher: "/usr/bin/sandbox-exec"}
	r := Run{Shell: "/bin/sh", Dir: "/w", Policy: Policy{Files: FilePolicy{Read: []string{"relative"}}}}

	cmd, err := s.Command(t.Context(), r)

	assert.Nil(t, cmd)
	assert.Error(t, err)
}

// A Deny holds for a path that does not exist when the profile is made: a
// file made there afterwards cannot be read. The home is under /var, a link,
// so the Deny is resolved through the deepest folder that exists.
func TestADenyHoldsForAPathMadeLater(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	r := m.on(s)
	r.Policy.Files.Read = append(r.Policy.Files.Read, m.home)
	r.Args = []string{"-c", `cat "$F"`}
	r.Env = append(r.Env, "F="+filepath.Join(m.home, ".ssh", "id_ed25519"))

	cmd := command(t, s, t.Context(), r)
	write(t, filepath.Join(m.home, ".ssh"), "id_ed25519")
	out, err := cmd.CombinedOutput()

	assert.Error(t, err)
	assert.NotContains(t, string(out), "secret")
}

// A Read and a Write rule whose paths are missing are skipped: the run
// starts, and it cannot make either path.
func TestAMissingReadOrWritePathIsSkipped(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	read, written := filepath.Join(m.base, "read"), filepath.Join(m.base, "written")
	r := m.on(s)
	r.Policy.Files.Read = append(r.Policy.Files.Read, read)
	r.Policy.Files.Write = append(r.Policy.Files.Write, written)

	out, _ := sh(t, s, r, `mkdir -p "$R" "$W" 2>/dev/null; touch "$R/x" "$W/x" 2>/dev/null; echo started`, "R="+read, "W="+written)

	assert.Equal(t, "started\n", out)
	assert.NoDirExists(t, read)
	assert.NoDirExists(t, written)
}

// The probe's run writes its own folder and reads its shell: the profile it
// hands the launcher names both.
func TestTheProbeHandsTheLauncherItsPolicy(t *testing.T) {
	t.Parallel()

	check := `printf '%s\n' "$@" | grep -q '^RULE_[0-9]*=.*kubetail-probe-' || { echo 'no probe folder' >&2; exit 1; }
printf '%s\n' "$@" | grep -q '^RULE_[0-9]*=/usr$' || { echo 'no /usr' >&2; exit 1; }
`
	s, v := probe(t.Context(), launcher(t, check+passThrough), testutil.Timeout)

	require.NotNil(t, s, v.Reason)
	closing(t, s)
	assert.True(t, v.Available)
}

// Command refuses a run holding a variable no run may hold.
func TestARunWithAnUnpassableVariableIsRefused(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/bin/sh", launcher: "/usr/bin/sandbox-exec"}
	for _, kv := range []string{"LD_PRELOAD=/x.so", "AWS_SESSION_TOKEN=t"} {
		cmd, err := s.Command(t.Context(), Run{Shell: "/bin/sh", Dir: "/", Env: []string{kv}})

		assert.Nil(t, cmd, kv)
		assert.ErrorContains(t, err, "may not pass", kv)
	}
}

// A probe whose context ended before it answered is the context's error, not
// a verdict: read as one it would say there is no sandbox.
func TestAProbeCutShortIsAnError(t *testing.T) {
	// It waits on the package's boundedCalls, which every probe adds to, so this test stays serial.
	t.Cleanup(boundedCalls.Wait)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s, _, err := Probe(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, s)
}

// hostAddress is an IPv4 address the host holds other than loopback and
// link-local. A CI runner can lose its address for a moment, as a DHCP lease
// lapses, so it waits up to hostAddressWait for one.
func hostAddress(t *testing.T) string {
	t.Helper()
	var host string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		addrs, err := net.InterfaceAddrs()
		require.NoError(c, err)
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() && !ip.IP.IsLinkLocalUnicast() {
				host = ip.IP.String()
				return
			}
		}
		assert.Fail(c, "the host holds no address but loopback and link-local", "held: %v", addrs)
	}, hostAddressWait, 250*time.Millisecond)
	return host
}

// hostAddressWait is how long hostAddress waits, long enough for a DHCP
// client to renew a lapsed lease.
const hostAddressWait = time.Minute

// curlConnect is a curl that says why a connect failed: -v names the error
// on a line of its own, which curl words differently across versions.
const curlConnect = `curl -sS -v --max-time 5 "http://$A/"`

// sandboxRefused reports whether curl -v's output says a connect failed with
// EPERM, which is what a Seatbelt deny answers.
func sandboxRefused(out string) bool {
	for line := range strings.Lines(out) {
		if strings.Contains(line, "connect") && strings.Contains(line, "Operation not permitted") {
			return true
		}
	}
	return false
}

// Without the internet a run reaches neither an address of the host's nor a
// public one.
func TestWithoutTheInternetNothingIsReached(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	addr := serveHTTP(t, "tcp", net.JoinHostPort(hostAddress(t), "0"), "reached")

	out, _ := sh(t, s, m.on(s), `curl -sS --max-time 5 "http://$A/"`, "A="+addr)
	assert.NotContains(t, out, "reached")
	out, ok := sh(t, s, m.on(s), `curl -sS --max-time 5 http://192.0.2.1/`)
	assert.False(t, ok, out)
}

// With the internet a connect leaves the run. Nothing answers a TEST-NET
// address (RFC 5737), so the connect fails either way, but only the run
// without the internet is refused by the sandbox.
func TestTheInternetReachesOut(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	connect := `curl -sS -v --connect-timeout 2 "http://$A/"`

	out, ok := sh(t, s, m.on(s), connect, "A=192.0.2.1")
	assert.False(t, ok, out)
	assert.True(t, sandboxRefused(out), "without the internet: %s", out)

	m.internet = true
	out, ok = sh(t, s, m.on(s), connect, "A=192.0.2.1")
	assert.False(t, ok, out)
	assert.False(t, sandboxRefused(out), "with the internet: %s", out)
}

// With the internet the host's own addresses stay shut, as its loopback does,
// IPv4-mapped included.
func TestTheInternetReachesNoHostAddress(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.internet = true
	host := hostAddress(t)
	addr := serveHTTP(t, "tcp", net.JoinHostPort(host, "0"), "reached")
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	for _, addr := range []string{addr, net.JoinHostPort("::ffff:"+host, port)} {
		out, _ := sh(t, s, m.on(s), curlConnect, "A="+addr)
		assert.NotContains(t, out, "reached", addr)
		assert.True(t, sandboxRefused(out), "%s: %s", addr, out)
	}
}

// With the internet a run still has no IPv6: the sandbox refuses a public
// address (RFC 3849) as it does the host's loopback.
func TestTheInternetIsIPv4Only(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.internet = true

	for _, addr := range []string{"[2001:db8::1]:80", "[::1]:80"} {
		out, _ := sh(t, s, m.on(s), curlConnect, "A="+addr)
		assert.True(t, sandboxRefused(out), "%s: %s", addr, out)
	}
}

// With the internet the host's loopback stays shut, IPv4 and IPv6, under every
// address the kernel connects to it, while the run's relay port still reaches
// the forwarder.
func TestTheInternetReachesNoHostLoopback(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	m.withCluster(t)
	m.internet = true
	// The kernel takes the unspecified address for loopback, and an
	// IPv4-mapped one for the IPv4 address it maps.
	v4 := serveHTTP(t, "tcp", "127.0.0.1:0", "loopback")
	_, port, err := net.SplitHostPort(v4)
	require.NoError(t, err)
	targets := []string{v4}
	for _, host := range []string{"0.0.0.0", "::ffff:127.0.0.1", "::ffff:0.0.0.0"} {
		targets = append(targets, net.JoinHostPort(host, port))
	}
	if ln, err := net.Listen("tcp", "[::1]:0"); err == nil {
		_ = ln.Close()
		v6 := serveHTTP(t, "tcp", "[::1]:0", "loopback")
		_, port, err := net.SplitHostPort(v6)
		require.NoError(t, err)
		targets = append(targets, v6, net.JoinHostPort("::", port))
	}

	for _, addr := range targets {
		out, _ := sh(t, s, m.on(s), curlConnect, "A="+addr)
		assert.NotContains(t, out, "loopback", addr)
		assert.True(t, sandboxRefused(out), "%s: %s", addr, out)
	}
	out, ok := sh(t, s, m.on(s), `curl -sS "http://127.0.0.1:$PORT/" && echo`)
	assert.True(t, ok, out)
	assert.Equal(t, "ok", firstLine(out))
}

// With the internet a run reaches mDNSResponder: its socket connects, and a
// name only mDNSResponder answers resolves. Without it the socket is refused.
// A failed lookup says nothing of why: libSystem reports a resolver it cannot
// reach as a name that does not exist.
func TestTheInternetResolves(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	name, addr := mdnsName(t)
	env := []string{"BIN=" + os.Args[0], "NAME=" + name}
	dial := `KUBETAIL_SANDBOX_TEST_HELPER=dial-resolver "$BIN"`
	lookup := `KUBETAIL_SANDBOX_TEST_HELPER=lookup KUBETAIL_SANDBOX_TEST_NAME="$NAME" "$BIN"`

	out, _ := sh(t, s, m.on(s), dial, env...)
	assert.Equal(t, "refused", firstLine(out), "without the internet")
	out, _ = sh(t, s, m.on(s), lookup, env...)
	assert.Equal(t, "not-found", firstLine(out), "without the internet")

	m.internet = true
	out, _ = sh(t, s, m.on(s), dial, env...)
	assert.Equal(t, "connected", firstLine(out), "with the internet")
	out, _ = sh(t, s, m.on(s), lookup, env...)
	first, _, _ := strings.Cut(firstLine(out), ",")
	assert.Equal(t, addr, first, "with the internet: %s", out)
}

// mdnsName registers a .local name, at a TEST-NET address, that mDNSResponder
// alone answers, for the test's life, and returns once it resolves: "Name now
// registered and active" does not promise the next lookup sees it. The host's
// own name will not do: on GitHub's runners, whose HostName is set, a run
// without the internet resolves it.
func mdnsName(t *testing.T) (name, addr string) {
	t.Helper()
	name = "kubetail-test-" + strings.ToLower(rand.Text()) + ".local"
	addr = "192.0.2.7"
	cmd := exec.Command("/usr/bin/dns-sd", "-P", "kubetail-test", "_kubetail-test._tcp", "local", "9", name, addr)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	registered := make(chan struct{})
	go func() {
		lines := bufio.NewScanner(out)
		for lines.Scan() {
			if strings.Contains(lines.Text(), name+": Name now registered and active") {
				close(registered)
				break
			}
		}
		_, _ = io.Copy(io.Discard, out)
	}()
	testutil.Wait(t, registered, name+"'s registration")
	resolves := func() bool {
		addrs, err := net.LookupHost(name)
		return err == nil && slices.Contains(addrs, addr)
	}
	require.Eventually(t, resolves, testutil.Timeout, 50*time.Millisecond, "%s never resolved", name)
	return name, addr
}

func init() {
	// dial-resolver connects to mDNSResponder's socket and prints connected,
	// refused for EPERM, or the error.
	helpers["dial-resolver"] = func() int {
		c, err := net.Dial("unix", "/private/var/run/mDNSResponder")
		switch {
		case errors.Is(err, syscall.EPERM):
			fmt.Print("refused")
			return 1
		case err != nil:
			fmt.Print(err)
			return 1
		}
		_ = c.Close()
		fmt.Print("connected")
		return 0
	}

	// verify verifies the first certificate in KUBETAIL_SANDBOX_TEST_CERT, the
	// rest as its intermediates, against the system's roots: at
	// KUBETAIL_SANDBOX_TEST_AT when set, for KUBETAIL_SANDBOX_TEST_HOST when set.
	// It prints verified, or why not.
	helpers["verify"] = func() int {
		raw, err := os.ReadFile(os.Getenv("KUBETAIL_SANDBOX_TEST_CERT"))
		if err != nil {
			fmt.Print(err)
			return 1
		}
		var certs []*x509.Certificate
		for block, rest := pem.Decode(raw); block != nil; block, rest = pem.Decode(rest) {
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				fmt.Print(err)
				return 1
			}
			certs = append(certs, c)
		}
		opts := x509.VerifyOptions{DNSName: os.Getenv("KUBETAIL_SANDBOX_TEST_HOST"), Intermediates: x509.NewCertPool()}
		for _, c := range certs[1:] {
			opts.Intermediates.AddCert(c)
		}
		if at := os.Getenv("KUBETAIL_SANDBOX_TEST_AT"); at != "" {
			opts.CurrentTime, _ = time.Parse(time.RFC3339, at)
		}
		if _, err := certs[0].Verify(opts); err != nil {
			fmt.Print(err)
			return 1
		}
		fmt.Print("verified")
		return 0
	}
}

// trustChainCaptured is when testdata/trust_chain.pem was captured, a
// moment its leaf was valid, so the fixture never expires.
const trustChainCaptured = "2026-10-04T05:55:13Z"

// aiaLeaf writes a leaf signed by an issuer the system does not hold, whose
// issuer and OCSP URLs name base, and answers its path.
func aiaLeaf(t *testing.T, dir, base string) string {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kubetail test issuer"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err = x509.ParseCertificate(caDER)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "kubetail.test"}, DNSNames: []string{"kubetail.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IssuingCertificateURL: []string{base + "/issuer.cer"}, OCSPServer: []string{base + "/ocsp"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	path := filepath.Join(dir, "leaf.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

// The trust daemon fetches a certificate's issuer and OCSP URLs from outside
// the sandbox, so a run without the internet is refused it: a leaf whose
// issuer is missing, and one checked for revocation, make the listener its
// URLs name see no request. With the internet a chain verifies, the daemon's
// own work.
func TestTrustFetchesNothingWithoutTheInternet(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	fetched := make(chan string, 8)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { fetched <- r.URL.Path }),
		ReadHeaderTimeout: testutil.Timeout,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	leaf := aiaLeaf(t, m.ws, "http://"+ln.Addr().String())

	out, ok := sh(t, s, m.on(s), `KUBETAIL_SANDBOX_TEST_HELPER=verify KUBETAIL_SANDBOX_TEST_CERT="$C" "$BIN"`, "C="+leaf, "BIN="+os.Args[0])
	assert.False(t, ok, out)
	out, _ = sh(t, s, m.on(s), `security verify-cert -c "$C" -R ocsp 2>&1`, "C="+leaf)
	t.Log("security verify-cert: ", out)
	// A negative assertion: the runs have ended, and a fetch the daemon made
	// for them would have landed by now; the window covers one still in flight.
	select {
	case path := <-fetched:
		assert.Fail(t, "the trust daemon fetched "+path+" for a run without the internet")
	case <-time.After(2 * time.Second):
	}

	chain, err := filepath.Abs(filepath.Join("testdata", "trust_chain.pem"))
	require.NoError(t, err)
	copied := filepath.Join(m.ws, "chain.pem")
	raw, err := os.ReadFile(chain)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(copied, raw, 0o600))
	m.internet = true
	out, ok = sh(t, s, m.on(s), `KUBETAIL_SANDBOX_TEST_HELPER=verify KUBETAIL_SANDBOX_TEST_CERT="$C" KUBETAIL_SANDBOX_TEST_HOST=www.google.com KUBETAIL_SANDBOX_TEST_AT="$AT" "$BIN"`,
		"C="+copied, "AT="+trustChainCaptured, "BIN="+os.Args[0])
	assert.True(t, ok, out)
	assert.Equal(t, "verified", firstLine(out))
}

func TestFixedMountIsEveryRunsOwnMount(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"/dev", "/dev/fd"} {
		assert.True(t, FixedMount(p), p)
	}
	for _, p := range []string{"/", "/tmp", "/proc", "/Users/me", "/devices"} {
		assert.False(t, FixedMount(p), p)
	}
}

// A run ends when the sidecar's lifeline closes, foreground or background:
// Seatbelt has nothing that ends a run with its parent, and the run leads a
// session of its own, so no signal to the sidecar's group reaches it.
func TestARunEndsWithTheLifeline(t *testing.T) {
	t.Parallel()

	s := confining(t)
	m := standIn(t)
	r := m.on(s)
	r.Args = []string{"-c", "sleep 1000 & echo up; wait"}
	cmd := command(t, s, t.Context(), r)

	requireEndsWithTheLifeline(t, cmd, s)
	assert.Equal(t, 128+int(syscall.SIGKILL), ExitCode(cmd.ProcessState), "the kill reached the forwarder too")
	require.Eventually(t, func() bool { return syscall.Kill(-cmd.Process.Pid, 0) == syscall.ESRCH }, testutil.Timeout, 10*time.Millisecond)
}

// A closed Sandbox makes no command, so no run starts without the lifeline.
func TestAClosedSandboxMakesNoCommand(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := lifeline(t)
	s.self, s.launcher = "/bin/kubetail-sidecar", "/usr/bin/sandbox-exec"
	r, _ := profileRun(t, s)
	require.NoError(t, s.Close())

	_, err := s.Command(t.Context(), r)

	assert.ErrorIs(t, err, errClosed)
}

// A probed sandbox hands every run the lifeline, and its forwarder watches it.
func TestTheForwarderIsToldOfTheLifeline(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := lifeline(t)
	s.self, s.launcher = "/bin/kubetail-sidecar", "/usr/bin/sandbox-exec"
	r, _ := profileRun(t, s)
	r.Args = []string{"-c", "exit 0"}

	cmd := command(t, s, t.Context(), r)

	assert.Equal(t, []*os.File{s.lifeline}, cmd.ExtraFiles)
	assert.Equal(t, []string{"/bin/kubetail-sidecar", InitCommand, lifelineFlag, "3", "--", "/bin/kubetail-sidecar", ShellCommand, "--", "/bin/sh", "-c", "exit 0"},
		cmd.Args[len(cmd.Args)-11:])
}

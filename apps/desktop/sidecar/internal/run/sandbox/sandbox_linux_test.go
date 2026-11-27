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
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// The first system copy that exists, in the list's order, comes first; then
// Kubetail's own, beside its executable; none where none exists.
func TestTheSystemCopyIsFoundFirst(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	bin := filepath.Join(base, "usr", "bin")
	system := []string{filepath.Join(base, "a", "bwrap"), filepath.Join(base, "b", "bwrap"), filepath.Join(base, "c", "bwrap")}
	write(t, system[1], "")
	write(t, system[2], "")

	assert.Empty(t, programPaths(bin, "bwrap", system[:1]))
	assert.Equal(t, []string{system[1]}, programPaths(bin, "bwrap", system))

	own := write(t, filepath.Join(base, "usr", "lib", "kubetail-desktop", "bwrap"), "")
	assert.Equal(t, []string{system[1], own}, programPaths(bin, "bwrap", system))
	assert.Equal(t, []string{own}, programPaths(bin, "bwrap", system[:1]))
	assert.Empty(t, programPaths(bin, "pasta", system[:1]), "Kubetail's own is found by its name")
}

// seq reports whether args holds want as a run of consecutive arguments.
func seq(args []string, want ...string) bool {
	for i := range args {
		if i+len(want) <= len(args) && slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// A run starts bwrap with the run's environment, its namespaces first and the
// chain last: the forwarder, then the shell launcher, then the shell.
func TestTheCommandStartsBwrapOverTheChain(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "opt/k", "w/sub", "c/tmp/1", "c/kubectl", "run/r1")
	self := write(t, filepath.Join(d[0], "kubetail-sidecar"), "")
	snap := write(t, filepath.Join(base, "snap.sh"), "")
	w := filepath.Join(base, "w")
	socket := filepath.Join(d[4], "proxy.sock")
	s := &Sandbox{self: self, bwrap: "/usr/bin/bwrap"}
	r := Run{
		Shell: "/bin/bash", Args: []string{"-c", "ls"}, Dir: d[1], Env: []string{"A=1"},
		Policy: Policy{
			Files:   FilePolicy{Read: []string{snap, d[4], self}, Write: []string{w, d[2], d[3]}},
			Network: NetworkPolicy{Relays: []Relay{{Port: 6443, Socket: socket}}},
		},
	}

	cmd := command(t, s, context.Background(), r)

	assert.Equal(t, "/usr/bin/bwrap", cmd.Path)
	assert.Equal(t, []string{"A=1"}, cmd.Env)
	args := cmd.Args[1:]
	assert.Equal(t, []string{
		"--unshare-user-try", "--unshare-net", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try",
		"--die-with-parent", "--new-session", "--as-pid-1",
	}, args[:9])
	assert.True(t, seq(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp"), args)
	for _, p := range []string{snap, d[4], self} {
		assert.True(t, seq(args, "--ro-bind", p, p), p)
	}
	for _, p := range []string{w, d[2], d[3]} {
		assert.True(t, seq(args, "--bind", p, p), p)
	}
	assert.True(t, seq(args, "--remount-ro", "/"), args)
	assert.Equal(t, []string{
		"--chdir", d[1], "--",
		self, InitCommand, "--socket", socket, "--port", "6443", "--",
		self, ShellCommand, "--", "/bin/bash", "-c", "ls",
	}, args[len(args)-16:])
}

// A run with no cluster names no socket, and its forwarder listens on nothing.
func TestARunWithNoSocketStartsTheForwarderWithNoFlags(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}

	args := command(t, s, context.Background(), Run{Shell: "/bin/sh", Args: []string{"-c", "true"}, Dir: "/w"}).Args

	assert.Equal(t, []string{"--", "/k", InitCommand, "--", "/k", ShellCommand, "--", "/bin/sh", "-c", "true"}, args[len(args)-10:])
	assert.False(t, slices.Contains(args, "--socket"))
}

// mounts is the arguments that mount r's rules: those after the fixed mounts
// and before the closing remounts.
func mounts(t *testing.T, s *Sandbox, r Run) []string {
	t.Helper()
	args := command(t, s, context.Background(), r).Args
	from := index(args, "--tmpfs", "/tmp") + 2
	return args[from:index(args, "--remount-ro", "/")]
}

// Each Read rule that exists is bound read-only at its resolved path, and one
// that does not, or links to nothing, is left out. A rule that is a link is
// recreated as one, and its target is bound unless a Read rule holds it.
func TestTheReadRulesAreBound(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "usr/bin", "var/opt")
	usr, varOpt := filepath.Join(base, "usr"), d[1]
	bin := filepath.Join(base, "bin")
	require.NoError(t, os.Symlink("usr/bin", bin))
	opt := filepath.Join(base, "opt")
	require.NoError(t, os.Symlink(varOpt, opt))
	dangling := filepath.Join(base, "lib64")
	require.NoError(t, os.Symlink("usr/lib64", dangling))
	missing := filepath.Join(base, "lib32")
	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}

	got := mounts(t, s, Run{Shell: "/bin/sh", Dir: usr, Policy: Policy{Files: FilePolicy{Read: []string{usr, bin, opt, dangling, missing}}}})

	assert.Equal(t, []string{
		"--ro-bind", usr, usr,
		"--symlink", d[0], bin,
		"--ro-bind", varOpt, varOpt, "--symlink", varOpt, opt,
	}, got)
}

// fakeBwrap is a script standing in for bwrap at path, running body.
func fakeBwrap(t *testing.T, path, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700))
	return path
}

func TestWithoutBwrapThereIsNoSandbox(t *testing.T) {
	t.Parallel()

	s, v := probe(t.Context(), os.Args[0], nil, time.Minute)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "bwrap not found"}, v)
}

// With no descriptor free, the probe has no lifeline to hand a run, so no
// sandbox. Serial: it lowers this process's open-files limit.
func TestAProbeWithNoDescriptorFreeHasNoSandbox(t *testing.T) {
	bwrap := fakeBwrap(t, filepath.Join(t.TempDir(), "bwrap"), "exit 0")
	openFilesLimit(t, 3)

	s, v := probe(t.Context(), os.Args[0], []string{bwrap}, time.Minute)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: "cannot make the runs' lifeline"}, v)
}

// A bwrap that fails the probe is no sandbox, and the reason is the first
// line it wrote, which names the cause.
func TestAProbeThatFailsSaysWhy(t *testing.T) {
	t.Parallel()

	bwrap := fakeBwrap(t, filepath.Join(t.TempDir(), "bwrap"),
		`echo "bwrap: setting up uid map: Permission denied" >&2; echo more >&2; exit 1`)

	s, v := probe(t.Context(), os.Args[0], []string{bwrap}, time.Minute)

	assert.Nil(t, s)
	assert.Equal(t, State{Reason: bwrap + ": bwrap: setting up uid map: Permission denied"}, v)
}

// A probe past its bound fails, saying so, and ends what the fake started.
func TestAProbePastItsBoundFails(t *testing.T) {
	// It waits on the package's boundedCalls, which every probe adds to, so this test stays serial.
	// Latency injected into the code under test: the fake outlasts the bound,
	// and its child holds the probe's pipes as it does.
	token := runToken(t)
	bwrap := fakeBwrap(t, filepath.Join(t.TempDir(), "bwrap"), token+" sleep 600 & exec sleep 600")
	t.Cleanup(func() {
		for pid := range processesOf(token) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	t.Cleanup(boundedCalls.Wait)
	var s *Sandbox
	var v State
	done := make(chan struct{})

	go func() {
		defer close(done)
		s, v = probe(t.Context(), os.Args[0], []string{bwrap}, 500*time.Millisecond)
	}()

	testutil.Wait(t, done, "the probe")
	assert.Nil(t, s)
	assert.Equal(t, State{Reason: bwrap + ": no answer in 500ms"}, v)
	assert.Eventually(t, func() bool { return len(processesOf(token)) == 0 }, testutil.Timeout, 10*time.Millisecond,
		"processes left: %v", processesOf(token))
}

// When the system's bwrap fails, Kubetail's own is probed in its place, and
// both reasons are named when both fail; one that passes is never replaced.
func TestKubetailsOwnBwrapStandsInForTheSystems(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "own-ran")
	failing := fakeBwrap(t, filepath.Join(dir, "system-failing"), `echo "system failed" >&2; exit 1`)
	passing := fakeBwrap(t, filepath.Join(dir, "system-passing"), "exit 0")
	own := fakeBwrap(t, filepath.Join(dir, "own"), "touch "+marker)
	ownFailing := fakeBwrap(t, filepath.Join(dir, "own-failing"), `echo "own failed" >&2; exit 1`)

	s, v := probe(t.Context(), os.Args[0], []string{failing, own}, time.Minute)
	require.NotNil(t, s)
	closing(t, s)
	assert.Equal(t, own, s.bwrap)
	assert.Equal(t, State{Available: true, Reason: "bwrap at " + own}, v)

	require.NoError(t, os.Remove(marker))
	s, _ = probe(t.Context(), os.Args[0], []string{passing, own}, time.Minute)
	require.NotNil(t, s)
	closing(t, s)
	assert.Equal(t, passing, s.bwrap)
	assert.NoFileExists(t, marker)

	s, v = probe(t.Context(), os.Args[0], []string{failing, ownFailing}, time.Minute)
	assert.Nil(t, s)
	assert.Equal(t, State{Reason: failing + ": system failed; " + ownFailing + ": own failed"}, v)
}

// A probe with nowhere to make its workspace fails, saying why.
func TestAProbeWithNoTempDirFails(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	bwrap := fakeBwrap(t, filepath.Join(t.TempDir(), "bwrap"), "exit 0")

	s, v := probe(t.Context(), os.Args[0], []string{bwrap}, time.Minute)

	assert.Nil(t, s)
	assert.Contains(t, v.Reason, "no such file or directory")
}

// A bwrap that fails without a word is named by how it exited.
func TestAProbeThatFailsSilentlySaysHowItExited(t *testing.T) {
	t.Parallel()

	bwrap := fakeBwrap(t, filepath.Join(t.TempDir(), "bwrap"), "exit 3")

	_, v := probe(t.Context(), os.Args[0], []string{bwrap}, time.Minute)

	assert.Equal(t, State{Reason: bwrap + ": exit status 3"}, v)
}

// run starts r through s and answers its exit code and its stdout. Stderr
// is left out of the answer: under coverage the forwarder, this test binary,
// warns there as it exits, since its GOCOVERDIR is outside the sandbox.
func run(t *testing.T, s *Sandbox, r testRun) (int, string) {
	t.Helper()
	cmd := command(t, s, t.Context(), r.on(s))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if cmd.ProcessState == nil {
		require.NoError(t, err)
	}
	t.Log("stderr: ", stderr.String())
	return ExitCode(cmd.ProcessState), string(out)
}

// testRun is a run as a test lays it out: the Run, the home its policy's
// System and Never paths are read under, the workspace, and the Files and
// Always rules the test adds.
type testRun struct {
	Run
	home, ws string
	files    FilePolicy
	always   AlwaysPolicy
}

// on is r as it runs on s, its policy built as Bash builds one: System less
// what lies in r's Kubetail paths, then r's own Files rules, and the Never paths
// denied beside r's Always part.
func (r testRun) on(s *Sandbox) Run {
	files := s.System(r.home, r.Shell).Files.Outside(r.always.Kubetail...)
	files.Read = append(files.Read, r.files.Read...)
	files.Write = append(files.Write, r.files.Write...)
	files.Deny = append(files.Deny, r.files.Deny...)
	always := r.always
	always.Deny = append(s.Never(r.home), always.Deny...)
	r.Policy = Policy{Files: files, Always: always, Network: r.Policy.Network}
	return r.Run
}

// shRun is a run of script under /bin/sh in a fresh workspace it writes, with
// a PATH of the system's and the environment env adds.
func shRun(t *testing.T, script string, env ...string) testRun {
	t.Helper()
	ws := realpath.Resolved(t.TempDir())
	return testRun{
		Run:  Run{Shell: "/bin/sh", Args: []string{"-c", script}, Dir: ws, Env: append([]string{"PATH=/usr/bin:/bin"}, env...)},
		home: realpath.Resolved(t.TempDir()), ws: ws, files: FilePolicy{Write: []string{ws}},
	}
}

// The command sees exactly the environment it was given: nothing the
// forwarder, the shell launcher or bwrap has of its own.
func TestTheEnvironmentIsExactlyGiven(t *testing.T) {
	t.Parallel()

	s := confining(t)

	code, out := run(t, s, shRun(t, "env | grep -v -e ^PWD= -e ^SHLVL= -e ^_= | sort", "ONLY=this"))

	assert.Equal(t, 0, code, out)
	assert.Equal(t, "ONLY=this\nPATH=/usr/bin:/bin\n", out)
}

// bwrap confines, and a run's forwarder listens on one fixed port, since the
// namespace's loopback is the run's own.
func TestBwrapConfinesOnAFixedPort(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}

	port, err := s.Port()

	require.NoError(t, err)
	assert.Equal(t, forwarderPort, port)
	assert.True(t, s.Confines())
}

// index is where want starts as a run in args, or -1.
func index(args []string, want ...string) int {
	for i := range args {
		if i+len(want) <= len(args) && slices.Equal(args[i:i+len(want)], want) {
			return i
		}
	}
	return -1
}

// The mounts come in the order that lets each lie over the last: the Files
// rules, shallowest first and on one path the narrower last, then every Always
// path over them, then the run's own, then the read-only remounts, which come
// after the binds made inside a denial. A rule on / goes under the fixed
// mounts.
func TestTheCommandMountsInOrder(t *testing.T) {
	t.Parallel()

	d := mkdirs(t, realpath.Resolved(t.TempDir()), "home/ana", "tools")
	home, outside := d[0], d[1]
	cargo := mkdirs(t, home, ".cargo/bin")[0]
	tree := filepath.Dir(cargo)
	creds := filepath.Join(tree, "credentials.toml")
	require.NoError(t, os.WriteFile(creds, nil, 0o600))
	data := mkdirs(t, home, ".cargo/kubetail-data")[0]
	ws := mkdirs(t, data, "chats/1/workspace")[0]
	tool := mkdirs(t, outside, "tool/bin")[0]
	tie := mkdirs(t, outside, "tie")[0]
	runtime := mkdirs(t, outside, "runtime")[0]
	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}
	r := Run{
		Shell: "/bin/sh", Args: []string{"-c", "true"}, Dir: ws,
		Policy: Policy{
			Files:  FilePolicy{Read: []string{tree, tool, tie}, Deny: []string{cargo, tie}},
			Always: AlwaysPolicy{Deny: []string{creds}, Kubetail: []string{data, runtime}, Write: []string{ws}},
		},
	}

	args := command(t, s, context.Background(), r).Args

	at := map[string]int{
		"fixed":   index(args, "--tmpfs", "/tmp"),
		"tree":    index(args, "--ro-bind", tree, tree),
		"bin":     index(args, "--tmpfs", cargo),
		"tieRead": index(args, "--ro-bind", tie, tie),
		"tieDeny": index(args, "--tmpfs", tie),
		"cred":    index(args, "--ro-bind", "/dev/null", creds),
		"data":    index(args, "--tmpfs", data),
		"own":     index(args, "--bind", ws, ws),
		"root":    index(args, "--remount-ro", "/"),
		"sealed":  index(args, "--remount-ro", data),
	}
	for name, i := range at {
		require.NotEqual(t, -1, i, "%s missing from %v", name, args)
	}
	assert.Less(t, at["fixed"], at["tree"])
	assert.Less(t, at["tree"], at["bin"])
	assert.Less(t, at["tieRead"], at["tieDeny"])
	for _, files := range []string{"bin", "tieDeny"} {
		assert.Less(t, at[files], at["cred"])
	}
	assert.Less(t, at["cred"], at["data"])
	assert.Less(t, at["data"], at["own"])
	assert.Less(t, at["own"], at["root"])
	assert.Less(t, at["root"], at["sealed"])
	assert.Equal(t, -1, index(args, "--tmpfs", runtime), "a denial no Read or Write reaches needs no mount")

	r.Policy = Policy{Files: FilePolicy{Read: []string{"/"}}}
	args = command(t, s, context.Background(), r).Args
	assert.Less(t, index(args, "--ro-bind", "/", "/"), index(args, "--proc", "/proc"), "a rule on / goes under the fixed mounts")
}

// A root with a link above it is bound at its resolved path and recreated as
// a link at its own, as Homebrew's is under Fedora Atomic's linked /home.
func TestARootBelowALinkIsBoundResolved(t *testing.T) {
	t.Parallel()

	base := realpath.Resolved(t.TempDir())
	brew := mkdirs(t, base, "var/home/linuxbrew/.linuxbrew")[0]
	require.NoError(t, os.Symlink(filepath.Join(base, "var", "home"), filepath.Join(base, "home")))
	root := filepath.Join(base, "home", "linuxbrew", ".linuxbrew")

	got := mounts(t, &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}, Run{Shell: "/bin/sh", Dir: brew, Policy: Policy{Files: FilePolicy{Read: []string{root}}}})

	assert.Equal(t, []string{"--ro-bind", brew, brew, "--symlink", brew, root}, got)
}

// A Read granted through a link inside a denied folder reads, whether the
// denial is mounted before its target or after: the denial's tmpfs hides the
// tree's link, so the link is recreated in it. The rest of the folder stays
// denied.
func TestALinkInsideADenialLeadsToItsRead(t *testing.T) {
	t.Parallel()

	s := confining(t)
	for name, c := range map[string]struct{ target, denied string }{
		"a denial above the target": {"target/deep", "denied"},
		"a denial below the target": {"target", "denied/sub/deeper"},
	} {
		t.Run(name, func(t *testing.T) {
			base := realpath.Resolved(t.TempDir())
			target := filepath.Join(base, c.target)
			write(t, filepath.Join(target, "f"), "granted")
			denied := filepath.Join(base, c.denied)
			write(t, filepath.Join(denied, "secret"), "secret")
			link := filepath.Join(denied, "link")
			require.NoError(t, os.Symlink(target, link))
			r := shRun(t, "cat "+filepath.Join(link, "f")+"; cat "+filepath.Join(denied, "secret"))
			r.files.Read = []string{base, link}
			r.files.Deny = []string{denied}

			_, out := run(t, s, r)

			assert.Contains(t, out, "granted")
			assert.NotContains(t, out, "secret")
		})
	}
}

// A Read under /tmp reads beside a Read on /, whose bind the run's fresh /tmp
// hides.
func TestAReadUnderTmpReadsBesideARootRead(t *testing.T) {
	t.Parallel()

	s := confining(t)
	dir, err := os.MkdirTemp("/tmp", "kubetail-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := write(t, filepath.Join(dir, "f"), "granted")
	r := shRun(t, "cat "+f)
	r.files.Read = []string{"/", dir}

	_, out := run(t, s, r)

	assert.Equal(t, "granted", out)
}

// write makes a file at path holding text, and its directory.
func write(t *testing.T, path, text string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o700))
	return path
}

// program makes an executable script at path that prints name.
func program(t *testing.T, path, name string) string {
	t.Helper()
	return write(t, path, "#!/bin/sh\necho "+name+"\n")
}

// withPath is r with PATH set to the system's then entries. PATH opens
// nothing: an entry finds its programs only where a list reads them.
func withPath(r testRun, entries ...string) testRun {
	r.Env = append([]string{"PATH=" + strings.Join(append([]string{"/usr/bin", "/bin"}, entries...), ":")}, r.Env[1:]...)
	return r
}

func TestTheHomeIsUnreadable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "")
	write(t, filepath.Join(r.home, "secret"), "secret")
	r.Args[1] = "cat " + filepath.Join(r.home, "secret") + "; ls -A " + r.home

	_, out := run(t, s, r)

	assert.NotContains(t, out, "secret")
}

func TestACredentialPathInsideAReadableTreeIsUnreadable(t *testing.T) {
	// addToolchain changes the package's lists, so this test stays serial.
	s := confining(t)
	r := shRun(t, "")
	program(t, filepath.Join(r.home, ".cargo", "bin", "tool"), "tool-ran")
	creds := write(t, filepath.Join(r.home, ".cargo", "credentials.toml"), "secret")
	addToolchain(t, "~/.cargo")
	r = withPath(r, filepath.Join(r.home, ".cargo", "bin"))
	r.Args[1] = "tool; cat " + creds

	_, out := run(t, s, r)

	assert.Contains(t, out, "tool-ran")
	assert.NotContains(t, out, "secret")
}

func TestLocalShareIsUnreadable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "")
	bin := filepath.Join(r.home, ".local", "bin")
	program(t, filepath.Join(bin, "tool"), "tool-ran")
	x := program(t, filepath.Join(r.home, ".local", "share", "pipx", "venvs", "x", "bin", "x"), "pipx-ran")
	require.NoError(t, os.Symlink(x, filepath.Join(bin, "x")))
	other := write(t, filepath.Join(r.home, ".local", "share", "other", "x"), "secret")
	state := write(t, filepath.Join(r.home, ".local", "state", "x"), "secret")
	r = withPath(r, bin)
	r.Args[1] = "tool; x; cat " + other + " " + state

	_, out := run(t, s, r)

	assert.Contains(t, out, "tool-ran")
	assert.Contains(t, out, "pipx-ran")
	assert.NotContains(t, out, "secret")
}

// linkedHome is a home reached through a link: the link, and the directory
// it names.
func linkedHome(t *testing.T) (link, home string) {
	t.Helper()
	base := realpath.Resolved(t.TempDir())
	home = mkdirs(t, base, "data/ana")[0]
	link = filepath.Join(base, "ana")
	require.NoError(t, os.Symlink(home, link))
	return link, home
}

func TestKubetailsDirectoriesAreUnreadableThroughALinkedHome(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	link, home := linkedHome(t)
	r := shRun(t, "")
	r.home = link
	program(t, filepath.Join(home, ".config", "tool", "bin", "tool"), "tool-ran")
	write(t, filepath.Join(home, ".config", "kubetail", "app.db"), "secret")
	addRoot(t, filepath.Join(link, ".config"))
	r.always.Kubetail = []string{filepath.Join(link, ".config", "kubetail")}
	r = withPath(r, filepath.Join(link, ".config", "tool", "bin"))
	r.Args[1] = "tool; cat " + filepath.Join(home, ".config", "kubetail", "app.db") + " " + filepath.Join(link, ".config", "kubetail", "app.db")

	_, out := run(t, s, r)

	assert.Contains(t, out, "tool-ran")
	assert.NotContains(t, out, "secret")
}

// A toolchain folder that is a link into a readable store is recreated as
// a link, so a PATH entry through it runs its program.
func TestAToolchainLinkIsRecreated(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	r := shRun(t, "nixtool")
	store := realpath.Resolved(t.TempDir())
	addRoot(t, store)
	program(t, filepath.Join(store, "profile", "bin", "nixtool"), "nix-ran")
	require.NoError(t, os.Symlink(filepath.Join(store, "profile"), filepath.Join(r.home, ".nix-profile")))
	r = withPath(r, filepath.Join(r.home, ".nix-profile", "bin"))

	code, out := run(t, s, r)

	assert.Equal(t, 0, code)
	assert.Equal(t, "nix-ran\n", out)
}

// A credential that is a link is unreadable where its target lies, though a
// toolchain folder takes that in.
func TestACredentialThroughALinkIsUnreadable(t *testing.T) {
	// addToolchain changes the package's lists, so this test stays serial.
	s := confining(t)
	r := shRun(t, "")
	program(t, filepath.Join(r.home, "tools", "bin", "tool"), "tool-ran")
	creds := write(t, filepath.Join(r.home, "tools", "credentials", "config"), "secret")
	require.NoError(t, os.Symlink(filepath.Dir(creds), filepath.Join(r.home, ".kube")))
	addToolchain(t, "~/tools")
	r = withPath(r, filepath.Join(r.home, "tools", "bin"))
	r.Args[1] = "tool; cat " + creds

	_, out := run(t, s, r)

	assert.Contains(t, out, "tool-ran")
	assert.NotContains(t, out, "secret")
}

// A root that is a link runs what is under its target, wherever that lies.
func TestARootThatIsALinkRuns(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	base := realpath.Resolved(t.TempDir())
	program(t, filepath.Join(base, "var", "opt", "bin", "tool"), "tool-ran")
	opt := filepath.Join(base, "opt")
	require.NoError(t, os.Symlink(filepath.Join(base, "var", "opt"), opt))
	addRoot(t, opt)

	code, out := run(t, s, withPath(shRun(t, "tool"), filepath.Join(opt, "bin")))

	assert.Equal(t, 0, code)
	assert.Equal(t, "tool-ran\n", out)
}

// Kubetail's directories inside a root are unreadable, but for the run's own
// workspace inside them.
func TestKubetailsDirectoriesInsideARootAreUnreadable(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	root := realpath.Resolved(t.TempDir())
	addRoot(t, root)
	data := filepath.Join(root, "kubetail")
	write(t, filepath.Join(data, "app.db"), "secret")
	ws := mkdirs(t, data, "chats/1/workspace")[0]
	r := shRun(t, "cat "+filepath.Join(data, "app.db")+"; touch w && echo wrote")
	r.Dir, r.files.Write = ws, nil
	r.always = AlwaysPolicy{Kubetail: []string{data}, Write: []string{ws}}

	_, out := run(t, s, r)

	assert.Equal(t, "wrote\n", out)
	assert.FileExists(t, filepath.Join(ws, "w"))
}

// kubetailRun is a run laid out as Kubetail lays one out, with its data and cache
// directories where a toolchain folder takes them in, and a sibling run
// beside it.
type kubetailRun struct {
	testRun
	data, cache, runtime, sibling  string
	snapshot, runDir, tmp, kubectl string
}

func newKubetailRun(t *testing.T) kubetailRun {
	t.Helper()
	r := shRun(t, "")
	home := r.home
	program(t, filepath.Join(home, "apps", "bin", "tool"), "tool-ran")
	program(t, filepath.Join(home, ".cache", "bin", "cached"), "cached-ran")
	data := filepath.Join(home, "apps", "kubetail")
	cache := filepath.Join(home, ".cache", "kubetail")
	runtime := realpath.Resolved(t.TempDir())
	write(t, filepath.Join(data, "app.db"), "secret")
	write(t, filepath.Join(cache, "kube", "c.db"), "secret")
	write(t, filepath.Join(runtime, "host.sock"), "secret")
	sibling := write(t, filepath.Join(runtime, "runs", "2", "kubeconfig"), "secret")
	snapshot := write(t, filepath.Join(runtime, "shell", "snapshot.sh"), "snapshot")
	runDir := filepath.Dir(write(t, filepath.Join(runtime, "runs", "1", "kubeconfig"), "own-kubeconfig"))
	ws := mkdirs(t, data, "chats/1/workspace")[0]
	tmp := mkdirs(t, cache, "tmp/1")[0]
	kubectl := mkdirs(t, cache, "kubectl/c")[0]
	r.Dir, r.ws, r.files.Write = ws, ws, nil
	r.always = AlwaysPolicy{Kubetail: []string{data, cache, runtime}, Read: []string{snapshot, runDir}, Write: []string{ws, tmp, kubectl}}
	addToolchain(t, "~/apps", "~/.cache")
	r = withPath(r, filepath.Join(home, "apps", "bin"), filepath.Join(home, ".cache", "bin"))
	return kubetailRun{
		testRun: r, data: data, cache: cache, runtime: runtime, sibling: sibling,
		snapshot: snapshot, runDir: runDir, tmp: tmp, kubectl: kubectl,
	}
}

// Kubetail's directories are unreadable, but for the run's own: its workspace,
// kubectl cache, snapshot, run directory and TMPDIR.
func TestKubetailsDirectoriesAreUnreadable(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	k := newKubetailRun(t)
	k.Args[1] = fmt.Sprintf("tool; cached; cat %s/app.db %s/kube/c.db %s/host.sock; cat %s %s/kubeconfig; touch %s/w %s/w %s/w && echo wrote",
		k.data, k.cache, k.runtime, k.snapshot, k.runDir, k.ws, k.tmp, k.kubectl)

	_, out := run(t, s, k.testRun)

	assert.Contains(t, out, "tool-ran\ncached-ran\n")
	assert.NotContains(t, out, "secret")
	assert.Contains(t, out, "snapshotown-kubeconfig")
	assert.Contains(t, out, "wrote")
}

func TestASiblingRunsKubeconfigIsUnreadable(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	k := newKubetailRun(t)
	k.Args[1] = "cat " + k.sibling + "; ls " + filepath.Dir(filepath.Dir(k.sibling))

	_, out := run(t, s, k.testRun)

	assert.NotContains(t, out, "secret")
	assert.NotContains(t, out, "2\n")
}

// A write lands in the workspace, the cache or the run's own /tmp, and
// anywhere else fails; nothing outside the sandbox changes but the first two.
// The stand-in home is under the host's /tmp, which inside is the run's own,
// so a write there lands in it: it is checked outside alone.
func TestOnlyTheWorkspaceAndTheCacheAreWritten(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	k := newKubetailRun(t)
	marker := "kubetail-write-" + strconv.Itoa(os.Getpid())
	var script strings.Builder
	for _, p := range []string{"/" + marker, "/etc/" + marker, "/usr/" + marker, k.data + "/" + marker, k.cache + "/" + marker} {
		fmt.Fprintf(&script, "touch %s 2>/dev/null && echo wrote %s; ", p, p)
	}
	fmt.Fprintf(&script, "touch %s/%s 2>/dev/null; ", k.home, marker)
	fmt.Fprintf(&script, "touch /tmp/%s && echo tmp; touch %s/%s %s/%s && echo own", marker, k.ws, marker, k.kubectl, marker)
	k.Args[1] = script.String()

	_, out := run(t, s, k.testRun)

	assert.Equal(t, "tmp\nown\n", out)
	assert.NoFileExists(t, "/tmp/"+marker)
	assert.NoFileExists(t, filepath.Join(k.home, marker))
	assert.NoFileExists(t, filepath.Join(k.data, marker))
	assert.FileExists(t, filepath.Join(k.ws, marker))
}

// A run cannot remove, rename or replace the root of a path it writes: each
// is a mount point, whose parent is the namespace's own. What lies inside
// stays its own.
func TestARunCannotReplaceItsWorkspace(t *testing.T) {
	// newKubetailRun changes the package's lists, so this test stays serial.
	s := confining(t)
	for _, script := range []string{
		`cd /; rm -rf "$W" && ln -s "$D" "$W"`,
		`cd /; rmdir "$W"`,
		`cd /; mv "$W" "$O/moved"`,
		`mkdir "$O/x" && mv -T "$O/x" "$W"`,
		`ln -s "$D" "$O/l" && mv -T "$O/l" "$W"`,
	} {
		k := newKubetailRun(t)
		for _, pair := range [][2]string{{k.ws, k.kubectl}, {k.kubectl, k.ws}} {
			w, other := pair[0], pair[1]
			r := k.testRun
			r.Args = []string{"-c", script}
			r.Env = append(slices.Clone(r.Env), "W="+w, "D="+k.data, "O="+other)
			code, out := run(t, s, r)
			assert.NotEqual(t, 0, code, "%s on %s: %s", script, w, out)
			info, err := os.Lstat(w)
			require.NoError(t, err, script)
			assert.True(t, info.IsDir(), "%s on %s left %v", script, w, info.Mode())
		}
	}

	k := newKubetailRun(t)
	for _, w := range []string{k.ws, k.kubectl} {
		r := k.testRun
		r.Args = []string{"-c", `mkdir "$W/a" && echo x > "$W/a/f" && mv "$W/a" "$W/b" && rm -rf "$W/b" && ln -s /usr "$W/l" && rm "$W/l"`}
		r.Env = append(slices.Clone(r.Env), "W="+w)
		code, out := run(t, s, r)
		assert.Equal(t, 0, code, "%s: %s", w, out)
	}
}

func init() {
	helpers["dial"] = func() int {
		c, err := net.DialTimeout("tcp", os.Getenv("KUBETAIL_SANDBOX_TEST_ADDR"), time.Second)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		_ = c.Close()
		fmt.Println("connected")
		return 0
	}
}

// self is a run of this test binary as the helper named, with env.
func self(t *testing.T, helper string, env ...string) testRun {
	t.Helper()
	r := shRun(t, "")
	r.Shell, r.Args = os.Args[0], nil
	r.Env = append(append(r.Env, "KUBETAIL_SANDBOX_TEST_HELPER="+helper), env...)
	return r
}

func TestNoListenerOutsideIsReachable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	code, out := run(t, s, self(t, "dial", "KUBETAIL_SANDBOX_TEST_ADDR="+ln.Addr().String()))

	assert.Equal(t, 1, code)
	assert.Contains(t, out, "connection refused")
}

// A lookup fails, and fails at once rather than waiting on a resolver.
func TestAPublicNameDoesNotResolve(t *testing.T) {
	t.Parallel()

	s := confining(t)
	start := time.Now()

	code, out := run(t, s, shRun(t, "getent hosts example.com"))

	assert.NotEqual(t, 0, code, out)
	assert.Less(t, time.Since(start), time.Second)
}

// The shell is refused a Unix or vsock socket while the forwarder, outside
// the filter, still relays a connection to the run's.
func TestAUnixOrVsockSocketCannotBeOpened(t *testing.T) {
	t.Parallel()

	s := confining(t)
	socket := echoSocket(t)
	r := self(t, "sockets")
	r.files.Read = []string{filepath.Dir(socket)}
	r.Policy.Network.Relays = []Relay{{Port: forwarderPort, Socket: socket}}

	_, out := run(t, s, r)
	assert.Equal(t, "unix=operation not permitted vsock=operation not permitted inet=ok dgram-pair=operation not permitted stream-pair=ok", out)

	r = self(t, "", "KUBETAIL_SANDBOX_TEST_DIAL="+strconv.Itoa(forwarderPort))
	r.files.Read = []string{filepath.Dir(socket)}
	r.Policy.Network.Relays = []Relay{{Port: forwarderPort, Socket: socket}}
	code, out := run(t, s, r)
	assert.Equal(t, 0, code)
	assert.Equal(t, "ping|eof", out)
}

// A datagram pair cannot be made, so nothing under the filter sends to a
// host's socket in a directory it can read.
func TestNoDatagramReachesAHostSocket(t *testing.T) {
	t.Parallel()

	s := confining(t)
	dir := realpath.Resolved(t.TempDir())
	path := filepath.Join(dir, "host.sock")
	ln, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	require.NoError(t, err)
	defer ln.Close()
	r := self(t, "sendto", "KUBETAIL_SANDBOX_TEST_SOCKET="+path)
	r.files.Read = []string{dir}

	code, out := run(t, s, r)

	assert.Equal(t, 1, code)
	assert.Equal(t, "operation not permitted", out)
	// The run has ended, so anything it sent is queued: a read that waits
	// for nothing finds it.
	require.NoError(t, ln.SetReadDeadline(time.Now()))
	_, _, err = ln.ReadFrom(make([]byte, 16))
	assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
}

func TestAUserNamespaceCannotBeMade(t *testing.T) {
	t.Parallel()

	s := confining(t)

	code, _ := run(t, s, shRun(t, "unshare -U true"))

	assert.NotEqual(t, 0, code)
}

// The forwarder is the namespace's first process, and nothing under the
// filter can read its environment or memory.
func TestTheFirstProcessCannotBeRead(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "tr '\\0' ' ' < /proc/1/cmdline; echo; cat /proc/1/environ >/dev/null 2>&1 || echo environ-refused; head -c1 /proc/1/mem >/dev/null 2>&1 || echo mem-refused")

	_, out := run(t, s, r)

	assert.Contains(t, out, InitCommand)
	assert.Contains(t, out, "environ-refused\nmem-refused\n")
}

// A command that leaves a process behind ends at once whether its group is
// sent SIGTERM or SIGKILL, and nothing of the run is left.
func TestNothingOutlivesTheGroupsKill(t *testing.T) {
	t.Parallel()

	s := confining(t)
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		marker := fmt.Sprintf("3001.%d%d", os.Getpid(), sig)
		cmd := command(t, s, t.Context(), shRun(t, "sleep "+marker+" & echo up; exec sleep 3002").on(s))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		out, err := cmd.StdoutPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())
		line, err := bufio.NewReader(out).ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "up\n", line)

		require.NoError(t, syscall.Kill(-cmd.Process.Pid, sig))
		_ = cmd.Wait()

		require.Eventually(t, func() bool { return !running(marker) }, 5*time.Second, 10*time.Millisecond, "%s left %s running", sig, marker)
	}
}

// running reports whether a process whose command line holds marker is alive.
func running(marker string) bool {
	procs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, p := range procs {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), marker) {
			return true
		}
	}
	return false
}

// A process orphaned under the forwarder is reaped: its /proc entry goes,
// where an unreaped one would stay a zombie and hold the loop open.
func TestAnOrphanIsReaped(t *testing.T) {
	t.Parallel()

	s := confining(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	r := shRun(t, `pid=$(sh -c 'true & echo $!'); while [ -e /proc/$pid ]; do :; done; echo reaped`)

	cmd := command(t, s, ctx, r.on(s))
	out, err := cmd.Output()

	require.NoError(t, err)
	assert.Equal(t, "reaped\n", string(out))
}

// bwrap's arguments over the fixture are the golden's, with a cluster and
// without.
func TestTheCompiledArgumentsMatchTheGolden(t *testing.T) {
	// newFixture changes the package's lists, so this test stays serial.
	f := newFixture(t)
	s := &Sandbox{self: f.self, bwrap: "/usr/bin/bwrap"}
	for name, cluster := range map[string]bool{"cluster": true, "no-cluster": false} {
		t.Run(name, func(t *testing.T) {
			cmd := command(t, s, context.Background(), f.run(s, cluster))
			f.golden(t, "args_linux_"+name+".golden", cmd.Args[1:])
		})
	}
}

// withIDs is args with the user's ids spelled $UID and $GID, which differ
// from one machine to the next.
func withIDs(args []string) []string {
	out := slices.Clone(args)
	for i := 1; i < len(out); i++ {
		switch out[i-1] {
		case "--uid":
			out[i] = "$UID"
		case "--gid":
			out[i] = "$GID"
		}
	}
	return out
}

// A run with the internet is pasta over bwrap, and its resolver is bound
// after every rule and fixed mount and before the closing remounts, where the
// host's link leads, so the link the run reads at /etc/resolv.conf reaches
// it; a host with no resolv.conf gets the bind at the path itself.
func TestTheResolverBindsWhereTheHostsLinkLeads(t *testing.T) {
	// It swaps the package's hostResolvConf, so this test stays serial.
	f := newFixture(t)
	stub := mkdirs(t, f.base, "sys/run/systemd/resolve")[0]
	require.NoError(t, os.WriteFile(filepath.Join(stub, "stub-resolv.conf"), nil, 0o600))
	link := filepath.Join(f.base, "sys", "etc", "resolv.conf")
	require.NoError(t, os.Symlink("../run/systemd/resolve/stub-resolv.conf", link))
	old := hostResolvConf
	t.Cleanup(func() { hostResolvConf = old })
	s := &Sandbox{self: f.self, bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}
	r := f.withInternet(s, f.run(s, true))

	hostResolvConf = link
	cmd := command(t, s, context.Background(), r)
	f.golden(t, "args_linux_internet.golden", withIDs(cmd.Args[1:]))

	hostResolvConf = filepath.Join(f.base, "sys", "etc", "missing.conf")
	args := command(t, s, context.Background(), r).Args
	i := slices.Index(args, hostResolvConf)
	require.Positive(t, i, "bound at the path itself")
	assert.Equal(t, []string{"--ro-bind", r.Policy.Network.Resolver}, args[i-2:i])

	for name, at := range map[string]string{
		"under /proc":      "/proc/self/resolv.conf",
		"under a denial":   filepath.Join(f.home, ".ssh", "resolv.conf"),
		"under Kubetail's": filepath.Join(f.data, "resolv.conf"),
	} {
		hostResolvConf = at
		_, err := s.Command(context.Background(), r)
		assert.ErrorContains(t, err, "cannot be bound", name)
	}
}

// A shell on /tmp or /dev, or on or under /proc, makes no rule, since a rule
// there would replace a fixed mount; one under /tmp does.
func TestSystemLeavesOutTheFixedMounts(t *testing.T) {
	t.Parallel()

	under := mkdirs(t, realpath.Resolved(t.TempDir()), "x")[0]
	s := &Sandbox{self: "/usr/bin/true"}

	for _, shell := range []string{"/tmp/sh", "/proc/1/sh", "/proc/sh", "/dev/sh"} {
		got := s.System("/nonexistent/home", shell).Files
		for _, p := range []string{"/tmp", "/dev", "/proc/1", "/proc"} {
			assert.NotContains(t, got.Read, p, shell)
		}
	}
	assert.Contains(t, s.System("/nonexistent/home", filepath.Join(under, "sh")).Files.Read, under)
}

// A policy that fails Check answers its error and no command.
func TestAPolicyThatFailsCheckStartsNothing(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}
	r := Run{Shell: "/bin/sh", Dir: "/w", Policy: Policy{Files: FilePolicy{Read: []string{"relative"}}}}

	cmd, err := s.Command(context.Background(), r)

	assert.Nil(t, cmd)
	assert.Error(t, err)
}

// A rule on /tmp or /dev, or on or under /proc, would replace a mount every
// run has, so Command refuses it, a Files rule and a run's own path alike.
func TestARuleOverAFixedMountIsRefused(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}
	for _, p := range []Policy{
		{Files: FilePolicy{Write: []string{"/tmp"}}},
		{Files: FilePolicy{Read: []string{"/dev"}}},
		{Files: FilePolicy{Read: []string{"/proc"}}},
		{Files: FilePolicy{Deny: []string{"/proc/1"}}},
		{Always: AlwaysPolicy{Kubetail: []string{"/"}, Write: []string{"/tmp"}}},
	} {
		cmd, err := s.Command(context.Background(), Run{Shell: "/bin/sh", Dir: "/w", Policy: p})

		assert.Nil(t, cmd, "%+v", p)
		assert.Error(t, err, "%+v", p)
	}
}

// A Read and a Write rule whose paths are missing are skipped: the run
// starts, and it cannot make either path.
func TestAMissingReadOrWritePathIsSkipped(t *testing.T) {
	t.Parallel()

	s := confining(t)
	base := realpath.Resolved(t.TempDir())
	read, written := filepath.Join(base, "read"), filepath.Join(base, "written")
	r := shRun(t, fmt.Sprintf("mkdir -p %[1]s %[2]s 2>/dev/null; touch %[1]s/x %[2]s/x 2>/dev/null; echo started", read, written))
	r.files.Read = []string{read}
	r.files.Write = append(r.files.Write, written)

	_, out := run(t, s, r)

	assert.Equal(t, "started\n", out)
	assert.NoDirExists(t, read)
	assert.NoDirExists(t, written)
}

// A Write rule on a folder under /tmp, and a run's own Write path under
// /dev/shm, are bound over the fixed mounts and written through them.
func TestARuleUnderTmpOrDevIsReachable(t *testing.T) {
	t.Parallel()

	s := confining(t)
	tmp, err := os.MkdirTemp("/tmp", "kubetail-rule-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	shm, err := os.MkdirTemp("/dev/shm", "kubetail-rule-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	own := mkdirs(t, shm, "own")[0]
	r := shRun(t, fmt.Sprintf("echo a > %s/a && echo b > %s/b && echo wrote", tmp, own))
	r.files.Write = append(r.files.Write, tmp)
	r.always = AlwaysPolicy{Kubetail: []string{shm}, Write: []string{own}}

	_, out := run(t, s, r)

	assert.Equal(t, "wrote\n", out)
	assert.FileExists(t, filepath.Join(tmp, "a"))
	assert.FileExists(t, filepath.Join(own, "b"))
}

// With no home, the probe still answers a sandbox: its policy denies the
// Never paths that are absolute alone.
func TestAProbeWithNoHomeFindsTheSandbox(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	confining(t)
	t.Setenv("HOME", "")

	s, v, err := Probe(t.Context())

	require.NoError(t, err)
	closing(t, s)
	require.NotNil(t, s, v.Reason)
	assert.True(t, v.Available)
}

// A folder on the sidecar's PATH opens nothing: only the lists decide what a
// run reads.
func TestAFolderOnThePathIsNotRead(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "")
	bin := filepath.Join(r.home, "notes", "bin")
	program(t, filepath.Join(bin, "tool"), "tool-ran")
	notes := write(t, filepath.Join(r.home, "notes", "todo"), "secret")
	r = withPath(r, bin)
	r.Args[1] = "tool; cat " + notes + "; ls " + filepath.Dir(notes)

	_, out := run(t, s, r)

	assert.NotContains(t, out, "tool-ran")
	assert.NotContains(t, out, "secret")
	assert.NotContains(t, out, "todo")
}

// /etc is read whole and its secret files are not, whatever their mode: a
// stand-in plays it, and the real /etc/hosts still reads.
func TestEtcSecretsStayHidden(t *testing.T) {
	// addRoot changes the package's lists, so this test stays serial.
	s := confining(t)
	etc := filepath.Join(realpath.Resolved(t.TempDir()), "etc")
	hosts := write(t, filepath.Join(etc, "hosts"), "hosts-read")
	key := write(t, filepath.Join(etc, "ssh", "ssh_host_ed25519_key"), "secret")
	shadow := write(t, filepath.Join(etc, "shadow"), "secret")
	for _, f := range []string{hosts, key, shadow} {
		require.NoError(t, os.Chmod(f, 0o644))
	}
	addRoot(t, etc)
	keepLists(t)
	platformLists.Never = append(slices.Clone(platformLists.Never), filepath.Join(etc, "ssh"), shadow)

	_, out := run(t, s, shRun(t, "cat "+hosts+" "+key+" "+shadow))

	assert.Contains(t, out, "hosts-read")
	assert.NotContains(t, out, "secret")

	code, out := run(t, s, shRun(t, "cat /etc/hosts >/dev/null && echo read"))
	assert.Equal(t, 0, code)
	assert.Equal(t, "read\n", out)
}

// A Read of the home leaves the Closed folders shut, and a Read of a folder
// inside one opens that folder.
func TestAGrantOfTheHomeLeavesClosedFoldersShut(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "")
	notes := write(t, filepath.Join(r.home, "notes"), "home-read")
	private := write(t, filepath.Join(r.home, "Documents", "private"), "secret")
	project := write(t, filepath.Join(r.home, "Documents", "project", "main.go"), "project-read")
	r.Args[1] = "cat " + notes + " " + private + " " + project
	r.files.Read = []string{r.home}

	_, out := run(t, s, r)

	assert.Contains(t, out, "home-read")
	assert.NotContains(t, out, "secret")
	assert.NotContains(t, out, "project-read")

	r.files.Read = append(r.files.Read, filepath.Dir(project))
	_, out = run(t, s, r)
	assert.Contains(t, out, "project-read")
	assert.NotContains(t, out, "secret")
}

// Each Toolchain location runs a program from its first folder, with its
// variables set and pointing into the home.
func TestEachToolchainLocationRunsAProgram(t *testing.T) {
	t.Parallel()

	s := confining(t)
	for _, l := range sharedLists.Toolchain {
		t.Run(l.Name, func(t *testing.T) {
			r := shRun(t, "")
			dir := inHome(r.home, l.Read[:1])[0]
			if filepath.Base(dir) != "bin" {
				dir = filepath.Join(dir, "bin")
			}
			prog := program(t, filepath.Join(dir, "kubetail-tool"), "tool-ran")
			sys := s.System(r.home, r.Shell)
			r.Env = append(r.Env, sys.Env...)
			script := prog
			for name := range l.Env {
				script += `; printf '%s\n' "$` + name + `"`
			}
			r.Args[1] = script

			code, out := run(t, s, r)

			require.Equal(t, 0, code, out)
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			assert.Equal(t, "tool-ran", lines[0])
			require.Len(t, lines, 1+len(l.Env))
			for _, v := range lines[1:] {
				assert.True(t, realpath.Within(v, r.home), "%s is not under the home", v)
			}
		})
	}
}

// A probe whose policy never answers fails at its bound: the policy lists
// folders, which can hang on a network mount.
func TestTheProbePolicyIsBounded(t *testing.T) {
	// It waits on the package's boundedCalls, which every probe adds to, so this test stays serial.
	release := make(chan struct{})
	// Cleanups run last first, so the build is released before it is waited on.
	t.Cleanup(boundedCalls.Wait)
	t.Cleanup(func() { close(release) })
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
	s.buildProbePolicy = func(string, string, string) Policy {
		<-release
		return Policy{}
	}

	_, err := s.try(t.Context(), 10*time.Millisecond)

	assert.EqualError(t, err, "no answer in 10ms")
}

// hang is a stand-in's body that blocks until release, and a channel closed
// when it starts. Inside a synctest bubble, time passes only once it blocks,
// so a deadline ends the call holding it, and code waiting on it is reported
// as a deadlock.
func hang() (wait func(), started <-chan struct{}, release func()) {
	start, stop := make(chan struct{}), make(chan struct{})
	wait = func() {
		close(start)
		<-stop
	}
	return wait, start, func() { close(stop) }
}

// hangingCheck is a sandbox whose check hangs. Its probe's policy builds at
// once.
func hangingCheck() (s *Sandbox, started <-chan struct{}, release func()) {
	wait, started, release := hang()
	s = &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
	s.buildProbePolicy = func(string, string, string) Policy { return Policy{} }
	s.check = func(Run) ([]rule, error) {
		wait()
		return nil, nil
	}
	return s, started, release
}

// hangingDenial is a sandbox whose check passes at once with one denial, and
// whose lookup of that denial then hangs, as on a mount that stopped
// answering since.
func hangingDenial() (s *Sandbox, started <-chan struct{}, release func()) {
	wait, started, release := hang()
	s = &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
	s.buildProbePolicy = func(string, string, string) Policy { return Policy{} }
	s.check = func(Run) ([]rule, error) {
		return []rule{{path: "/mnt/nfs", at: "/mnt/nfs", kind: ruleDeny}}, nil
	}
	s.stat = func(string) (os.FileInfo, error) {
		wait()
		return nil, os.ErrNotExist
	}
	return s, started, release
}

// requireStarted fails the test unless the check began.
func requireStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	default:
		require.FailNow(t, "the check never started")
	}
}

// A probe whose check never answers fails at its own bound, though its caller
// waits on: the check looks up each rule path and its ancestors, which can
// hang on a network mount.
func TestTheProbesCheckIsBounded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingCheck()
		defer release()

		_, err := s.try(t.Context(), 10*time.Millisecond)

		requireStarted(t, started)
		assert.EqualError(t, err, "no answer in 10ms")
	})
}

// A check that hangs holds the command only until its context ends.
func TestACommandWhoseCheckHangsIsRefusedWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingCheck()
		defer release()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		cmd, err := s.Command(ctx, Run{Shell: "/bin/sh", Dir: "/"})

		requireStarted(t, started)
		assert.Nil(t, cmd)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// A denial's lookup after the check, which can hang as the check can, holds
// the command and the probe only until their context ends.
func TestADenialLookupThatHangsIsBounded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingDenial()
		defer release()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		cmd, err := s.Command(ctx, Run{Shell: "/bin/sh", Dir: "/"})

		requireStarted(t, started)
		assert.Nil(t, cmd)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingDenial()
		defer release()

		_, err := s.try(t.Context(), 10*time.Millisecond)

		requireStarted(t, started)
		assert.EqualError(t, err, "no answer in 10ms")
	})
}

// A probe's build left behind reads the bwrap it was started with, while the
// probe moves on to its next one. The race detector checks it: the write
// below is ordered after nothing the build does.
func TestAProbesBuildLeftBehindReadsItsOwnBwrap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _, release := hangingCheck()

		_, err := s.try(t.Context(), 10*time.Millisecond)
		require.EqualError(t, err, "no answer in 10ms")
		release()
		s.bwrap = "/usr/lib/kubetail-desktop/bwrap"
	})
}

// A check Command leaves behind reads a copy of the run, so the caller may
// change its own once Command returns.
func TestACheckLeftBehindReadsACopyOfTheRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		wait, _, release := hang()
		seen := make(chan string, 1)
		s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
		s.check = func(r Run) ([]rule, error) {
			wait()
			seen <- r.Env[0]
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		env := []string{"A=1"}

		_, err := s.Command(ctx, Run{Shell: "/bin/sh", Dir: "/", Env: env})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		env[0] = "A=2"
		release()

		assert.Equal(t, "A=1", <-seen)
	})
}

// Command refuses a run holding a variable no run may hold.
func TestARunWithAnUnpassableVariableIsRefused(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: "/k", bwrap: "/usr/bin/bwrap"}
	for _, kv := range []string{"LD_PRELOAD=/x.so", "AWS_SESSION_TOKEN=t"} {
		cmd, err := s.Command(context.Background(), Run{Shell: "/bin/sh", Dir: "/", Env: []string{kv}})

		assert.Nil(t, cmd, kv)
		assert.ErrorContains(t, err, "may not pass", kv)
	}
}

// A shell installed under the home and reached through a link runs: the
// policy reads where the link leads.
func TestALinkedShellRuns(t *testing.T) {
	t.Parallel()

	s := confining(t)
	r := shRun(t, "echo ran")
	sh, err := os.ReadFile("/bin/sh")
	require.NoError(t, err)
	install := write(t, filepath.Join(r.home, "shell-install", "bin", "sh"), string(sh))
	link := filepath.Join(r.home, ".local", "bin", "sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(install, link))
	r.Shell = link

	code, out := run(t, s, r)

	assert.Equal(t, 0, code)
	assert.Equal(t, "ran\n", out)
}

// From 5.14 the kernel counts a process limit per user namespace.
func TestTheKernelReleaseIsRead(t *testing.T) {
	t.Parallel()

	for _, release := range []string{"5.14.0-284.el9", "6.1.0", "7.0.14"} {
		assert.True(t, countsPerNamespace(release), release)
	}
	for _, release := range []string{"4.18.0-553.el8", "5.13.19", "", "linux", "5"} {
		assert.False(t, countsPerNamespace(release), release)
	}
}

// Two namespaces with different maps are different namespaces; a line that
// matches the sidecar's, or none, is not the run's own.
func TestTheUidMapDecidesTheNamespace(t *testing.T) {
	t.Parallel()

	sidecar := "         0          0 4294967295"

	assert.True(t, ownNamespace("1000 1000 1", sidecar))
	assert.False(t, ownNamespace("0 0 4294967295", sidecar))
	assert.False(t, ownNamespace("", sidecar))
}

// Where the machine makes a user namespace, the probed sandbox's run has one
// of its own, and every run asks bwrap for it.
func TestTheProbeFindsItsOwnUserNamespace(t *testing.T) {
	t.Parallel()

	if err := exec.Command("unshare", "-U", "true").Run(); err != nil {
		t.Skip("no user namespace outside the sandbox: ", err)
	}
	s := confining(t)

	assert.True(t, s.ownUserNS)
	args := command(t, s, t.Context(), Run{Shell: "/bin/sh", Dir: "/"}).Args
	assert.Contains(t, args, "--unshare-user-try")
}

// Through the real sandbox a command cannot trace even its own child, which
// the kernel's own checks would allow: the filter is what refuses it.
func TestTheShellCannotTraceItsOwnChild(t *testing.T) {
	t.Parallel()

	outside(t, "trace")
	s := confining(t)

	code, out := run(t, s, self(t, "trace"))

	assert.Equal(t, 1, code)
	assert.Equal(t, "operation not permitted", out)
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

func TestFixedMountIsEveryRunsOwnMount(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"/proc", "/proc/1/root", "/dev", "/dev/pts", "/tmp", "/tmp/x"} {
		assert.True(t, FixedMount(p), p)
	}
	for _, p := range []string{"/", "/home/me", "/procs", "/var/tmp", "/devices"} {
		assert.False(t, FixedMount(p), p)
	}
	assert.False(t, overFixedMount("/tmp/kubetail-1000"), "a run's own runtime directory under /tmp is no rule over a mount")
}

// A run ends when the sidecar's lifeline closes, while bwrap's parent, which
// --die-with-parent watches, lives on.
func TestARunEndsWithTheLifeline(t *testing.T) {
	t.Parallel()

	s := confining(t)
	cmd := command(t, s, t.Context(), shRun(t, "sleep 1000 & echo up; wait").on(s))

	requireEndsWithTheLifeline(t, cmd, s)
}

// Every run leads a session of its own, with the internet or without, so a
// kill to its group reaches nothing of the sidecar's.
func TestARunLeadsASessionOfItsOwn(t *testing.T) {
	t.Parallel()

	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}
	r := internetRun(t, s, "-c", "true")

	assert.True(t, command(t, s, t.Context(), r).SysProcAttr.Setsid)
	r.Policy.Network = NetworkPolicy{}
	assert.True(t, command(t, s, t.Context(), r).SysProcAttr.Setsid)
}

// A closed Sandbox makes no command, so no run starts without the lifeline.
func TestAClosedSandboxMakesNoCommand(t *testing.T) {
	// profileRun changes the package's lists, so this test stays serial.
	s := lifeline(t)
	s.self, s.bwrap, s.pasta = os.Args[0], "/usr/bin/bwrap", "/usr/bin/pasta"
	r := internetRun(t, s, "-c", "true")
	require.NoError(t, s.Close())

	_, err := s.Command(t.Context(), r)
	assert.ErrorIs(t, err, errClosed)
	r.Policy.Network = NetworkPolicy{}
	_, err = s.Command(t.Context(), r)
	assert.ErrorIs(t, err, errClosed)
}

// A closed Sandbox's probe starts no run either: the probe fails on it.
func TestAClosedSandboxTriesNoRun(t *testing.T) {
	t.Parallel()

	s := lifeline(t)
	s.self, s.bwrap = os.Args[0], "/usr/bin/bwrap"
	require.NoError(t, s.Close())

	_, err := s.try(t.Context(), time.Minute)
	assert.ErrorIs(t, err, errClosed)
}

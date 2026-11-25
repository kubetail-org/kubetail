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

package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// access is what a run could do with a file: read its contents, write it.
type access struct{ read, write bool }

var (
	none      = access{}
	readOnly  = access{read: true}
	readWrite = access{read: true, write: true}
)

// accessOf runs files through s under a policy of System plus files and
// always, over base, and answers what the run could do with each file, named
// by its path under base. A read counts only when it returns the file's
// contents: on Linux a denied file reads as /dev/null and a denied folder
// lists as empty, where macOS answers an error.
func accessOf(t *testing.T, s *Sandbox, base string, files FilePolicy, always AlwaysPolicy, names ...string) map[string]access {
	t.Helper()
	home := mkdirs(t, base, "home")[0]
	var script strings.Builder
	for _, name := range names {
		path := filepath.Join(base, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("contents of "+name), 0o600))
		fmt.Fprintf(&script, "printf '%%s\\n' \"$(cat '%[1]s' 2>/dev/null)\"; echo x >> '%[1]s' 2>/dev/null; ", path)
	}
	script.WriteString("true")
	env := []string{"PATH=/usr/bin:/bin"}
	system := s.System(home, "/bin/sh").Files
	files.Read = append(system.Read, files.Read...)
	files.Deny = append(system.Deny, files.Deny...)
	always.Deny = append(s.Never(home), always.Deny...)
	r := Run{Shell: "/bin/sh", Args: []string{"-c", script.String()}, Dir: "/", Env: env, Policy: Policy{Files: files, Always: always}}

	out, err := command(t, s, t.Context(), r).Output()
	require.NoError(t, err, string(out))

	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	require.Len(t, lines, len(names), string(out))
	got := map[string]access{}
	for i, name := range names {
		written, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(name)))
		require.NoError(t, err)
		got[name] = access{read: lines[i] == "contents of "+name, write: strings.HasSuffix(string(written), "x\n")}
	}
	return got
}

// Every pair of rules answers as the rules say, on Linux and on macOS alike:
// the deepest rule wins, the narrower wins a tie, and inside a Kubetail path
// only the run's own paths open anything.
func TestEveryPairOfRulesAnswersAlike(t *testing.T) {
	s := confining(t)
	for name, c := range map[string]struct {
		files  func(outer, inner string) FilePolicy
		inner  access
		beside access
	}{
		"Read over Read":  {func(o, i string) FilePolicy { return FilePolicy{Read: []string{o, i}} }, readOnly, readOnly},
		"Read over Write": {func(o, i string) FilePolicy { return FilePolicy{Read: []string{o}, Write: []string{i}} }, readWrite, readOnly},
		"Read over Deny":  {func(o, i string) FilePolicy { return FilePolicy{Read: []string{o}, Deny: []string{i}} }, none, readOnly},
		"Deny over Read":  {func(o, i string) FilePolicy { return FilePolicy{Deny: []string{o}, Read: []string{i}} }, readOnly, none},
		"Deny over Write": {func(o, i string) FilePolicy { return FilePolicy{Deny: []string{o}, Write: []string{i}} }, readWrite, none},
		"Deny over Deny":  {func(o, i string) FilePolicy { return FilePolicy{Deny: []string{o, i}} }, none, none},
	} {
		t.Run(name, func(t *testing.T) {
			base := realpath.Resolved(t.TempDir())
			outer, inner := filepath.Join(base, "outer"), filepath.Join(base, "outer", "inner")

			got := accessOf(t, s, base, c.files(outer, inner), AlwaysPolicy{}, "outer/inner/f", "outer/beside/f")

			assert.Equal(t, map[string]access{"outer/inner/f": c.inner, "outer/beside/f": c.beside}, got)
		})
	}
	for name, c := range map[string]struct {
		files FilePolicy
		want  access
	}{
		"Write and Read": {FilePolicy{Write: []string{"p"}, Read: []string{"p"}}, readOnly},
		"Write and Deny": {FilePolicy{Write: []string{"p"}, Deny: []string{"p"}}, none},
		"Read and Deny":  {FilePolicy{Read: []string{"p"}, Deny: []string{"p"}}, none},
	} {
		t.Run("a tie of "+name, func(t *testing.T) {
			base := realpath.Resolved(t.TempDir())
			on := func(rules []string) []string {
				if len(rules) == 0 {
					return nil
				}
				return []string{filepath.Join(base, "p")}
			}
			files := FilePolicy{Read: on(c.files.Read), Write: on(c.files.Write), Deny: on(c.files.Deny)}

			got := accessOf(t, s, base, files, AlwaysPolicy{}, "p/f")

			assert.Equal(t, map[string]access{"p/f": c.want}, got)
		})
	}
	t.Run("a Kubetail path with the run's own inside", func(t *testing.T) {
		base := realpath.Resolved(t.TempDir())
		k := filepath.Join(base, "k")
		always := AlwaysPolicy{Kubetail: []string{k}, Read: []string{filepath.Join(k, "r")}, Write: []string{filepath.Join(k, "w")}}

		got := accessOf(t, s, base, FilePolicy{Read: []string{base}}, always, "beside/f", "k/other/f", "k/r/f", "k/w/f")

		assert.Equal(t, map[string]access{"beside/f": readOnly, "k/other/f": none, "k/r/f": readOnly, "k/w/f": readWrite}, got)
	})
}

// The login shell's shape answers alike: a Read on / reads the home and
// nothing in Kubetail's directories, and the run's own write inside one of them
// is the one place it writes. The base is outside /tmp, which a Linux run
// mounts privately.
func TestTheLoginShellsPolicyAnswersAlike(t *testing.T) {
	s := confining(t)
	base := testutil.GrantableDir(t)
	kubetail := mkdirs(t, base, "data", "cache", "runtime")
	scratch := mkdirs(t, base, "cache/tmp/1-1")[0]
	always := AlwaysPolicy{Kubetail: kubetail, Write: []string{scratch}}

	got := accessOf(t, s, base, FilePolicy{Read: []string{"/"}}, always,
		"data/f", "cache/f", "runtime/f", "cache/tmp/1-1/f", "home/f")

	assert.Equal(t, map[string]access{
		"data/f": none, "cache/f": none, "runtime/f": none,
		"cache/tmp/1-1/f": readWrite, "home/f": readOnly,
	}, got)
}

// A Kubetail directory nested in another, as the logs sit in the state on Linux,
// is accepted and denied with the one around it.
func TestANestedKubetailPathIsAcceptedAndDenied(t *testing.T) {
	s := confining(t)
	base := testutil.GrantableDir(t)
	kubetail := mkdirs(t, base, "data", "cache", "runtime", "data/logs")
	scratch := mkdirs(t, base, "cache/tmp/1-1")[0]
	always := AlwaysPolicy{Kubetail: kubetail, Write: []string{scratch}}
	files := FilePolicy{Read: []string{"/"}}
	require.NoError(t, Policy{Files: files, Always: always}.Check())

	got := accessOf(t, s, base, files, always, "data/f", "data/logs/f", "cache/tmp/1-1/f")

	assert.Equal(t, map[string]access{"data/f": none, "data/logs/f": none, "cache/tmp/1-1/f": readWrite}, got)
}

// A Read on the home reads ~/.zshrc and not ~/.ssh, a denied-always path;
// and a Read inside ~/.ssh is refused by Check. Step 4D's grants rest on this.
func TestTheDeniedAlwaysListWinsOverARead(t *testing.T) {
	s := confining(t)
	base := realpath.Resolved(t.TempDir())
	home := filepath.Join(base, "home")

	got := accessOf(t, s, base, FilePolicy{Read: []string{home}}, AlwaysPolicy{}, "home/.zshrc", "home/.ssh/id_ed25519")

	assert.Equal(t, map[string]access{"home/.zshrc": readOnly, "home/.ssh/id_ed25519": none}, got)
	p := Policy{Files: FilePolicy{Read: []string{filepath.Join(home, ".ssh", "keys")}}, Always: AlwaysPolicy{Deny: s.Never(home)}}
	assert.Error(t, p.Check())
}

// Every rule is compiled at its target: a Kubetail directory given through a
// linked home, and a credential path that is itself a link, each lie where
// they resolve, inside the Read that holds them.
func TestARuleIsCompiledWhereItsLinkLeads(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	home := mkdirs(t, base, "home")[0]
	linked := filepath.Join(base, "home-link")
	require.NoError(t, os.Symlink(home, linked))
	d := mkdirs(t, home, "tools/kubetail", "tools/credentials")
	require.NoError(t, os.Symlink(d[1], filepath.Join(home, ".kube")))
	p := Policy{
		Files:  FilePolicy{Read: []string{filepath.Join(linked, "tools")}},
		Always: AlwaysPolicy{Deny: []string{filepath.Join(linked, ".kube")}, Kubetail: []string{filepath.Join(linked, "tools", "kubetail")}},
	}

	var at []string
	for _, r := range compiledRules(t, p) {
		at = append(at, r.at)
	}

	assert.Equal(t, []string{filepath.Join(home, "tools"), d[1], d[0]}, at)
}

// A run's own path that is a link is refused, Read or Write, since the profile
// would open where the link leads. A link above the last component is not.
// It cannot be compared, so a Deny that overlaps it under another name could
// be dropped as out of reach.
func TestARuleTheKernelCannotNameIsRefused(t *testing.T) {
	svc := unnameable(t)

	require.ErrorContains(t, Policy{Files: FilePolicy{Read: []string{svc}}}.Check(), svc)
	require.ErrorContains(t, Policy{Files: FilePolicy{Deny: []string{svc}}}.Check(), svc)
}

func TestARunsOwnPathThatIsALinkIsRefused(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "data/chats/c/workspace", "cache/tmp/1-a", "runtime/runs/1-a")
	link := filepath.Join(base, "data/chats/c/linked")
	require.NoError(t, os.Symlink(filepath.Join(base, "data"), link))
	above := filepath.Join(base, "cache/above")
	require.NoError(t, os.Symlink(filepath.Join(base, "data"), above))
	kubetail := []string{filepath.Join(base, "data"), filepath.Join(base, "cache"), filepath.Join(base, "runtime")}
	policy := func(read, write []string) Policy {
		return Policy{Always: AlwaysPolicy{Kubetail: kubetail, Read: read, Write: write}}
	}
	require.NoError(t, policy([]string{d[2]}, []string{d[0], d[1]}).Check())

	assert.ErrorContains(t, policy([]string{d[2]}, []string{link, d[1]}).Check(), "is a link", "a Write path")
	assert.ErrorContains(t, policy([]string{link}, []string{d[0]}).Check(), "is a link", "a Read path")
	assert.NoError(t, policy(nil, []string{filepath.Join(above, "chats/c/workspace")}).Check(), "a link above the last component")
}

// A run's own path that cannot be looked at is refused, since it could be a
// link.
func TestARunsOwnPathThatCannotBeLookedAtIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root looks through any folder's mode")
	}
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "data/chats/c/workspace")
	chat := filepath.Dir(d[0])
	require.NoError(t, os.Chmod(chat, 0))
	t.Cleanup(func() { _ = os.Chmod(chat, 0o700) })
	p := Policy{Always: AlwaysPolicy{Kubetail: []string{filepath.Join(base, "data")}, Write: d}}

	assert.ErrorIs(t, p.Check(), os.ErrPermission)
}

func init() {
	// files opens /dev/null until it cannot, and prints how many it opened
	// and why it stopped.
	helpers["files"] = func() int {
		for n := 0; ; n++ {
			if _, err := os.Open("/dev/null"); err != nil {
				fmt.Println(n, errors.Is(err, syscall.EMFILE))
				return 0
			}
		}
	}
}

// limitedRunOf is a run of argv on s under the System policy over a fresh
// home, with a workspace it writes, the system's PATH and env, and limits l.
func limitedRunOf(t *testing.T, s *Sandbox, l Limits, env []string, argv ...string) Run {
	t.Helper()
	d := mkdirs(t, realpath.Resolved(t.TempDir()), "home", "ws")
	env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	files := s.System(d[0], "/bin/sh").Files
	files.Write = append(files.Write, d[1])
	return Run{
		Shell: argv[0], Args: argv[1:], Dir: d[1], Env: env,
		Policy: Policy{Files: files, Always: AlwaysPolicy{Deny: s.Never(d[0])}, Limits: l},
	}
}

// limitedRun runs limitedRunOf's run to its end and answers its exit code and
// stdout. A failsafe bounds it, since a limit that fails to hold leaves a
// spin running; a run that reaches it fails the test, so the kill is never
// read as the limit's.
func limitedRun(t *testing.T, s *Sandbox, l Limits, env []string, argv ...string) (int, string) {
	t.Helper()
	r := limitedRunOf(t, s, l, env, argv...)
	ctx, cancel := context.WithTimeout(t.Context(), 3*testutil.Timeout)
	defer cancel()
	cmd := command(t, s, ctx, r)
	out, err := cmd.Output()
	if cmd.ProcessState == nil {
		require.NoError(t, err)
	}
	require.NoError(t, ctx.Err(), "the run outlived its bound")
	return ExitCode(cmd.ProcessState), string(out)
}

// A process past its CPU time dies of SIGXCPU, which a shell reports as 152.
func TestACPUSpinEndsWithSIGXCPU(t *testing.T) {
	s := confining(t)

	code, _ := limitedRun(t, s, Limits{CPUSeconds: 1}, nil, "/bin/sh", "-c", "while :; do :; done")

	assert.Equal(t, 152, code)
}

func TestOpeningPastTheFileLimitFails(t *testing.T) {
	s := confining(t)

	code, out := limitedRun(t, s, Limits{OpenFiles: 64}, []string{"KUBETAIL_SANDBOX_TEST_HELPER=files"}, os.Args[0])

	require.Equal(t, 0, code, out)
	var n int
	var emfile bool
	_, err := fmt.Sscan(out, &n, &emfile)
	require.NoError(t, err, out)
	assert.Less(t, n, 64)
	assert.True(t, emfile, out)
}

func init() {
	// forks starts sleep children until it cannot, or until it has started
	// KUBETAIL_SANDBOX_TEST_FORKS, so a limit that fails to hold never takes the
	// machine. It prints how many it started and whether the kernel said
	// EAGAIN, then waits for its input to end.
	helpers["forks"] = func() int {
		// Linux counts threads against the limit too, and a garbage collection
		// wakes idle Ps, so the runtime may start a thread once the loop has
		// filled the run, and dies when it cannot. With the collector off,
		// nothing in this process needs a thread it does not already have.
		debug.SetGCPercent(-1)
		most, _ := strconv.Atoi(os.Getenv("KUBETAIL_SANDBOX_TEST_FORKS"))
		n := 0
		var err error
		for ; n < most; n++ {
			if err = exec.Command("sleep", "1000").Start(); err != nil {
				break
			}
		}
		fmt.Println(n, errors.Is(err, syscall.EAGAIN))
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
}

// forkLoop runs the forks helper through s under l, at most most children,
// calls whileFull once it stops, and answers how many children it started,
// whether it stopped on EAGAIN, and whileFull's error. Each of with changes
// the run first.
func forkLoop(t *testing.T, s *Sandbox, l Limits, most int, whileFull func() error, with ...func(Run) Run) (n int, eagain bool, err error) {
	t.Helper()
	env := []string{"KUBETAIL_SANDBOX_TEST_HELPER=forks", "KUBETAIL_SANDBOX_TEST_FORKS=" + strconv.Itoa(most)}
	r := limitedRunOf(t, s, l, env, os.Args[0])
	for _, w := range with {
		r = w(r)
	}
	cmd := command(t, s, t.Context(), r)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })

	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	_, err = fmt.Sscan(line, &n, &eagain)
	require.NoError(t, err, line)
	err = whileFull()
	_ = in.Close()
	return n, eagain, err
}

// Where the kernel counts the user's whole machine, a margin over the count
// still stops a fork loop. Other tests move the count, so the bound is loose:
// the test proves a limit holds, not its value.
func TestAForkLoopStopsUnderAMachineWideCount(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the kernel holds root to no process limit")
	}
	s := confining(t)
	base, err := s.CountedProcesses(false)
	require.NoError(t, err)
	if base == 0 {
		t.Skip("the run counts its own namespace alone")
	}

	n, eagain, _ := forkLoop(t, s, Limits{Processes: base + 512, OpenFiles: 4096}, 4096, func() error { return nil })

	assert.Less(t, n, 4096)
	assert.True(t, eagain)
}

// sudo cannot gain root: Linux sets no_new_privs, and macOS refuses to start
// it.
func TestSudoCannotGainRoot(t *testing.T) {
	if out, err := exec.Command("sudo", "-n", "id", "-u").Output(); err != nil || string(out) != "0\n" {
		t.Skip("sudo does not gain root outside the sandbox without a password")
	}
	s := confining(t)

	code, out := limitedRun(t, s, Limits{}, nil, "/bin/sh", "-c", "sudo -n id -u")

	assert.NotEqual(t, 0, code)
	assert.NotContains(t, out, "0")
}

// The internet and the relay go together, and a Resolver is checked as a run's
// own path is: absolute, not a link, inside Kubetail's directories, and only with
// the internet.
func TestTheResolverIsChecked(t *testing.T) {
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "runtime/runs/1-a", "home")
	resolver := filepath.Join(d[0], "resolv.conf")
	require.NoError(t, os.WriteFile(resolver, []byte("nameserver 169.254.1.53\n"), 0o600))
	link := filepath.Join(d[0], "linked.conf")
	require.NoError(t, os.Symlink(resolver, link))
	kubetail := []string{filepath.Join(base, "data"), filepath.Join(base, "cache"), filepath.Join(base, "runtime")}
	policy := func(n NetworkPolicy) Policy {
		return Policy{Always: AlwaysPolicy{Kubetail: kubetail, Read: []string{d[0]}}, Network: n}
	}
	relay := []Relay{{Port: 6443, Socket: filepath.Join(d[0], "proxy.sock")}}

	assert.NoError(t, policy(NetworkPolicy{Internet: true}).Check())
	assert.NoError(t, policy(NetworkPolicy{Relays: relay, Internet: true, Resolver: resolver}).Check(), "the relay and the internet")

	for name, n := range map[string]NetworkPolicy{
		"a resolver without the internet": {Resolver: resolver},
		"a relative resolver":             {Internet: true, Resolver: "resolv.conf"},
		"a resolver that is a link":       {Internet: true, Resolver: link},
		"a resolver outside Kubetail's":   {Internet: true, Resolver: filepath.Join(d[1], "resolv.conf")},
	} {
		assert.Error(t, policy(n).Check(), name)
	}
}

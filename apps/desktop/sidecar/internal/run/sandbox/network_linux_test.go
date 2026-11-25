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
	"encoding/binary"
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
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// internetRun is a run on s with the internet and its resolver, under a
// policy whose Kubetail directory is base: the probe's policy, with the run's
// directory read and the resolver in it.
func internetRun(t *testing.T, s *Sandbox, args ...string) Run {
	t.Helper()
	base := realpath.Resolved(t.TempDir())
	d := mkdirs(t, base, "kubetail/runs/1-a", "work")
	resolver := filepath.Join(d[0], "resolv.conf")
	require.NoError(t, os.WriteFile(resolver, []byte("nameserver "+ResolverAddress+"\n"), 0o600))
	home, _ := os.UserHomeDir()
	p := s.probePolicy("/bin/sh", d[1], home)
	p.Always.Kubetail = []string{filepath.Join(base, "kubetail")}
	p.Always.Read = []string{d[0]}
	p.Network = NetworkPolicy{Internet: true, Resolver: resolver}
	return Run{Shell: "/bin/sh", Args: args, Dir: d[1], Env: []string{"PATH=/usr/bin:/bin", "HOME=" + d[1]}, Policy: p}
}

// A machine with no pasta offers no network, says why, and refuses a run
// with the internet; a run without it still starts.
func TestNoPastaNoNetwork(t *testing.T) {
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}

	s.probePasta(t.Context(), nil, time.Minute)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Equal(t, "pasta not found", reason)
	_, err := s.Command(t.Context(), internetRun(t, s, "-c", "true"))
	assert.ErrorContains(t, err, "pasta not found")
	_, err = s.Command(t.Context(), Run{Shell: "/bin/sh", Dir: "/"})
	assert.NoError(t, err)
}

// A pasta that never answers offers no network once the bound is out.
func TestTheProbeIsBounded(t *testing.T) {
	// Latency injected into the code under test: the fake outlasts the bound.
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), "exec sleep 60")
	// The bound can cut off the policy build too.
	t.Cleanup(boundedCalls.Wait)
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}

	s.probePasta(t.Context(), []string{pasta}, 10*time.Millisecond)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Equal(t, pasta+": no answer in 10ms", reason)
}

// A pasta that fails the probe says why in its first line.
func TestAPastaThatFailsTheProbeSaysWhy(t *testing.T) {
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), `echo "Failed to open() /dev/net/tun" >&2; exit 1`)
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}

	s.probePasta(t.Context(), []string{pasta}, time.Minute)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Equal(t, pasta+": Failed to open() /dev/net/tun", reason)
}

// A pasta probe whose policy never answers fails at its bound, as the bwrap
// probe's does.
func TestThePastaProbePolicyIsBounded(t *testing.T) {
	release := make(chan struct{})
	// Cleanups run last first, so the build is released before it is waited on.
	t.Cleanup(boundedCalls.Wait)
	t.Cleanup(func() { close(release) })
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
	s.buildProbePolicy = func(string, string, string) Policy {
		<-release
		return Policy{}
	}

	_, err := s.tryPasta(t.Context(), "/bin/true", 10*time.Millisecond)

	assert.EqualError(t, err, "no answer in 10ms")
}

// A pasta probe whose check never answers fails at its bound too.
func TestThePastaProbesCheckIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingCheck()
		defer release()

		_, err := s.tryPasta(t.Context(), "/bin/true", 10*time.Millisecond)

		requireStarted(t, started)
		assert.EqualError(t, err, "no answer in 10ms")
	})
}

// A pasta probe whose denial lookup hangs after its check fails at its bound
// too.
func TestThePastaProbesDenialLookupIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, started, release := hangingDenial()
		defer release()

		_, err := s.tryPasta(t.Context(), "/bin/true", 10*time.Millisecond)

		requireStarted(t, started)
		assert.EqualError(t, err, "no answer in 10ms")
	})
}

// A probe past its bound ends pasta's child too. Killed during setup, pasta
// leaves the child it made for the run's namespaces holding the probe's pipes,
// so the probe would wait on them forever.
func TestAProbePastItsBoundEndsPastasChild(t *testing.T) {
	token := runToken(t)
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), token+` sleep 600 & exec sleep 600`)
	t.Cleanup(func() {
		for pid := range processesOf(token) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	t.Cleanup(boundedCalls.Wait)
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}
	done := make(chan struct{})

	go func() {
		defer close(done)
		s.probePasta(t.Context(), []string{pasta}, 500*time.Millisecond)
	}()

	testutil.Wait(t, done, "the probe")
	_, reason := s.NetworkStatus()
	assert.Equal(t, pasta+": no answer in 500ms", reason)
	assert.Eventually(t, func() bool { return len(processesOf(token)) == 0 }, testutil.Timeout, 10*time.Millisecond,
		"processes left: %v", processesOf(token))
}

// A probe whose policy cannot be enforced fails, saying why: here, a home
// the kernel cannot name.
func TestAPastaProbeWithARefusedPolicyFails(t *testing.T) {
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), "exit 0")
	t.Setenv("HOME", unnameable(t))
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}

	s.probePasta(t.Context(), []string{pasta}, time.Minute)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Contains(t, reason, "the path of")
}

// With no temporary directory to run in, the pasta probe fails, saying why.
func TestAPastaProbeWithNoTempDirFails(t *testing.T) {
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), "exit 0")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap"}

	s.probePasta(t.Context(), []string{pasta}, time.Minute)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Contains(t, reason, "no such file or directory")
}

// A closed Sandbox probes no pasta: a run with the internet is handed the
// lifeline as any run is.
func TestAClosedSandboxProbesNoPasta(t *testing.T) {
	s := lifeline(t)
	s.self, s.bwrap = os.Args[0], "/usr/bin/bwrap"
	require.NoError(t, s.Close())

	s.probePasta(t.Context(), []string{"/bin/true"}, time.Minute)

	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Contains(t, reason, errClosed.Error())
}

// When the system's pasta fails, Kubetail's own is probed in its place, and both
// reasons are named when both fail; one that passes is never replaced. The
// passing pastas run bwrap in the sidecar's own network.
func TestKubetailsOwnPastaStandsInForTheSystems(t *testing.T) {
	s := confining(t)
	// Each case probes from scratch, as Probe does, whatever pasta this machine has.
	fresh := func() { s.pasta, s.pastaOwnUserNS, s.networkReason = "", false, "" }
	dir := t.TempDir()
	marker := filepath.Join(dir, "own-ran")
	failing := fakeBwrap(t, filepath.Join(dir, "system-failing"), `echo "system failed" >&2; exit 1`)
	passing := fakeBwrap(t, filepath.Join(dir, "system-passing"), `while [ "$1" != -- ]; do shift; done; shift; exec "$@"`)
	own := fakeBwrap(t, filepath.Join(dir, "own"), `touch `+marker+`; while [ "$1" != -- ]; do shift; done; shift; exec "$@"`)
	ownFailing := fakeBwrap(t, filepath.Join(dir, "own-failing"), `echo "own failed" >&2; exit 1`)

	fresh()
	s.probePasta(t.Context(), []string{failing, own}, time.Minute)
	assert.Equal(t, own, s.pasta, s.networkReason)

	require.NoError(t, os.Remove(marker))
	fresh()
	s.probePasta(t.Context(), []string{passing, own}, time.Minute)
	assert.Equal(t, passing, s.pasta, s.networkReason)
	assert.NoFileExists(t, marker)

	fresh()
	s.probePasta(t.Context(), []string{failing, ownFailing}, time.Minute)
	available, reason := s.NetworkStatus()
	assert.False(t, available)
	assert.Equal(t, failing+": system failed; "+ownFailing+": own failed", reason)
}

// pasta that fails a run fails it with its own words, and the command never
// starts.
func TestAPastaFailureFailsTheRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), `echo "No routable interface" >&2; exit 1`)
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: pasta}

	cmd := command(t, s, t.Context(), internetRun(t, s, "-c", "touch "+marker))
	out, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Equal(t, "No routable interface\n", withoutCoverWarning(out))
	assert.NoFileExists(t, marker)
}

// splitPastaArgs is what args, a run's under sandbox-pasta, hands pasta: its flags,
// then the command after the -- that ends them.
func splitPastaArgs(t *testing.T, args []string) (flags, command []string) {
	t.Helper()
	require.Equal(t, []string{PastaCommand, "--", "/usr/bin/pasta"}, args[1:4])
	rest := args[4:]
	end := slices.Index(rest, "--")
	return rest[:end], rest[end+1:]
}

// A run with the internet starts pasta through sandbox-pasta.
func TestARunStartsPastaThroughTheLauncher(t *testing.T) {
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}

	cmd := command(t, s, t.Context(), internetRun(t, s, "-c", "true"))

	assert.Equal(t, os.Args[0], cmd.Path)
	_, rest := splitPastaArgs(t, cmd.Args)
	assert.Equal(t, "/usr/bin/bwrap", rest[0])
}

// The probe passes pasta every flag a run passes, with probeNet in place of
// --config-net, so a pasta lacking one fails the probe rather than every run.
func TestTheProbePassesARunsPastaFlags(t *testing.T) {
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}
	r := internetRun(t, s, "-c", "true")

	run, _ := splitPastaArgs(t, command(t, s, t.Context(), r).Args)
	probe, _ := splitPastaArgs(t, slices.Concat([]string{s.self}, s.pastaArgs(s.bwrap, s.pasta, r, compiledRules(t, r.Policy), "", false)))

	assert.Equal(t, "--config-net", run[0])
	assert.Equal(t, append(slices.Clone(probeNet), run[1:]...), probe)
}

// A run with the internet makes its own user namespace inside pasta's, as the
// user, with no capability, and no network namespace of its own: pasta's is
// the run's.
func TestANetworkRunAsksBwrapForTheUsersIDsAndNoCapability(t *testing.T) {
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}

	_, rest := splitPastaArgs(t, command(t, s, t.Context(), internetRun(t, s, "-c", "true")).Args)
	bwrap := strings.Join(rest[1:], " ")

	assert.True(t, strings.HasPrefix(bwrap,
		"--unshare-user --uid "+strconv.Itoa(os.Getuid())+" --gid "+strconv.Itoa(os.Getgid())+" --cap-drop ALL --unshare-pid"), bwrap)
	assert.NotContains(t, bwrap, "--unshare-net")
	assert.NotContains(t, bwrap, "--unshare-user-try")
}

// Linux's Command refuses a run with the internet and no resolver.
func TestARunWithTheInternetNeedsAResolver(t *testing.T) {
	s := &Sandbox{self: os.Args[0], bwrap: "/usr/bin/bwrap", pasta: "/usr/bin/pasta"}
	r := internetRun(t, s, "-c", "true")
	r.Policy.Network.Resolver = ""

	_, err := s.Command(t.Context(), r)

	assert.ErrorContains(t, err, "resolver")
}

// networked is this machine's sandbox when it can give a run the internet,
// or testutil.RequireSandbox's verdict where it cannot.
func networked(t *testing.T) *Sandbox {
	t.Helper()
	s := confining(t)
	if available, reason := s.NetworkStatus(); !available {
		testutil.RequireSandbox(t, "no network for a run: "+reason)
	}
	return s
}

// netKubetail is a Kubetail directory with a run's directory in it holding its
// resolver: what a run with the internet adds to its Always part.
func netKubetail(t *testing.T) (kubetail, runDir, resolver string) {
	t.Helper()
	base := realpath.Resolved(t.TempDir())
	runDir = mkdirs(t, base, "kubetail/runs/1-a")[0]
	resolver = filepath.Join(runDir, "resolv.conf")
	require.NoError(t, os.WriteFile(resolver, []byte("nameserver "+ResolverAddress+"\n"), 0o600))
	return filepath.Join(base, "kubetail"), runDir, resolver
}

// withNet is r with the internet: its resolver in a Kubetail directory of its
// own, which it reads.
func withNet(t *testing.T, r testRun) testRun {
	t.Helper()
	kubetail, runDir, resolver := netKubetail(t)
	r.always.Kubetail = append(r.always.Kubetail, kubetail)
	r.always.Read = append(r.always.Read, runDir)
	r.Policy.Network.Internet, r.Policy.Network.Resolver = true, resolver
	return r
}

// netRun is withNet for a Run already built.
func netRun(t *testing.T, r Run) Run {
	t.Helper()
	kubetail, runDir, resolver := netKubetail(t)
	r.Policy.Always.Kubetail = append(r.Policy.Always.Kubetail, kubetail)
	r.Policy.Always.Read = append(r.Policy.Always.Read, runDir)
	r.Policy.Network.Internet, r.Policy.Network.Resolver = true, resolver
	return r
}

// The probe found pasta on a machine whose sandbox gives a run the internet.
func TestTheProbeFindsPasta(t *testing.T) {
	s := networked(t)

	assert.NotEmpty(t, s.pasta)
	assert.True(t, s.pastaOwnUserNS, "bwrap makes the run a user namespace inside pasta's")
}

// A run with the internet holds no capability and runs as the user, as one
// without it does.
func TestANetworkRunHoldsNoCapability(t *testing.T) {
	s := networked(t)

	code, out := run(t, s, withNet(t, shRun(t, "grep CapEff /proc/self/status; id -u; id -g")))

	assert.Equal(t, 0, code, out)
	assert.Equal(t, fmt.Sprintf("CapEff:\t0000000000000000\n%d\n%d\n", os.Getuid(), os.Getgid()), out)
}

// A run with the internet cannot unmount the tmpfs over a denied folder to
// read what lies under it.
func TestANetworkRunCannotUnmountANeverPath(t *testing.T) {
	s := networked(t)
	denied := realpath.Resolved(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(denied, "secret"), []byte("s3cret"), 0o600))
	r := withNet(t, shRun(t, `umount "$D" 2>&1 || echo refused; cat "$D/secret" 2>&1`, "D="+denied))
	r.always.Deny = append(r.always.Deny, denied)
	r.files.Read = append(r.files.Read, filepath.Dir(denied))

	_, out := run(t, s, r)

	assert.Contains(t, out, "refused")
	assert.NotContains(t, out, "s3cret")
}

func init() {
	// mounts calls each mount syscall with no arguments, and prints what each
	// answered.
	helpers["mounts"] = func() int {
		var got []string
		for _, nr := range mountSyscalls {
			_, _, errno := unix.Syscall6(uintptr(nr), 0, 0, 0, 0, 0, 0)
			got = append(got, errno.Error())
		}
		fmt.Print(strings.Join(got, ","))
		return 0
	}
}

// Every mount syscall answers EPERM in a run, with the internet or without.
func TestTheMountSyscallsAreRefused(t *testing.T) {
	s := networked(t)
	want := strings.TrimSuffix(strings.Repeat("operation not permitted,", len(mountSyscalls)), ",")

	_, out := run(t, s, self(t, "mounts"))
	assert.Equal(t, want, out, "without the internet")
	_, out = run(t, s, withNet(t, self(t, "mounts")))
	assert.Equal(t, want, out, "with the internet")
}

// pasta passes the command's exit on: a code, and a death by signal as the
// same run without the internet reports it.
func TestANetworkRunKeepsTheExitCode(t *testing.T) {
	s := networked(t)

	code, _ := run(t, s, withNet(t, shRun(t, "exit 7")))
	assert.Equal(t, 7, code)

	plain, _ := run(t, s, shRun(t, "kill -SEGV $$"))
	code, _ = run(t, s, withNet(t, shRun(t, "kill -SEGV $$")))
	assert.Equal(t, plain, code)
}

// defaultInterface is the interface holding the host's default route, the
// one whose address pasta copies into a run's namespace.
func defaultInterface(t *testing.T) string {
	t.Helper()
	f, err := os.Open("/proc/net/route")
	require.NoError(t, err)
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) > 1 && fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

// otherAddress is an IPv4 address the host holds on an interface other than
// loopback and the default route's, standing in for the internet: a run
// reaches it as it reaches any address but the host's loopback.
func otherAddress(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	skip := defaultInterface(t)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Name == skip {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil {
				return ip.IP.String()
			}
		}
	}
	require.FailNow(t, "the host holds no address but loopback and the default route's, so nothing stands in for the internet")
	return ""
}

// With the internet a run reaches an address of the host's other than the one
// pasta copies; without it, the same listener is out of reach.
func TestTheInternetReachesOut(t *testing.T) {
	s := networked(t)
	ln, err := net.Listen("tcp", net.JoinHostPort(otherAddress(t), "0"))
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	addr := "KUBETAIL_SANDBOX_TEST_ADDR=" + ln.Addr().String()

	code, out := run(t, s, withNet(t, self(t, "dial", addr)))
	assert.Equal(t, 0, code, out)
	assert.Equal(t, "connected\n", out)

	code, out = run(t, s, self(t, "dial", addr))
	assert.Equal(t, 1, code, out)
}

// With the internet the host's loopback stays shut, IPv4 and IPv6, while the
// run's relay port still reaches the forwarder.
func TestTheInternetReachesNoHostLoopback(t *testing.T) {
	s := networked(t)
	listeners := []string{"127.0.0.1:0"}
	if ln, err := net.Listen("tcp", "[::1]:0"); err == nil {
		_ = ln.Close()
		listeners = append(listeners, "[::1]:0")
	}
	for _, at := range listeners {
		ln, err := net.Listen("tcp", at)
		require.NoError(t, err)
		defer ln.Close()

		code, out := run(t, s, withNet(t, self(t, "dial", "KUBETAIL_SANDBOX_TEST_ADDR="+ln.Addr().String())))

		assert.Equal(t, 1, code, at)
		assert.Contains(t, out, "connection refused", at)
	}

	socket := echoSocket(t)
	r := withNet(t, self(t, "", "KUBETAIL_SANDBOX_TEST_DIAL="+strconv.Itoa(forwarderPort)))
	r.files.Read = []string{filepath.Dir(socket)}
	r.Policy.Network.Relays = []Relay{{Port: forwarderPort, Socket: socket}}
	code, out := run(t, s, r)
	assert.Equal(t, 0, code)
	assert.Equal(t, "ping|eof", out)
}

// The process count and the memory limit hold in a run with the internet.
func TestTheLimitsHoldWithTheInternet(t *testing.T) {
	s := networked(t)
	dd := []string{"/bin/sh", "-c", "dd if=/dev/zero of=/dev/null bs=1G count=1 2>&1"}
	code, out := limitedRun(t, s, Limits{MemoryBytes: 512 << 20, OpenFiles: 4096}, nil, dd...)
	require.NotEqual(t, 0, code)
	assert.Contains(t, out, "memory", "without the internet")

	r := netRun(t, limitedRunOf(t, s, Limits{MemoryBytes: 512 << 20, OpenFiles: 4096}, nil, dd...))
	got, err := command(t, s, t.Context(), r).Output()
	assert.Error(t, err)
	assert.Contains(t, string(got), "memory", "with the internet")

	if os.Getuid() == 0 {
		t.Skip("the kernel holds root to no process limit")
	}
	base, err := s.CountedProcesses(true)
	require.NoError(t, err)
	n, eagain, _ := forkLoop(t, s, Limits{Processes: base + 128, OpenFiles: 4096}, base+512, func() error { return nil },
		func(r Run) Run { return netRun(t, r) })
	assert.Less(t, n, base+128+forwarderTasks)
	assert.True(t, eagain)
}

// runToken is a variable set in a run's environment that names it, so its
// processes are found in /proc whatever namespace they are in.
func runToken(t *testing.T) string {
	return "KUBETAIL_SANDBOX_TEST_RUN=" + strconv.FormatInt(time.Now().UnixNano(), 36) + t.Name()
}

// processesOf is every process whose environment holds token, by name.
func processesOf(token string) map[int]string {
	out := map[int]string{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		env, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil || !slices.Contains(strings.Split(string(env), "\x00"), token) {
			continue
		}
		comm, _ := os.ReadFile("/proc/" + e.Name() + "/comm")
		out[pid] = strings.TrimSpace(string(comm))
	}
	return out
}

// startSleeping starts a run with the internet that sleeps in the foreground,
// in a session of its own as Bash starts one, and waits until the sleep runs.
func startSleeping(t *testing.T, s *Sandbox, token string) *exec.Cmd {
	t.Helper()
	r := withNet(t, shRun(t, "sleep 3600; echo woke", token))
	cmd := command(t, s, context.Background(), r.on(s))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	require.Eventually(t, func() bool { return slices.Contains(mapValues(processesOf(token)), "sleep") },
		testutil.Timeout, 10*time.Millisecond, "the sleep starts")
	return cmd
}

func mapValues(m map[int]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// assertRunGone waits for cmd and asserts no process of the run is left:
// pasta, bwrap, the forwarder, the shell or the sleep.
func assertRunGone(t *testing.T, cmd *exec.Cmd, token string) {
	t.Helper()
	_ = cmd.Wait()
	assert.Eventually(t, func() bool { return len(processesOf(token)) == 0 }, testutil.Timeout, 10*time.Millisecond,
		"processes left: %v", processesOf(token))
}

// A stop to the run's group, as Bash's Stop and timeout send, ends pasta,
// bwrap and the command alike.
func TestStopEndsANetworkRun(t *testing.T) {
	s := networked(t)
	token := runToken(t)
	cmd := startSleeping(t, s, token)

	require.NoError(t, syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM))

	assertRunGone(t, cmd, token)
}

// A timed-out run's group is killed once its grace is out, and nothing of it
// is left.
func TestATimeoutEndsANetworkRun(t *testing.T) {
	s := networked(t)
	token := runToken(t)
	cmd := startSleeping(t, s, token)

	require.NoError(t, syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL))

	assertRunGone(t, cmd, token)
}

// sandbox-pasta killed alone takes pasta, bwrap and the command with it.
func TestKillingTheLauncherEndsTheRun(t *testing.T) {
	s := networked(t)
	token := runToken(t)
	cmd := startSleeping(t, s, token)

	require.NoError(t, cmd.Process.Kill())

	assertRunGone(t, cmd, token)
}

func init() {
	// resolvens runs in a network and mount namespace of its own, where it is
	// root: it gives the namespace a dummy interface holding the default route,
	// which pasta copies, serves DNS on 127.0.0.1:53 with one record, binds a
	// resolv.conf naming that server over the host's, where pasta reads it, and
	// runs the lookup helper with the internet through the sandbox.
	helpers["resolvens"] = func() int {
		for _, args := range [][]string{
			{"link", "add", "d0", "type", "dummy"}, {"addr", "add", "10.99.0.2/24", "dev", "d0"},
			{"link", "set", "d0", "up"}, {"link", "set", "lo", "up"}, {"route", "add", "default", "via", "10.99.0.1"},
		} {
			if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
				fmt.Printf("ip %v: %s", args, out)
				return 1
			}
		}
		stop, err := serveDNS("127.0.0.1:53", "kubetail.test.", net.IPv4(10, 1, 2, 3))
		if err != nil {
			fmt.Print(err)
			return 1
		}
		defer stop()
		base, _ := os.MkdirTemp("", "resolvens-")
		defer os.RemoveAll(base)
		hostConf := filepath.Join(base, "resolv.conf")
		_ = os.WriteFile(hostConf, []byte("nameserver 127.0.0.1\n"), 0o644)
		if err := syscall.Mount(hostConf, "/etc/resolv.conf", "", syscall.MS_BIND, ""); err != nil {
			fmt.Print(err)
			return 1
		}
		s, v, _ := Probe(context.Background())
		defer s.Close()
		if s == nil || !v.NetworkAvailable {
			fmt.Printf("no network: %+v", v)
			return 1
		}
		self := os.Getenv("KUBETAIL_SANDBOX_TEST_SELF")
		home, _ := os.UserHomeDir()
		dir, kubetail := filepath.Join(base, "work"), filepath.Join(base, "kubetail")
		runDir := filepath.Join(kubetail, "runs", "1-a")
		_ = os.MkdirAll(dir, 0o700)
		_ = os.MkdirAll(runDir, 0o700)
		resolver := filepath.Join(runDir, "resolv.conf")
		_ = os.WriteFile(resolver, []byte("nameserver "+ResolverAddress+"\n"), 0o600)
		p := s.probePolicy(self, dir, home)
		p.Always.Kubetail, p.Always.Read = []string{kubetail}, []string{runDir}
		p.Network = NetworkPolicy{Internet: true, Resolver: resolver}
		cmd, err := s.Command(context.Background(), Run{
			Shell: self, Dir: dir, Policy: p,
			Env: []string{"KUBETAIL_SANDBOX_TEST_HELPER=lookup", "KUBETAIL_SANDBOX_TEST_NAME=kubetail.test"},
		})
		if err != nil {
			fmt.Print(err)
			return 1
		}
		out, err := cmd.CombinedOutput()
		fmt.Print(withoutCoverWarning(out))
		if err != nil {
			return 1
		}
		return 0
	}
	// netprobe runs the probe and prints whether it found the network.
	helpers["netprobe"] = func() int {
		s, v, _ := Probe(context.Background())
		_ = s.Close()
		fmt.Printf("%t %s", v.NetworkAvailable, v.NetworkReason)
		return 0
	}
}

// serveDNS answers every A query for name at addr with ip, and every other
// with NXDOMAIN, until stop.
func serveDNS(addr, name string, ip net.IP) (stop func(), err error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if reply := dnsReply(buf[:n], name, ip); reply != nil {
				_, _ = conn.WriteTo(reply, from)
			}
		}
	}()
	return func() { _ = conn.Close() }, nil
}

// dnsReply is the answer to one query: ip for an A query of name, an empty
// answer for another type of name, NXDOMAIN for anything else.
func dnsReply(q []byte, name string, ip net.IP) []byte {
	if len(q) < 12 {
		return nil
	}
	var labels []string
	i := 12
	for i < len(q) && q[i] != 0 {
		l := int(q[i])
		if i+1+l > len(q) {
			return nil
		}
		labels = append(labels, string(q[i+1:i+1+l]))
		i += 1 + l
	}
	end := i + 5
	if end > len(q) {
		return nil
	}
	question := q[12:end]
	qtype := binary.BigEndian.Uint16(q[i+1 : i+3])
	reply := append([]byte{}, q[:2]...)
	found := strings.EqualFold(strings.Join(labels, ".")+".", name)
	switch {
	case !found:
		reply = append(reply, 0x81, 0x83, 0, 1, 0, 0, 0, 0, 0, 0)
	case qtype != 1:
		reply = append(reply, 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0)
	default:
		reply = append(reply, 0x81, 0x80, 0, 1, 0, 1, 0, 0, 0, 0)
	}
	reply = append(reply, question...)
	if found && qtype == 1 {
		reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
		reply = append(reply, ip.To4()...)
	}
	return reply
}

// inOwnNetwork runs this test binary as helper in a user, network and mount
// namespace of its own, where it is root and holds loopback alone, or answers
// testutil.RequireSandbox's verdict where the machine cannot make one.
func inOwnNetwork(t *testing.T, helper string, needs ...string) string {
	t.Helper()
	for _, n := range append([]string{"unshare"}, needs...) {
		if _, err := exec.LookPath(n); err != nil {
			testutil.RequireSandbox(t, n+" not found")
		}
	}
	if err := exec.Command("unshare", "-rnm", "true").Run(); err != nil {
		testutil.RequireSandbox(t, "no network namespace of the test's own: "+err.Error())
	}
	cmd := exec.Command("unshare", "-rnm", os.Args[0])
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER="+helper, "KUBETAIL_SANDBOX_TEST_SELF="+os.Args[0])
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
	return string(out)
}

// A run with the internet resolves through its own resolv.conf, which sends
// its queries to pasta, which forwards them to the host's resolver: here a
// server on the namespace's loopback, as systemd-resolved's stub is, since the
// machine's resolvers are not the test's to rely on.
func TestTheInternetResolves(t *testing.T) {
	networked(t)

	assert.Equal(t, "10.1.2.3", inOwnNetwork(t, "resolvens", "ip"))
}

// The probe reads no route: in a namespace holding loopback alone it still
// finds the network, so a Kubetail started offline offers it once the machine
// is online. The user there is uid 0, which the probe expects of a run.
func TestTheProbeNeedsNoRoute(t *testing.T) {
	networked(t)

	assert.Equal(t, "true ", inOwnNetwork(t, "netprobe"))
}

// Without the internet a run reaches neither an address of the host's nor a
// public one: its namespace holds loopback alone.
func TestWithoutTheInternetNothingIsReached(t *testing.T) {
	s := confining(t)
	ln, err := net.Listen("tcp", net.JoinHostPort(otherAddress(t), "0"))
	require.NoError(t, err)
	defer ln.Close()

	for _, addr := range []string{ln.Addr().String(), "192.0.2.1:80"} {
		code, out := run(t, s, self(t, "dial", "KUBETAIL_SANDBOX_TEST_ADDR="+addr))

		assert.Equal(t, 1, code, addr)
		assert.Contains(t, out, "network is unreachable", addr)
	}
}

// The probe holds when the run was the user, with no capability, and its user
// namespace is its own when its uid_map differs from the sidecar's.
func TestThePastaProbeReadsTheRunsIdentity(t *testing.T) {
	ids := fmt.Sprintf("Uid: %d\nGid: %d\n", os.Getuid(), os.Getgid())

	own, err := readPastaProbe("4242 4242 1\n" + ids + "CapEff: 0000000000000000\n")
	require.NoError(t, err)
	assert.True(t, own)

	_, err = readPastaProbe("4242 4242 1\n" + ids + "CapEff: 000001ffffffffff\n")
	assert.EqualError(t, err, `the run's CapEff is "000001ffffffffff", not "0000000000000000"`)
}

// A run with the internet ends when the sidecar's lifeline closes, though
// pasta, between the sidecar and the forwarder, closes the descriptor: it is
// sandbox-pasta that watches it.
func TestARunWithTheInternetEndsWithTheLifeline(t *testing.T) {
	s := networked(t)
	cmd := command(t, s, t.Context(), withNet(t, shRun(t, "sleep 1000 & echo up; wait")).on(s))

	requireEndsWithTheLifeline(t, cmd, s)
}

// sandbox-pasta, not the forwarder, is handed the lifeline.
func TestSandboxPastaWatchesTheLifeline(t *testing.T) {
	s := lifeline(t)
	s.self, s.bwrap, s.pasta = os.Args[0], "/usr/bin/bwrap", "/usr/bin/pasta"

	cmd := command(t, s, t.Context(), internetRun(t, s, "-c", "true"))

	assert.Equal(t, []*os.File{s.lifeline}, cmd.ExtraFiles)
	assert.Equal(t, []string{PastaCommand, lifelineFlag, "3", "--", "/usr/bin/pasta"}, cmd.Args[1:6])
	assert.Equal(t, -1, index(cmd.Args[6:], lifelineFlag))
}

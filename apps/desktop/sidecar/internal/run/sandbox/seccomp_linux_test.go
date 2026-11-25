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
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// verdict is what filter answers for a syscall nr of arch with args. The
// kernel reads seccomp_data's words in the machine's order and bpf.VM reads
// them big-endian, so each word is written big-endian at its offset: nr, arch,
// then each argument's low word and high word, as they lie little-endian.
func verdict(t *testing.T, arch uint32, nr int, args ...uint64) uint32 {
	t.Helper()
	data := make([]byte, 64)
	binary.BigEndian.PutUint32(data[0:], uint32(nr))
	binary.BigEndian.PutUint32(data[4:], arch)
	for i, a := range args {
		binary.BigEndian.PutUint32(data[16+8*i:], uint32(a))
		binary.BigEndian.PutUint32(data[20+8*i:], uint32(a>>32))
	}
	vm, err := bpf.NewVM(filter())
	require.NoError(t, err)
	out, err := vm.Run(data)
	require.NoError(t, err)
	return uint32(out)
}

func errno(e unix.Errno) uint32 { return unix.SECCOMP_RET_ERRNO | uint32(e) }

// A syscall of another architecture is killed, since its numbers mean other
// calls; one of the build's own is allowed.
func TestTheFilterKillsAnotherArchitecture(t *testing.T) {
	other := uint32(unix.AUDIT_ARCH_X86_64)
	if auditArch == unix.AUDIT_ARCH_X86_64 {
		other = unix.AUDIT_ARCH_AARCH64
	}

	assert.Equal(t, uint32(unix.SECCOMP_RET_KILL_PROCESS), verdict(t, other, unix.SYS_GETPID))
	assert.Equal(t, uint32(unix.SECCOMP_RET_ALLOW), verdict(t, auditArch, unix.SYS_GETPID))
}

// A socket is IP or refused: a Unix socket would reach a resolver, and vsock
// the hypervisor, past the network namespace.
func TestTheFilterAllowsOnlyAnIPSocket(t *testing.T) {
	for _, family := range []uint64{unix.AF_UNIX, unix.AF_VSOCK, unix.AF_NETLINK, unix.AF_PACKET} {
		assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, unix.SYS_SOCKET, family, unix.SOCK_STREAM), family)
	}
	for _, family := range []uint64{unix.AF_INET, unix.AF_INET6} {
		assert.Equal(t, uint32(unix.SECCOMP_RET_ALLOW), verdict(t, auditArch, unix.SYS_SOCKET, family, unix.SOCK_STREAM), family)
	}
}

// A datagram pair is refused, since either end can send to a pathname socket
// whatever it is connected to; a stream or seqpacket pair reaches its other
// end alone.
func TestTheFilterRefusesADatagramSocketPair(t *testing.T) {
	for _, typ := range []uint64{unix.SOCK_DGRAM, unix.SOCK_DGRAM | unix.SOCK_CLOEXEC, unix.SOCK_RAW} {
		assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, unix.SYS_SOCKETPAIR, unix.AF_UNIX, typ), typ)
	}
	for _, typ := range []uint64{unix.SOCK_STREAM | unix.SOCK_NONBLOCK, unix.SOCK_SEQPACKET} {
		assert.Equal(t, uint32(unix.SECCOMP_RET_ALLOW), verdict(t, auditArch, unix.SYS_SOCKETPAIR, unix.AF_UNIX, typ), typ)
	}
}

// io_uring can open and connect a socket without calling socket, so it is
// answered as a kernel without it.
func TestTheFilterRefusesIOUring(t *testing.T) {
	for _, nr := range []int{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER} {
		assert.Equal(t, errno(unix.ENOSYS), verdict(t, auditArch, nr), nr)
	}
}

// A new user namespace is refused however it is asked for. clone3's flags sit
// behind a pointer the filter cannot read, so clone3 answers as a kernel
// without it, and glibc falls back to clone.
func TestTheFilterRefusesAUserNamespace(t *testing.T) {
	assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, unix.SYS_UNSHARE, unix.CLONE_NEWUSER|unix.CLONE_NEWNS))
	assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, unix.SYS_CLONE, unix.CLONE_NEWUSER))
	assert.Equal(t, errno(unix.ENOSYS), verdict(t, auditArch, unix.SYS_CLONE3))

	assert.Equal(t, uint32(unix.SECCOMP_RET_ALLOW), verdict(t, auditArch, unix.SYS_UNSHARE, unix.CLONE_NEWNS))
	assert.Equal(t, uint32(unix.SECCOMP_RET_ALLOW), verdict(t, auditArch, unix.SYS_CLONE, unix.CLONE_VM|unix.CLONE_THREAD))
}

func init() {
	helpers["sockets"] = func() int {
		fmt.Printf("unix=%v vsock=%v inet=%v dgram-pair=%v stream-pair=%v", socketErr(unix.AF_UNIX),
			socketErr(unix.AF_VSOCK), socketErr(unix.AF_INET), pairErr(unix.SOCK_DGRAM), pairErr(unix.SOCK_STREAM))
		return 0
	}
	helpers["sendto"] = func() int {
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
		if err == nil {
			err = unix.Sendto(fds[0], []byte("ping"), 0, &unix.SockaddrUnix{Name: os.Getenv("KUBETAIL_SANDBOX_TEST_SOCKET")})
		}
		if err != nil {
			fmt.Print(err)
			return 1
		}
		fmt.Print("sent")
		return 0
	}
}

// pairErr is why a Unix socket pair of typ cannot be made, or ok.
func pairErr(typ int) string {
	fds, err := unix.Socketpair(unix.AF_UNIX, typ, 0)
	if err != nil {
		return err.Error()
	}
	_ = unix.Close(fds[0])
	_ = unix.Close(fds[1])
	return "ok"
}

// socketErr is why a stream socket of family cannot be opened, or ok.
func socketErr(family int) string {
	fd, err := unix.Socket(family, unix.SOCK_STREAM, 0)
	if err != nil {
		return err.Error()
	}
	_ = unix.Close(fd)
	return "ok"
}

// shellCmd is the test binary as sandbox-shell over argv.
func shellCmd(argv ...string) *exec.Cmd {
	return exec.Command(os.Args[0], append([]string{ShellCommand, "--"}, argv...)...)
}

// Under sandbox-shell a process is refused a Unix or vsock socket and a
// datagram pair, and keeps TCP and a stream pair.
func TestTheShellCannotOpenAUnixOrVsockSocket(t *testing.T) {
	cmd := shellCmd(os.Args[0])
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER=sockets")

	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	assert.Equal(t, "unix=operation not permitted vsock=operation not permitted inet=ok dgram-pair=operation not permitted stream-pair=ok", string(out))
}

// The filter holds whatever the shell starts: unshare cannot make a user
// namespace.
func TestTheShellCannotMakeAUserNamespace(t *testing.T) {
	out, err := shellCmd("/bin/sh", "-c", "unshare -U true").CombinedOutput()

	require.Error(t, err)
	assert.True(t, strings.Contains(string(out), "Operation not permitted"), string(out))
}

// A shell launcher with no command is its own failure: 125 and one line.
func TestTheShellRefusesBadArguments(t *testing.T) {
	code, _, stderr := runInit(t, exec.Command(os.Args[0], ShellCommand))

	assert.Equal(t, 125, code)
	assert.True(t, strings.HasPrefix(stderr, "sandbox-shell: bad arguments: "), stderr)
}

// ShellMain refuses what it cannot run before it installs anything, so
// these run in the test's own process.
func TestShellMainRefusesBeforeItFilters(t *testing.T) {
	assert.Equal(t, 125, ShellMain(nil))
	assert.Equal(t, 125, ShellMain([]string{"--", "/nonexistent/shell"}))
}

// The filter assembles into one socket filter instruction per BPF one, the
// shape the kernel loads.
func TestTheFilterAssembles(t *testing.T) {
	got, err := assemble(filter())

	require.NoError(t, err)
	assert.Len(t, got, len(filter()))
	assert.Equal(t, unix.SockFilter{Code: 0x06, K: unix.SECCOMP_RET_ALLOW}, got[len(got)-1])
}

// A program that does not assemble is refused before the kernel is asked, so
// this runs in the test's own process.
func TestAProgramThatDoesNotAssembleIsNotInstalled(t *testing.T) {
	bad := []bpf.Instruction{bpf.LoadAbsolute{Off: 0, Size: 3}}

	_, err := assemble(bad)
	require.Error(t, err)
	assert.Error(t, install(bad))
}

// limitOf is the soft limit /proc/<pid>/limits names on the line beginning
// with name.
func limitOf(t *testing.T, limits, name string) string {
	t.Helper()
	for line := range strings.SplitSeq(limits, "\n") {
		if rest, ok := strings.CutPrefix(line, name); ok {
			return strings.Fields(rest)[0]
		}
	}
	require.Failf(t, "no limit", "%q in %s", name, limits)
	return ""
}

// sandbox-shell sets the memory and process limits on what it execs, and
// everything that starts.
func TestTheShellAppliesItsLimits(t *testing.T) {
	p := strconv.Itoa(ownProcesses(t))
	cmd := exec.Command(os.Args[0], ShellCommand, "--memory", "536870912", "--processes", p, "--", "/bin/sh", "-c", "cat /proc/self/limits")

	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	assert.Equal(t, "536870912", limitOf(t, string(out), "Max address space"))
	assert.Equal(t, p, limitOf(t, string(out), "Max processes"))
}

// A command the kernel will not exec under the limits is sandbox-shell's
// failure, written without allocating: the line, the errno, and 125.
func TestAShellThatCannotExecUnderLimitsSaysWhy(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage")
	require.NoError(t, os.WriteFile(garbage, []byte{0, 1, 2, 3}, 0o700))
	p := strconv.Itoa(ownProcesses(t))

	code, _, stderr := runInit(t, exec.Command(os.Args[0], ShellCommand, "--memory", "536870912", "--processes", p, "--", garbage))

	assert.Equal(t, 125, code)
	assert.Equal(t, "sandbox-shell: cannot start "+garbage+": errno "+strconv.Itoa(int(unix.ENOEXEC))+"\n", stderr)
}

// Tracing is refused: attaching, reading or writing another process's memory,
// taking its descriptors, or comparing kernel objects with it.
func TestTheFilterRefusesTracing(t *testing.T) {
	for _, nr := range []int{unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_PIDFD_GETFD, unix.SYS_KCMP, unix.SYS_PROCESS_MADVISE} {
		assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, nr), nr)
	}
}

func init() {
	// trace starts a sleep child and attaches to it, as a debugger does, and
	// prints why it could not, or attached.
	helpers["trace"] = func() int {
		runtime.LockOSThread()
		child := exec.Command("sleep", "60")
		if err := child.Start(); err != nil {
			fmt.Print(err)
			return 1
		}
		defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
		if err := unix.PtraceAttach(child.Process.Pid); err != nil {
			fmt.Print(err)
			return 1
		}
		fmt.Print("attached")
		return 0
	}
}

// outside skips the test unless the helper named succeeds outside the filter,
// so the filter is what refuses it inside.
func outside(t *testing.T, helper string) {
	t.Helper()
	if out, err := helperCmd(helper).CombinedOutput(); err != nil {
		t.Skipf("%s fails outside the filter: %s", helper, out)
	}
}

func TestTheShellCannotTraceAProcess(t *testing.T) {
	outside(t, "trace")
	cmd := shellCmd(os.Args[0])
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER=trace")

	out, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Equal(t, "operation not permitted", string(out))
}

func TestTheFilterRefusesTheKeyring(t *testing.T) {
	for _, nr := range []int{unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY} {
		assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, nr), nr)
	}
}

func init() {
	// keyring asks for the session keyring, which a process inherits from the
	// user's session, and prints why it could not, or its id.
	helpers["keyring"] = func() int {
		id, err := unix.KeyctlInt(unix.KEYCTL_GET_KEYRING_ID, unix.KEY_SPEC_SESSION_KEYRING, 0, 0, 0)
		if err != nil {
			fmt.Print(err)
			return 1
		}
		fmt.Print(id)
		return 0
	}
}

func TestTheShellCannotReadTheKeyring(t *testing.T) {
	outside(t, "keyring")
	cmd := shellCmd(os.Args[0])
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER=keyring")

	out, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Equal(t, "operation not permitted", string(out))
}

// mountSyscalls is every syscall that mounts, unmounts or moves a mount.
var mountSyscalls = []int{
	unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT,
	unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_MOUNT_SETATTR,
}

// No run mounts anything, so a capability that slipped through unmounts
// nothing.
func TestTheFilterRefusesMounts(t *testing.T) {
	for _, nr := range mountSyscalls {
		assert.Equal(t, errno(unix.EPERM), verdict(t, auditArch, nr), nr)
	}
}

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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// ShellMain is sandbox-shell, given the arguments after the subcommand: it
// installs the filter, sets the run's limits and execs the command under
// both, so they hold the command and everything it starts. It answers only if
// it cannot.
func ShellMain(args []string) int {
	a, err := parseShellArgs(args)
	if err != nil {
		return fail(os.Stderr, ShellCommand, "bad arguments", err)
	}
	path, err := exec.LookPath(a.argv[0])
	if err != nil {
		return fail(os.Stderr, ShellCommand, "cannot start "+a.argv[0], err)
	}
	// no_new_privs holds for the calling thread alone, and seccomp refuses a
	// thread without it; the exec below runs on this one.
	// coverage:ignore-start only sandbox-shell runs this, in a child that writes no coverage
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fail(os.Stderr, ShellCommand, "cannot set no_new_privs", err)
	}
	if err := install(filter()); err != nil {
		return fail(os.Stderr, ShellCommand, "cannot install the filter", err)
	}
	if err := setTimeAndFiles(a); err != nil {
		return fail(os.Stderr, ShellCommand, "cannot set limits", err)
	}
	if a.memory > 0 || a.processes > 0 {
		return execLimited(path, a)
	}
	err = unix.Exec(path, a.argv, os.Environ())
	return fail(os.Stderr, ShellCommand, "cannot start "+a.argv[0], err)
	// coverage:ignore-stop
}

// execLimited execs path under a's process and memory limits. Past the
// address-space limit an allocation is a runtime crash, so everything the
// exec needs, its failure line included, is built first, the collector is
// off, and nothing from the last limit to the exit allocates. It answers only
// if it cannot set the limits.
func execLimited(path string, a shellArgs) int { // coverage:ignore only sandbox-shell runs this, in a child that writes no coverage
	debug.SetGCPercent(-1)
	pathp, err1 := syscall.BytePtrFromString(path)
	argvp, err2 := syscall.SlicePtrFromStrings(a.argv)
	envp, err3 := syscall.SlicePtrFromStrings(os.Environ())
	if err := errors.Join(err1, err2, err3); err != nil {
		return fail(os.Stderr, ShellCommand, "cannot start "+a.argv[0], err)
	}
	prefix := "sandbox-shell: cannot start " + a.argv[0] + ": errno "
	line := append(make([]byte, 0, len(prefix)+24), prefix...)

	if a.processes > 0 {
		if err := setClamped(unix.RLIMIT_NPROC, a.processes, 0); err != nil {
			return fail(os.Stderr, ShellCommand, "cannot set limits", err)
		}
	}
	if a.memory > 0 {
		as, err := clamped(unix.RLIMIT_AS, a.memory, 0)
		if err != nil {
			return fail(os.Stderr, ShellCommand, "cannot set limits", err)
		}
		// The scheduler allocates when it starts a thread, which it can do on
		// preempting this goroutine, or to look for work for an idle
		// processor. With one processor there is no idle one, and the yield
		// restarts the time slice and leaves an idle thread for a preemption
		// to reuse; the raw syscall is not one the scheduler sees.
		runtime.GOMAXPROCS(1)
		runtime.Gosched()
		if _, _, errno := unix.RawSyscall6(unix.SYS_PRLIMIT64, 0, unix.RLIMIT_AS, uintptr(unsafe.Pointer(&as)), 0, 0, 0); errno != 0 {
			return fail(os.Stderr, ShellCommand, "cannot set limits", errno)
		}
	}
	_, _, errno := unix.RawSyscall(unix.SYS_EXECVE,
		uintptr(unsafe.Pointer(pathp)), uintptr(unsafe.Pointer(&argvp[0])), uintptr(unsafe.Pointer(&envp[0])))
	line = strconv.AppendUint(line, uint64(errno), 10)
	line = append(line, '\n')
	_, _, _ = unix.RawSyscall(unix.SYS_WRITE, 2, uintptr(unsafe.Pointer(&line[0])), uintptr(len(line)))
	_, _, _ = unix.RawSyscall(unix.SYS_EXIT_GROUP, initFailed, 0, 0)
	return initFailed
}

// install loads prog on every thread of the process, since the Go runtime
// runs several.
func install(prog []bpf.Instruction) error {
	filters, err := assemble(prog)
	if err != nil {
		return err
	}
	// coverage:ignore-start only sandbox-shell runs this, in a child that writes no coverage
	fprog := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	tid, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER,
		unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&fprog)))
	switch {
	case errno != 0:
		return errno
	case tid != 0:
		return fmt.Errorf("thread %d could not take it", tid)
	}
	return nil
	// coverage:ignore-stop
}

// Offsets into seccomp_data: the syscall's number, its architecture, and the
// low words of its first two arguments, which is enough for every flag it
// reads.
const (
	offNr   = 0
	offArch = 4
	offArg0 = 16
	offArg1 = 24
)

// sockTypeMask is a socket type's bits below SOCK_NONBLOCK and SOCK_CLOEXEC.
const sockTypeMask = 0xf

var (
	allow  = bpf.RetConstant{Val: unix.SECCOMP_RET_ALLOW}
	eperm  = bpf.RetConstant{Val: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)}
	enosys = bpf.RetConstant{Val: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)}
)

// filter is the seccomp program sandbox-shell installs over the shell.
func filter() []bpf.Instruction {
	prog := []bpf.Instruction{
		bpf.LoadAbsolute{Off: offArch, Size: 4},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: auditArch, SkipTrue: 1},
		bpf.RetConstant{Val: unix.SECCOMP_RET_KILL_PROCESS},
		bpf.LoadAbsolute{Off: offNr, Size: 4},
	}
	prog = append(prog, archRules...)
	prog = append(prog,
		// IP alone, which the network namespace fences. A Unix socket reaches
		// the host's resolvers and buses, and vsock the hypervisor and other
		// VMs, and no namespace fences either.
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_SOCKET, SkipFalse: 5},
		bpf.LoadAbsolute{Off: offArg0, Size: 4},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.AF_INET, SkipTrue: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.AF_INET6, SkipTrue: 1},
		eperm,
		allow,

		// A stream or seqpacket pair reaches its other end alone, but a
		// datagram end can send to a pathname socket, and a raw Unix pair is
		// a datagram one.
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_SOCKETPAIR, SkipFalse: 6},
		bpf.LoadAbsolute{Off: offArg1, Size: 4},
		bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: sockTypeMask},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SOCK_STREAM, SkipTrue: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SOCK_SEQPACKET, SkipTrue: 1},
		eperm,
		allow,
	)
	for _, nr := range []uint32{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER, unix.SYS_CLONE3} {
		prog = append(prog, bpf.JumpIf{Cond: bpf.JumpEqual, Val: nr, SkipFalse: 1}, enosys)
	}
	// No process traces, reads or writes another, takes its descriptors, or
	// compares kernel objects with it, its own children included. Nor does
	// one reach the kernel keyring, which bwrap leaves the user's session's.
	for _, nr := range []uint32{
		unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_PIDFD_GETFD, unix.SYS_KCMP, unix.SYS_PROCESS_MADVISE,
		unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
		// No run mounts or unmounts anything: bwrap has made every mount
		// before the filter, and a capability that slipped through must not
		// unmount the tmpfs over a denied path.
		unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_OPEN_TREE, unix.SYS_MOVE_MOUNT,
		unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_MOUNT_SETATTR,
	} {
		prog = append(prog, bpf.JumpIf{Cond: bpf.JumpEqual, Val: nr, SkipFalse: 1}, eperm)
	}
	return append(prog,
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_UNSHARE, SkipTrue: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_CLONE, SkipFalse: 4},
		bpf.LoadAbsolute{Off: offArg0, Size: 4},
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: unix.CLONE_NEWUSER, SkipFalse: 1},
		eperm,
		allow,

		allow,
	)
}

// assemble is prog in the form the kernel loads.
func assemble(prog []bpf.Instruction) ([]unix.SockFilter, error) {
	raw, err := bpf.Assemble(prog)
	if err != nil {
		return nil, err
	}
	filters := make([]unix.SockFilter, len(raw))
	for i, r := range raw {
		filters[i] = unix.SockFilter{Code: r.Op, Jt: r.Jt, Jf: r.Jf, K: r.K}
	}
	return filters, nil
}

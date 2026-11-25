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

//go:build windows

package bash

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// DefaultPath is empty: Windows has no sandbox, so no run is built with it.
const DefaultPath = ""

// pipeGrace bounds the wait for the output pipe to close once the job has
// been terminated: nothing can leave the job, but a handle inherited by
// something outside it could still hold the pipe, and the call must end
// without it.
const pipeGrace = time.Second

// drainPipe is never reached: a Windows pipe takes no read deadline, so awaitCopy
// closes it instead.
func drainPipe(io.Writer, *os.File) {} // coverage:ignore never called on Windows

// The codes a stop terminates the job with, the ones a shell reports for the
// signal Unix would send: there is no SIGTERM to give a job.
const (
	timeoutCode = 143
	cancelCode  = 137
)

// hooks order a race in a test; production runs with none.
type hooks struct {
	after      func(d time.Duration) <-chan time.Time // the call's deadline; time.After when nil
	onPipe     func(w windows.Handle)                 // a duplicate of the output pipe's write end
	beforeReap func(ctx context.Context)
	onStop     func(s stop)
	afterReap  func(ctx context.Context)
	sys        *sys // the Windows calls; winSys when nil
}

// sys is the Windows calls that start a child, which no test can make fail
// any other way.
type sys struct {
	pipe                       func() (r, w *os.File, err error)
	setHandleInformation       func(h windows.Handle, mask, flags uint32) error
	createFile                 func(name *uint16, access, mode uint32, sa *windows.SecurityAttributes, disposition, attrs uint32, template windows.Handle) (windows.Handle, error)
	createJobObject            func(sa *windows.SecurityAttributes, name *uint16) (windows.Handle, error)
	setInformationJobObject    func(job windows.Handle, class uint32, info uintptr, length uint32) (int, error)
	newProcThreadAttributeList func(maxAttrCount uint32) (*windows.ProcThreadAttributeListContainer, error)
	updateProcThreadAttribute  func(al *windows.ProcThreadAttributeListContainer, attribute uintptr, value unsafe.Pointer, size uintptr) error
	assignProcessToJobObject   func(job, process windows.Handle) error
	resumeThread               func(thread windows.Handle) (uint32, error)
	getExitCodeProcess         func(process windows.Handle, code *uint32) error
}

var winSys = sys{
	pipe:                       os.Pipe,
	setHandleInformation:       windows.SetHandleInformation,
	createFile:                 windows.CreateFile,
	createJobObject:            windows.CreateJobObject,
	setInformationJobObject:    windows.SetInformationJobObject,
	newProcThreadAttributeList: windows.NewProcThreadAttributeList,
	updateProcThreadAttribute:  (*windows.ProcThreadAttributeListContainer).Update,
	assignProcessToJobObject:   windows.AssignProcessToJobObject,
	resumeThread:               windows.ResumeThread,
	getExitCodeProcess:         windows.GetExitCodeProcess,
}

func (h hooks) calls() *sys {
	if h.sys != nil {
		return h.sys
	}
	return &winSys
}

// findShell trusts only Git for Windows' own install record. A bash.exe on
// PATH is of unknown provenance — System32's is the WSL launcher, whose shell
// sees a Linux filesystem and a kubeconfig of its own — so PATH is never
// consulted and the name alone is never trusted.
func findShell() (path, kind string, ok bool) {
	path, ok = findGitBash()
	return path, "bash", ok
}

// hideWindow starts cmd with no console: bash.exe is a console-subsystem binary,
// and the sidecar has none to lend it.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

// readInstallPath is the registry read, a seam so the lookup's rules are
// testable on a machine with no Git for Windows. Production never touches it.
var readInstallPath = registryInstallPath

func registryInstallPath() (string, bool) {
	return installPathUnder(`SOFTWARE\GitForWindows`)
}

// installPathUnder is the InstallPath recorded under key in the first view
// that has one.
func installPathUnder(key string) (string, bool) {
	views := []struct {
		root   registry.Key
		access uint32
	}{
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY}, // the usual machine-wide install
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY}, // a 32-bit Git, under WOW6432Node
		{registry.CURRENT_USER, 0},                     // a per-user install
	}
	for _, v := range views {
		k, err := registry.OpenKey(v.root, key, registry.QUERY_VALUE|v.access)
		if err != nil {
			continue
		}
		path, _, err := k.GetStringValue("InstallPath")
		k.Close()
		if err == nil && path != "" {
			return path, true
		}
	}
	return "", false
}

func findGitBash() (string, bool) {
	install, ok := readInstallPath()
	if !ok {
		return "", false
	}
	bash := filepath.Join(install, "bin", "bash.exe")
	info, err := os.Stat(bash)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return bash, true
}

// run starts bash on a script file inside a job object. The command never
// rides a Windows command line: the kernel takes one string and the MSYS
// runtime parses it back, so the approved bytes go to a file bash reads —
// what the approval request showed is what bash is handed. A stop terminates
// the job, recorded as it is sent under a lock the reap closes, as on Unix.
func run(parent context.Context, s spec) result {
	fail := func(err error) result { return result{Error: err.Error()} }

	// A context that already ended runs nothing, as exec.CommandContext's
	// Start refuses one on Unix: the write ahead of a command can take a
	// while, and the cancel may have landed during it.
	if err := parent.Err(); err != nil {
		return fail(err)
	}

	// The script sits in the app's own directory, never in the user's home.
	// Read-only: bash reads a script incrementally, so an appendable one would
	// let the running command extend itself past what the approval request
	// showed; os.Remove clears the attribute itself.
	if err := os.MkdirAll(s.scripts, 0o700); err != nil {
		return fail(err)
	}
	script, err := writeScript(s.scripts, s.command)
	if err != nil {
		return fail(err)
	}
	defer os.Remove(script)

	c, err := start(s.shell, []string{script}, s.dir, s.env, s.hooks)
	if err != nil {
		return fail(err)
	}
	defer c.close()

	// The watcher starts before the resume, so a stop from here on terminates
	// the job with the child still suspended. Its join is deferred after
	// c.close (LIFO), so it runs first: a watcher still in flight could
	// otherwise terminate whatever job Windows next hands the number. The join
	// cancels ctx, and the reap has refused the stop by then.
	ctx, cancel := s.hooks.deadline(parent, s.timeout)
	g := newGuard(s.hooks)
	watcherGone := make(chan struct{})
	go func() {
		defer close(watcherGone)
		<-ctx.Done()
		why := stopFor(parent)
		if !g.record(why) {
			return
		}
		code := uint32(timeoutCode)
		if why == stopCancel {
			code = cancelCode
		}
		_ = windows.TerminateJobObject(c.job, code)
	}()
	defer func() { cancel(); <-watcherGone }()

	if err := c.resume(); err != nil {
		_ = windows.TerminateJobObject(c.job, 1)
		return fail(err)
	}

	out := &captureWriter{limit: s.capture, onKeep: s.onOutput}
	read := copyOutput(out, c.out)

	s.hooks.waitBeforeReap(ctx)
	_, _ = windows.WaitForSingleObject(c.pi.Process, windows.INFINITE)
	res := result{Stop: g.reap()}
	// Again after bash exits, so a `sleep 120 &` is gone before the result
	// is read. It cannot change bash's own code.
	_ = windows.TerminateJobObject(c.job, 1)
	s.hooks.waitAfterReap(ctx)
	awaitCopy(read, c.out, s.pipeGrace)

	res.Output, res.Discarded = string(out.buf), out.discarded
	var code uint32
	if c.sys.getExitCodeProcess(c.pi.Process, &code) != nil {
		res.CodeUnknown = true
	} else {
		res.ExitCode = int(code)
	}
	return res
}

// child is a bash started suspended inside a kill-on-close job object, its
// stdout and stderr on the far end of out.
type child struct {
	job windows.Handle
	pi  windows.ProcessInformation
	out *os.File
	sys *sys
}

// start builds the child with CreateProcess directly, not exec.Cmd, which never
// hands out the main thread's handle: started suspended and assigned to the job,
// left for the caller to resume, since a running start leaves a window in which
// bash can fork outside the job. The cost is owned here — escaping, the
// environment block, the handle list, the error text are this file's, not
// os/exec's. stdin is NUL, and only the pipe's write end is made inheritable.
// h.onPipe, a test's hook, is handed a duplicate of that end. Everything in the
// job dies with TerminateJobObject, and KILL_ON_JOB_CLOSE is the backstop: a
// sidecar crash closes the handle and the descendants die with it.
func start(shell string, args []string, dir string, env []string, h hooks) (child, error) {
	sys := h.calls()
	r, w, err := sys.pipe()
	if err != nil {
		return child{}, err
	}
	fail := func(err error) (child, error) {
		r.Close()
		return child{}, err
	}
	// The parent's copies of the inheritable handles go whatever happened; the
	// child holds its own.
	defer w.Close()
	wh := windows.Handle(w.Fd())
	if err := sys.setHandleInformation(wh, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return fail(err)
	}
	// Opened inheritable by hand: there is no exec.Cmd to supply the null device.
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	nul, err := sys.createFile(windows.StringToUTF16Ptr("NUL"), windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, sa, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(nul)

	job, err := sys.createJobObject(nil, nil)
	if err != nil {
		return fail(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := sys.setInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return fail(err)
	}

	pi, err := startSuspended(sys, shell, args, dir, env, wh, nul)
	if err != nil {
		windows.CloseHandle(job)
		return fail(err)
	}
	if h.onPipe != nil {
		var dup windows.Handle
		self := windows.CurrentProcess()
		if windows.DuplicateHandle(self, wh, self, &dup, 0, false, windows.DUPLICATE_SAME_ACCESS) == nil {
			h.onPipe(dup)
		}
	}
	if err := sys.assignProcessToJobObject(job, pi.Process); err != nil {
		// Suspended and outside the job: nothing has run, and the job's
		// guarantee cannot be made — end it rather than resume it.
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(job)
		return fail(err)
	}
	return child{job: job, pi: pi, out: r, sys: sys}, nil
}

// resume lets the child run, and closes the thread handle nothing needs again.
func (c child) resume() error {
	_, err := c.sys.resumeThread(c.pi.Thread)
	windows.CloseHandle(c.pi.Thread)
	return err
}

// close releases the child's handles. Closing the job is what kills anything
// still in it, so it comes after every wait on the process.
func (c child) close() {
	_ = c.out.Close()
	windows.CloseHandle(c.pi.Process)
	windows.CloseHandle(c.job)
}

// writeScript puts the command, byte for byte, in a read-only file under
// root, named so nothing else could hold it.
func writeScript(root, command string) (string, error) {
	path := filepath.Join(root, "cmd-"+rand.Text())
	if err := os.WriteFile(path, []byte(command), 0o400); err != nil {
		return "", err
	}
	return path, nil
}

// startSuspended is the CreateProcess call: the command line is bash and args,
// all generated by this package, each through syscall.EscapeArg — the escaping
// exec.Cmd itself uses — and the child inherits exactly the two handles the
// attribute list names. EXTENDED_STARTUPINFO_PRESENT is what makes
// CreateProcess honour that list; without it the call silently inherits every
// inheritable handle instead.
func startSuspended(sys *sys, bash string, args []string, dir string, extra []string, wh, nul windows.Handle) (windows.ProcessInformation, error) {
	var pi windows.ProcessInformation

	line := syscall.EscapeArg(bash)
	for _, arg := range args {
		line += " " + syscall.EscapeArg(arg)
	}
	cmdline, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return pi, err
	}
	dirp, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return pi, err
	}
	env, err := environBlock(extra)
	if err != nil {
		return pi, err
	}

	attrs, err := sys.newProcThreadAttributeList(1)
	if err != nil {
		return pi, err
	}
	defer attrs.Delete()
	inherited := []windows.Handle{nul, wh}
	if err := sys.updateProcThreadAttribute(attrs, windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&inherited[0]), uintptr(len(inherited))*unsafe.Sizeof(inherited[0])); err != nil {
		return pi, err
	}

	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = nul
	si.StdOutput = wh
	si.StdErr = wh

	// CREATE_NO_WINDOW: bash.exe is a console-subsystem binary, and the
	// sidecar has no console, so without it every command flashes one.
	err = windows.CreateProcess(nil, cmdline, nil, nil, true,
		windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT|
			windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_NO_WINDOW,
		&env[0], dirp, &si.StartupInfo, &pi)
	return pi, err
}

// environBlock is os.Environ() and extra as CreateProcess takes it: UTF-16 entries,
// NUL-separated, double-NUL-terminated, sorted by name as Windows requires, an
// entry of extra replacing one of its name.
// The =C: drive-cwd entries go through — per-drive relative paths must
// resolve in the child as in the user's own Git Bash. An entry holding a NUL
// is refused, never silently truncating the block.
func environBlock(extra []string) ([]uint16, error) {
	name := func(e string) string { return strings.ToUpper(envName(e)) }
	env := lastOfEachName(append(os.Environ(), extra...), name)
	sort.SliceStable(env, func(i, j int) bool {
		return name(env[i]) < name(env[j])
	})
	var block []uint16
	for _, e := range env {
		u, err := syscall.UTF16FromString(e)
		if err != nil {
			return nil, errors.New("environment entry contains NUL")
		}
		block = append(block, u...)
	}
	if len(block) == 0 {
		block = append(block, 0) // an empty environment is still a double NUL
	}
	return append(block, 0), nil
}

// envName is the entry up to its separator; a =C: entry's name starts at the
// leading '=' and ends at the next.
func envName(entry string) string {
	if entry == "" {
		return ""
	}
	if i := strings.IndexByte(entry[1:], '='); i >= 0 {
		return entry[:i+1]
	}
	return entry
}

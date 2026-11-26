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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
)

// keysFDFlag carries the provider keys across the re-exec: the number of a pipe
// the previous image wrote them to. It is set by scrubExecEnvironment alone and
// never by a launcher, and never carries a key itself: argv is as readable as
// the environment.
const keysFDFlag = "--keys-fd="

// scrubExecEnvironment is scrubExec over the process: main's first step.
func scrubExecEnvironment() ([]string, func(string) string, error) {
	return scrubExec(os.Args, os.Environ(), syscall.Exec)
}

// execFunc is syscall.Exec's shape: it returns only on failure.
type execFunc func(argv0 string, argv []string, envv []string) error

// scrubExec keeps the provider keys out of the strings the kernel
// keeps from the exec: the environment and arguments another process of this
// user can read, through kern.procargs2 on macOS before 27 and /proc/<pid>/environ
// on Linux, which os.Unsetenv never touches. Started with a key variable set, it
// writes the keys to a pipe and execs the same binary with the same arguments,
// keysFDFlag added and the key variables left out; the pid and every open file
// stay. In the new image, keysFDFlag names the pipe to read the keys from. With no
// key set there is nothing to hide and no exec.
//
// It returns the arguments main parses and the environment the config reads,
// which answers each key from the pipe and everything else from os.Getenv. A
// pipe that cannot be read is an error, as is a key variable set beside the
// flag, which the re-exec never leaves: the sidecar stops rather than start
// without the keys it was given, or with one in its launch strings. exec
// returns only on failure.
func scrubExec(args, environ []string, exec execFunc) ([]string, func(string) string, error) {
	rest, fd, found, err := takeKeysFD(args)
	if err != nil {
		return nil, nil, err
	}
	keys, kept := splitKeys(environ)
	if found {
		if len(keys) > 0 {
			return nil, nil, fmt.Errorf("%s%d: a provider key variable is set beside it", keysFDFlag, fd)
		}
		keys, err := readKeysFD(fd)
		if err != nil {
			return nil, nil, fmt.Errorf("%s%d: %w", keysFDFlag, fd, err)
		}
		getenv := func(name string) string {
			if v, ok := keys[name]; ok {
				return v
			}
			return os.Getenv(name)
		}
		return rest, getenv, nil
	}

	if len(keys) == 0 {
		return args, os.Getenv, nil
	}
	exe, err := os.Executable()
	if err != nil { // coverage:ignore os.Executable resolves wherever the sidecar runs
		return nil, nil, fmt.Errorf("re-exec without the provider keys: %w", err)
	}
	r, err := writeKeysPipe(keys)
	if err != nil {
		return nil, nil, fmt.Errorf("re-exec without the provider keys: %w", err)
	}
	argv := append(append([]string(nil), args...), keysFDFlag+strconv.Itoa(int(r.Fd())))
	err = exec(exe, argv, kept)
	_ = r.Close()
	return nil, nil, fmt.Errorf("re-exec without the provider keys: %w", err)
}

// takeKeysFD removes keysFDFlag from args and returns the number it carried.
func takeKeysFD(args []string) (rest []string, fd int, found bool, err error) {
	for i, a := range args {
		v, ok := strings.CutPrefix(a, keysFDFlag)
		if !ok {
			continue
		}
		fd, err = strconv.Atoi(v)
		if err != nil {
			return nil, 0, false, fmt.Errorf("%s%s: not a file descriptor", keysFDFlag, v)
		}
		rest = append(append([]string(nil), args[:i]...), args[i+1:]...)
		return rest, fd, true, nil
	}
	return args, 0, false, nil
}

// splitKeys sorts environ into the set key variables, by name, and the rest.
// The first of a name wins, as it does for os.Getenv; an empty one hides
// nothing and stays.
func splitKeys(environ []string) (keys map[string]string, kept []string) {
	keyVars := map[string]bool{}
	for _, name := range catalog.KeyVars() {
		keyVars[name] = true
	}
	keys = map[string]string{}
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if keyVars[name] && value != "" {
			if _, seen := keys[name]; !seen {
				keys[name] = value
			}
			continue
		}
		kept = append(kept, kv)
	}
	return keys, kept
}

// writeKeysPipe writes keys to a new pipe and returns its read end, which
// survives the exec. Nothing reads the pipe until the exec, so the write never
// blocks: it is non-blocking, and keys the pipe cannot hold whole are an
// error. The write end is closed so the reader sees their end.
func writeKeysPipe(keys map[string]string) (*os.File, error) {
	payload, err := json.Marshal(keys)
	if err != nil { // coverage:ignore a map of strings always marshals
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil { // coverage:ignore a pipe fails only with no descriptor free, which main's first step never meets
		return nil, err
	}
	wfd := int(w.Fd())
	err = unix.SetNonblock(wfd, true)
	if err == nil {
		var n int
		n, err = unix.Write(wfd, payload)
		if err == nil && n != len(payload) {
			err = fmt.Errorf("the provider keys do not fit the pipe")
		}
	}
	_ = w.Close()
	if err == nil {
		// os.Pipe sets close-on-exec on both ends.
		_, err = unix.FcntlInt(r.Fd(), unix.F_SETFD, 0)
	}
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

// readKeysFD reads the keys the previous image wrote to fd and closes it,
// whatever it was. Only a pipe is read: anything else was not written by
// scrubExec.
func readKeysFD(fd int) (map[string]string, error) {
	f := os.NewFile(uintptr(fd), "keys")
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeNamedPipe == 0 {
		return nil, fmt.Errorf("not a pipe")
	}
	payload, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var keys map[string]string
	if err := json.Unmarshal(payload, &keys); err != nil {
		return nil, fmt.Errorf("keys: %w", err)
	}
	return keys, nil
}

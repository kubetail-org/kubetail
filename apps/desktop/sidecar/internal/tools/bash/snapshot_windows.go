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
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// shims are Unix's alone. Here KUBETAIL_SIDECAR_PID is a Windows pid, Git Bash's
// kill takes MSYS pids, and taskkill passes around any shim, so one would guard
// nothing.
const shims = ""

// launchDump runs the dump in Git Bash as an interactive login shell, started
// the way run starts a command, in the home directory. The dump goes to a file
// under scripts and the command line carries only its path, so Git Bash's own
// command-line parsing never sees it. The job is terminated and the file removed
// before it returns. reason is "" when the dump arrived whole.
func (t *Tool) launchDump(ctx context.Context) (out []byte, reason string, code int) {
	return t.launchDumpWith(ctx, hooks{})
}

// launchDumpWith is launchDump under h, a test's.
func (t *Tool) launchDumpWith(ctx context.Context, h hooks) (out []byte, reason string, code int) {
	const noShell = "no shell"
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, noShell, -1
	}
	if err := os.MkdirAll(t.scripts, 0o700); err != nil {
		return nil, noShell, -1
	}
	dump, err := writeScript(t.scripts, dumpCommand(t.kind))
	if err != nil {
		return nil, noShell, -1
	}
	defer os.Remove(dump)

	args := []string{"-l", "-i", "-c", `\builtin '.' ` + quote(filepath.ToSlash(dump))}
	c, err := start(t.shell, args, home, []string{"DISABLE_AUTO_UPDATE=true"}, h)
	if err != nil {
		return nil, noShell, -1
	}
	defer c.close()
	if err := c.resume(); err != nil {
		_ = windows.TerminateJobObject(c.job, 1)
		return nil, noShell, -1
	}

	// Buffered, so the reader never blocks once this returns.
	read := make(chan dumpRead, 1)
	go func() { read <- readDump(c.out, t.snapLimit) }()
	var res dumpRead
	select {
	case res = <-read:
	case <-ctx.Done():
		res = dumpRead{reason: "timeout"}
	}

	// Whatever happened: nothing the rc files started outlives the launch.
	_ = windows.TerminateJobObject(c.job, 1)
	_, _ = windows.WaitForSingleObject(c.pi.Process, windows.INFINITE)
	code = -1
	var exit uint32
	if res.reason != "timeout" && c.sys.getExitCodeProcess(c.pi.Process, &exit) == nil {
		code = int(exit)
	}
	return res.out, res.reason, code
}

// dumpRead is what reading the dump concluded; reason is "" once it is whole.
type dumpRead struct {
	out    []byte
	reason string
}

// readDump reads r until the dump's end marker arrives, it runs past limit, or
// it ends, never waiting for EOF past the marker.
func readDump(r io.Reader, limit int) dumpRead {
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			from := buf.Len()
			buf.Write(chunk[:n])
			if snapshotDone(buf.Bytes(), from) {
				return dumpRead{out: buf.Bytes()}
			}
			if buf.Len() > limit {
				return dumpRead{reason: "output limit"}
			}
		}
		if err != nil {
			return dumpRead{reason: "bad output"}
		}
	}
}

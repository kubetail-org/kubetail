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

package ipc

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"
)

// peerOf reports the client process on the other end of the pipe. Windows has
// no uid; the pipe's owner-only DACL already pins the account, so the uid is
// reported as this process's own and only the pid check carries meaning.
func peerOf(conn net.Conn) (peer, error) {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return peer{}, fmt.Errorf("peer credentials unavailable on %T", conn)
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return peer{}, fmt.Errorf("GetNamedPipeClientProcessId: %w", err)
	}
	return peer{pid: int(pid), uid: os.Getuid()}, nil
}

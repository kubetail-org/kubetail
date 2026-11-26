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

//go:build darwin

package ipc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// peerCreds is the one step that differs per Unix; Darwin splits the pid and the
// uid across two socket options.
func peerCreds(fd uintptr) (peer, error) {
	pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	if err != nil {
		return peer{}, fmt.Errorf("LOCAL_PEERPID: %w", err)
	}
	xucred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil { // coverage:ignore LOCAL_PEERPID just succeeded on the same socket
		return peer{}, fmt.Errorf("LOCAL_PEERCRED: %w", err)
	}
	return peer{pid: pid, uid: int(xucred.Uid)}, nil
}

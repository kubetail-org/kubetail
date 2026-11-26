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

//go:build linux

package ipc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// peerCreds is the one step that differs per Unix; Linux carries both halves on
// a single socket option.
func peerCreds(fd uintptr) (peer, error) {
	ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return peer{}, fmt.Errorf("SO_PEERCRED: %w", err)
	}
	return peer{pid: int(ucred.Pid), uid: int(ucred.Uid)}, nil
}

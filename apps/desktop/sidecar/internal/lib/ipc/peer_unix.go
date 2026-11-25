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

package ipc

import (
	"fmt"
	"net"
	"syscall"
)

// peerOf reads the peer's credentials, which the kernel stamps on the socket at
// connect time — the client cannot choose them. Every connection this package
// accepts is a *net.UnixConn; anything else is a programming error.
func peerOf(conn net.Conn) (peer, error) {
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return peer{}, fmt.Errorf("peer credentials unavailable on %T", conn)
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return peer{}, err
	}

	var p peer
	var credErr error
	if err := raw.Control(func(fd uintptr) { p, credErr = peerCreds(fd) }); err != nil {
		return peer{}, fmt.Errorf("control: %w", err)
	}
	return p, credErr
}

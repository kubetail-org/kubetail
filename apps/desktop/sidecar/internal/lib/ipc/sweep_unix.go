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
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// SweepStale removes the sidecar sockets in dir that refuse a dial: what a
// sidecar the host killed left, since it never ran its own removal. It leaves
// own, anything that is not a socket, and every socket that accepts the dial
// or fails it any other way, since a timeout means something still listens.
// Sidecars of hosts with different data directories share dir, so a live
// socket is told by the dial alone; that dial draws a "rejected IPC peer"
// warning in the live sidecar's log.
func SweepStale(dir, own string) {
	// Glob fails only on a malformed pattern.
	socks, _ := filepath.Glob(filepath.Join(dir, "kubetail-sidecar-*.sock"))
	for _, sock := range socks {
		if sock == own {
			continue
		}
		info, err := os.Lstat(sock)
		if err != nil || info.Mode().Type() != os.ModeSocket {
			continue
		}
		conn, err := net.DialTimeout("unix", sock, time.Second)
		if err == nil {
			conn.Close()
			continue
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			continue
		}
		if err := os.Remove(sock); err != nil {
			slog.Warn("could not remove a stale sidecar socket; the next start tries again", "socket", sock, "err", err)
		}
	}
}

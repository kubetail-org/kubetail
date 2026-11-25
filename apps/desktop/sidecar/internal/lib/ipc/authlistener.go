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

package ipc

import (
	"log/slog"
	"net"
	"os"
)

// peer identifies the process on the other end of an accepted connection.
type peer struct {
	pid int
	uid int
}

// Policy states who may speak to the endpoint. The socket's file mode (or the
// pipe's DACL) already keeps other users out; the pid is what additionally
// keeps *other processes of this user* out, which is the whole point.
type Policy struct {
	// HostPID is the process allowed to connect. Zero disables the pid check —
	// a standalone sidecar run has no host to pin, and falls back to the uid.
	HostPID int
}

// Authenticated rejects any connection whose peer the policy disallows. A
// rejected peer is closed and accepting continues: an unauthorized process
// must not be able to end the serving loop.
//
// The pid is stamped by the kernel at connect time, so a client cannot claim
// another's. It goes stale only if the host dies and its pid is reused, which
// the sidecar's own exit-on-stdin-EOF bounds.
func Authenticated(ln net.Listener, p Policy) net.Listener {
	return &authListener{Listener: ln, policy: p, peerOf: peerOf}
}

type authListener struct {
	net.Listener
	policy Policy
	// Seam for tests, which cannot spawn a process with a chosen pid.
	peerOf func(net.Conn) (peer, error)
}

func (l *authListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if reason := l.reject(conn); reason != "" {
			slog.Warn("rejected IPC peer", "reason", reason)
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

// reject names why the peer is not allowed, or "" if it is.
func (l *authListener) reject(conn net.Conn) string {
	p, err := l.peerOf(conn)
	if err != nil {
		return "peer credentials unavailable: " + err.Error()
	}
	if p.uid != os.Getuid() {
		return "uid mismatch"
	}
	if l.policy.HostPID != 0 && p.pid != l.policy.HostPID {
		return "pid mismatch"
	}
	return ""
}

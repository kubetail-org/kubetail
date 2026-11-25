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
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
)

// DefaultSocketPath returns a `\\.\pipe\...` path the host dials verbatim. Only for a
// standalone run — the host passes its own via `--socket`.
func DefaultSocketPath() string {
	return fmt.Sprintf(`\\.\pipe\kubetail-sidecar-%d`, os.Getpid())
}

// Scheme labels the transport in main.go's `READY <scheme>:<path>` line; the host
// doesn't parse it.
const Scheme = "pipe"

// ownerOnlyDACL grants Generic All to the process owner (`OW`) alone; `D:P` protects the
// DACL from inherited ACEs, so this is the entire access policy. Like chmod 0600.
const ownerOnlyDACL = "D:P(A;;GA;;;OW)"

// closeGrace bounds Close's wait. A listener that is not wedged closes at once,
// so this time is only ever spent on the deadlock pipeListener describes.
const closeGrace = time.Second

// Listen binds a named pipe restricted to the current user.
func Listen(path string) (net.Listener, error) {
	ln, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: ownerOnlyDACL,
	})
	if err != nil {
		return nil, err
	}
	return &pipeListener{Listener: ln, closeGrace: closeGrace}, nil
}

// pipeListener bounds Close. Closing go-winio's listener aborts the connect a
// pending Accept is waiting on, and its listener routine stops only when that
// abort surfaces as net.ErrClosed; any other error leaves the routine parked at
// its top select, with nothing left to wake it and Close waiting on it forever.
// http.Server.Shutdown closes the listener with Serve's Accept in flight, so an
// unbounded Close hands that hang to shutdown.
type pipeListener struct {
	net.Listener
	// A parameter so a test needn't outwait the production grace.
	closeGrace time.Duration
}

func (l *pipeListener) Close() error {
	done := make(chan error, 1)
	go func() { done <- l.Listener.Close() }()
	select {
	case err := <-done:
		return err
	case <-time.After(l.closeGrace):
		// The pipe handle dies with the process, and nothing accepts on an
		// abandoned listener: Accept has already returned the abort's error.
		slog.Warn("named pipe listener did not close; abandoning it")
		return nil
	}
}

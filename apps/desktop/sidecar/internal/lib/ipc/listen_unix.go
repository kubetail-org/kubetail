//go:build !windows

// Package ipc binds the host↔sidecar IPC endpoint with user-only access.
// The transport is platform-native: an AF_UNIX socket on Unix, a named
// pipe on Windows. main.go consumes Listen, DefaultSocketPath, and Scheme.
package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// Scheme labels the transport in main.go's `READY <scheme>:<path>` line; the host
// doesn't parse it.
const Scheme = "unix"

// DefaultSocketPath returns a temp-dir AF_UNIX socket, pid-namespaced so concurrent
// sidecars don't collide.
func DefaultSocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("kubetail-sidecar-%d.sock", os.Getpid()))
}

// Listen binds the IPC endpoint with user-only access. Order is security-critical:
// net.Listen creates the socket at 0777 & ~umask, so another local user could connect in
// the bind→chmod window — tightening umask first lands it at 0600 atomically, and the
// Chmod only covers unusual umask states.
func Listen(path string) (net.Listener, error) {
	_ = os.Remove(path)
	prev := syscall.Umask(0o177)
	ln, err := net.Listen("unix", path)
	syscall.Umask(prev)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil { // coverage:ignore the socket was just bound at this path by this process, which owns it
		_ = ln.Close()
		return nil, fmt.Errorf("chmod: %w", err)
	}
	return ln, nil
}

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

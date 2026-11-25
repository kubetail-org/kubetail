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

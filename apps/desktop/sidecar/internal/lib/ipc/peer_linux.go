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

package ipc

import (
	"net"
)

// acceptOne runs Accept in the background and reports the connection it
// yields, or nothing if it never yields one.
//
// It must be called before a client dials: a Windows named pipe has no accept
// backlog — winio creates an instance only inside Accept — so a dial made
// first retries until it times out.
func acceptOne(ln net.Listener) <-chan net.Conn {
	accepted := make(chan net.Conn, 1)
	go func() {
		defer close(accepted)
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn
		}
	}()
	return accepted
}

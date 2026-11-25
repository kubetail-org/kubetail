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

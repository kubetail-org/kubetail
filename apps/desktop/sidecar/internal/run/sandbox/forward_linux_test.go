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

package sandbox

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The forwarder's threads, which Linux counts against the run's process
// limit, stay within what the limit holds for them while it relays many
// connections at once.
func TestTheForwarderStaysUnderItsTasks(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cmd := initCmd(forwarding(echoSocket(t), port, "cat")...)
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Wait() })

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var first net.Conn
	require.Eventually(t, func() bool { first, err = net.Dial("tcp", addr); return err == nil }, testutil.Timeout, 10*time.Millisecond)
	_ = first.Close()

	const relays = 256
	conns := make([]net.Conn, relays)
	var wg sync.WaitGroup
	for i := range conns {
		wg.Go(func() {
			c, err := net.Dial("tcp", addr)
			if !assert.NoError(t, err) {
				return
			}
			conns[i] = c
			_, err = c.Write([]byte("x"))
			assert.NoError(t, err)
			_, err = io.ReadFull(c, make([]byte, 1))
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	t.Cleanup(func() {
		for _, c := range conns {
			if c != nil {
				_ = c.Close()
			}
		}
	})

	threads := threadsOf(cmd.Process.Pid, strconv.Itoa(os.Getuid()))
	assert.Positive(t, threads)
	assert.LessOrEqual(t, threads, forwarderTasks)
}

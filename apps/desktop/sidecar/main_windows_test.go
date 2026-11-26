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

package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/ipc"
)

// sockets numbers each pipe name, so a -count iteration never asks for a pipe
// an earlier one's listener may still hold.
var sockets atomic.Int64

// socketPath returns a bindable pipe name for one test, pid-namespaced so a
// concurrent run of the package doesn't collide.
func socketPath(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\kubetail-test-%d-%d-%s`, os.Getpid(), sockets.Add(1), t.Name())
}

// unbindableEndpoint names a pipe this test already holds: the first instance
// of a name is created exclusively, so a second bind collides.
func unbindableEndpoint(t *testing.T) string {
	t.Helper()
	path := socketPath(t)
	ln, err := ipc.Listen(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return path
}

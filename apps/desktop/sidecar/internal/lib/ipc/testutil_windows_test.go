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
	"net"
	"os"
	"sync/atomic"
	"testing"

	"github.com/Microsoft/go-winio"
)

// endpoints numbers each pipe name. A listener abandoned past closeGrace keeps
// its pipe alive, so a name reused by the next -count iteration is refused.
var endpoints atomic.Int64

// testEndpoint returns a fresh pipe name for one test.
func testEndpoint(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\kubetail-test-%d-%d-%s`, os.Getpid(), endpoints.Add(1), t.Name())
}

func dialEndpoint(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}

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

//go:build !windows

package ipc

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bound socket must land at 0600 regardless of the caller's umask —
// otherwise another local user on a shared box could connect during the
// window between bind and chmod. See listen_unix.go for the umask dance
// this guards.
func TestListen_IsOwnerOnly(t *testing.T) {
	for _, umask := range []int{0o022, 0o000} {
		t.Run("", func(t *testing.T) {
			prev := syscall.Umask(umask)
			defer syscall.Umask(prev)

			path := testEndpoint(t)
			ln, err := Listen(path)
			require.NoError(t, err)
			defer ln.Close()

			fi, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "umask was %#o", umask)
		})
	}
}

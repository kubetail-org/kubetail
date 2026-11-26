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

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// launchStrings is pid's arguments and environment as ps reads them, through
// kern.procargs2: the strings the kernel kept from the exec. Where macOS 27
// withholds the environment, the read proves the arguments alone.
func launchStrings(t *testing.T, pid int) string {
	t.Helper()
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	require.NoError(t, err)
	return strings.ReplaceAll(string(raw), "\x00", "\n")
}

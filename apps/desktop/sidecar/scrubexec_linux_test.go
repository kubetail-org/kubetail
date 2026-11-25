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
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// launchStrings is pid's arguments and environment as the kernel kept them from
// the exec, read from /proc, which os.Unsetenv never reaches.
func launchStrings(t *testing.T, pid int) string {
	t.Helper()
	var out []string
	for _, name := range []string{"cmdline", "environ"} {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/" + name)
		require.NoError(t, err)
		out = append(out, strings.ReplaceAll(string(raw), "\x00", "\n"))
	}
	return strings.Join(out, "\n")
}

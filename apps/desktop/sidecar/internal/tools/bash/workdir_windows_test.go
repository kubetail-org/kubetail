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

package bash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A workdir comes out in the native form CreateProcess takes, against the
// workspace or the home by mode. Git Bash's form of a drive path is converted; a
// path only Git Bash can place is refused, and so are the two shapes
// filepath.IsAbs calls relative but no directory can hold. Windows has no
// sandbox yet, but the sandboxed column is ready for one.
func TestWhereACallStartsOnWindows(t *testing.T) {
	const home, ws = `C:\Users\ana`, `C:\Users\ana\AppData\Roaming\kubetail\chats\c1\workspace`
	for _, c := range []struct{ workdir, sandboxed, outside string }{
		{"", ws, ws},
		{"~", ws, home},
		{"~/src", ws + `\src`, `C:\Users\ana\src`},
		{`~\src`, ws + `\src`, `C:\Users\ana\src`},
		{"src/app", ws + `\src\app`, ws + `\src\app`},
		{ws, ws, ws},
		{"/c/Users/ana/AppData/Roaming/kubetail/chats/c1/workspace/x", ws + `\x`, ws + `\x`},
		{`..\bob`, "", `C:\Users\ana\AppData\Roaming\kubetail\chats\c1\bob`},
		{`~\..`, "", `C:\Users`},
		{`C:\x`, "", `C:\x`},
		{"C:/x", "", `C:\x`},
		{`D:\x\..\y`, "", `D:\y`},
		{"/c/x", "", `C:\x`},
		{"/C/x", "", `C:\x`},
		{"/c", "", `C:\`},
		{`\\server\share`, "", `\\server\share`},
		{"//server/share/x", "", `\\server\share\x`},
	} {
		got, err := resolveWorkdir(home, ws, c.workdir, false)
		require.NoError(t, err, c.workdir)
		assert.Equal(t, c.outside, got, "outside: %q", c.workdir)

		got, err = resolveWorkdir(home, ws, c.workdir, true)
		if c.sandboxed == "" {
			assert.ErrorIs(t, err, errOutsideWorkspace, "sandboxed: %q", c.workdir)
			continue
		}
		require.NoError(t, err, c.workdir)
		assert.Equal(t, c.sandboxed, got, "sandboxed: %q", c.workdir)
	}

	for _, workdir := range []string{"/tmp", "/usr/bin", "/cx", `\x`, "C:x", "~ana", `~ana\x`} {
		for _, sandboxed := range []bool{false, true} {
			_, err := resolveWorkdir(home, ws, workdir, sandboxed)
			assert.ErrorIs(t, err, errInput, workdir)
		}
	}
}

// checkWorkdir refuses what no home can make valid, and nothing else.
func TestCheckWorkdir(t *testing.T) {
	for _, workdir := range []string{"", "~", "~/src", `~\src`, "src", `C:\x`, "/c/x", `\\server\share`} {
		assert.NoError(t, checkWorkdir(workdir), workdir)
	}
	for _, workdir := range []string{"/tmp", "/usr/bin", "/cx", `\x`, "C:x", "~ana", `~ana\x`} {
		assert.ErrorIs(t, checkWorkdir(workdir), errInput, workdir)
	}
}

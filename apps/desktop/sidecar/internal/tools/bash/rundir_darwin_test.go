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

package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A run's TMPDIR starts with xcrun's lookup cache, so a developer tool the
// user has run needs no xcodebuild under the sandbox.
func TestARunsTMPDIRStartsWithXcrunsCache(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	runs, tmpDir := runsIn(t), t.TempDir()
	host := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(host, "xcrun_db"), []byte("cache"), 0o600))
	t.Setenv("TMPDIR", host)

	d, err := newRunDir(runs, tmpDir, 1234)
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(d.tmp, "xcrun_db"))
	require.NoError(t, err)
	assert.Equal(t, "cache", string(b))
}

// The sidecar resolves the runtime folder, so on macOS a run's socket sits
// under /private/var, eight bytes longer than $TMPDIR's /var. Under a 48-byte
// $TMPDIR and a three-digit uid every runtime folder the host names fits but a
// dev build's fresh folder beside a refused one, which did not fit under /var
// either.
func TestTheSocketPathFitsUnderAResolvedTMPDIR(t *testing.T) {
	t.Parallel()

	tmpdir := "/private/var/folders/" + strings.Repeat("x", 2) + "/" + strings.Repeat("x", 30) + "/T"
	require.Len(t, tmpdir, len("/private")+48)
	for _, name := range []string{"kubetail-501", "kubetail-dev-501", "kubetail-501-XXXXXX"} {
		assert.NoError(t, checkSocketPath(filepath.Join(tmpdir, name, "runs")), name)
	}
	assert.Error(t, checkSocketPath(filepath.Join(tmpdir, "kubetail-dev-501-XXXXXX", "runs")))
}

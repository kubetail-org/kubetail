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

package rootdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A junction and a directory symlink are each a link, which Open refuses
// rather than follows.
func TestOpenRefusesAJunctionAndADirectorySymlink(t *testing.T) {
	for _, kind := range []string{"/J", "/D"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			out, err := exec.Command("cmd", "/c", "mklink", kind, filepath.Join(dir, "link"), t.TempDir()).CombinedOutput()
			require.NoError(t, err, string(out))
			parent, err := os.OpenRoot(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = parent.Close() })

			_, err = Open(parent, "link", true)

			assert.ErrorIs(t, err, ErrNotADirectory)
		})
	}
}

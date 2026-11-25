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

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
)

// A layout folder junctioned out of the profile fails the start; one
// junctioned inside it starts, named by its target.
func TestAJunctionOutOfTheProfileFailsTheStart(t *testing.T) {
	for _, c := range []struct {
		name    string
		target  func(root string) string
		refused bool
	}{
		{"outside", func(string) string { return scratch(t) }, true},
		{"inside", func(root string) string { return filepath.Join(root, "moved") }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratch(t)
			n := namedOn(layout.Windows, root, sharedWindowsRoots(root))
			target := c.target(root)
			require.NoError(t, os.MkdirAll(target, 0o700))
			require.NoError(t, os.MkdirAll(filepath.Dir(n.Layout.App), 0o700))
			out, err := exec.Command("cmd", "/c", "mklink", "/J", n.Layout.App, target).CombinedOutput()
			require.NoError(t, err, string(out))

			f, err := ResolveFolders(n)

			if c.refused {
				require.ErrorContains(t, err, "outside your profile")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(target, "data"), f.Data)
		})
	}
}

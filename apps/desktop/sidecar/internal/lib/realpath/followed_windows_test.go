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

package realpath

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A link whose target is rooted on the current drive, \foo, leads to that
// folder on the link's own volume.
func TestALinkToADriveRootedPathIsFollowedOnItsVolume(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	target := mkdirs(t, base, "target")[0]
	name := filepath.Join(base, "link")
	driveRooted := target[len(filepath.VolumeName(target)):]
	out, err := exec.Command("cmd", "/c", "mklink", "/D", name, driveRooted).CombinedOutput()
	require.NoError(t, err, string(out))

	got, _ := followed(t, name)

	assert.Equal(t, target, got)
}

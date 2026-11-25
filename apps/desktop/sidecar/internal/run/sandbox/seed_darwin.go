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

package sandbox

import (
	"os"
	"path/filepath"
)

// xcrunCache is the name of xcrun's lookup cache in its TMPDIR.
const xcrunCache = "xcrun_db"

// SeedTmpDir copies xcrun's lookup cache from this process's TMPDIR into a
// run's, dir. /usr/bin's developer tools (git, python3, make) are xcrun shims,
// and a miss with Xcode.app selected runs xcodebuild, which takes seconds under
// the sandbox. The copy is the run's own, so nothing a run writes to it reaches
// another run. A cache that cannot be copied leaves the run to fill its own.
func SeedTmpDir(dir string) {
	b, err := os.ReadFile(filepath.Join(os.TempDir(), xcrunCache))
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, xcrunCache), b, 0o600)
}

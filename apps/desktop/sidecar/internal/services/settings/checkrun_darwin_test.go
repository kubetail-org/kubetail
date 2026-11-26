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

package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// otherCase is dir with its last name in capitals, skipping the test on a
// volume that tells the two apart.
func otherCase(t *testing.T, dir string) string {
	t.Helper()
	upper := filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)))
	if _, err := os.Stat(upper); err != nil {
		t.Skip("the filesystem is case-sensitive")
	}
	return upper
}

// On a volume that folds case, a typed folder in another case opens, but is
// left out naming the disk's spelling, which the sandbox's profile matches.
func TestATypedFolderInAnotherCaseIsLeftOut(t *testing.T) {
	z := newZones(t)
	dir := z.dir(t, "home/tools")

	got := checked(z, typedFolder(otherCase(t, dir)))
	assert.False(t, got[0].Searched)
	assert.Contains(t, got[0].Reason, "is spelled "+dir)
}

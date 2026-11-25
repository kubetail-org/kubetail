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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// linkInAnotherCase makes base/Disk/kube and a link, base/link, whose text
// spells it base/DISK/KUBE. It answers the folder and the link. On a
// case-insensitive volume the link leads to the folder; on another it skips
// the test.
func linkInAnotherCase(t *testing.T) (folder, link string) {
	t.Helper()
	base := canonicalOf(t, t.TempDir())[0]
	folder = mkdirs(t, base, "Disk/kube")[0]
	upper := filepath.Join(base, "DISK", "KUBE")
	skipIfCaseSensitive(t, upper)
	link = filepath.Join(base, "link")
	require.NoError(t, os.Symlink(upper, link))
	return folder, link
}

// skipIfCaseSensitive skips the test where other, another spelling of a file
// the test made, names nothing.
func skipIfCaseSensitive(t *testing.T, other string) {
	t.Helper()
	if _, err := os.Stat(other); errors.Is(err, fs.ErrNotExist) {
		t.Skip("the filesystem is case-sensitive")
	}
}

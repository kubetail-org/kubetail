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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// In a case-sensitive directory State and state are two folders, and logs
// under the second are not inside the first. CI's runner may not make such a
// directory, so state stands in for a second folder through stat.
func TestRelationTellsApartSiblingsThatDifferInCase(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dirs := mkdirs(t, base, "State", "other/logs")
	upper, lower, other := dirs[0], filepath.Join(base, "state"), filepath.Dir(dirs[1])
	stat := func(p string) (fs.FileInfo, error) {
		if p == lower || strings.HasPrefix(p, lower+string(filepath.Separator)) {
			return os.Stat(other + p[len(lower):])
		}
		return os.Stat(p)
	}

	assert.Equal(t, Apart, relation(filepath.Join(lower, "logs"), upper, stat))
	assert.Equal(t, Apart, relation(lower, upper, stat))
	assert.Equal(t, Inside, relation(filepath.Join(upper, "logs"), upper, stat))
}

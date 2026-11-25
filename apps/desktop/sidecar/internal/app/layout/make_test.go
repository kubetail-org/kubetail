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

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// macOSUnder is macOS's layout over roots in a scratch folder, which has an
// app folder above the data.
func macOSUnder(t *testing.T) Layout {
	root := t.TempDir()
	support := filepath.Join(root, "support")
	return Of(MacOS, true, Roots{Config: support, LocalData: support, Cache: filepath.Join(root, "caches"), Home: root})
}

func TestMakeOwnerOnlyMakesTheFolderAndItsParents(t *testing.T) {
	l := macOSUnder(t)

	require.NoError(t, MakeOwnerOnly(l, l.Data))

	assert.DirExists(t, l.Data)
}

// A kind outside the app folder leaves it alone.
func TestMakeOwnerOnlyOutsideTheAppFolderLeavesItAlone(t *testing.T) {
	l := macOSUnder(t)

	require.NoError(t, MakeOwnerOnly(l, l.Cache))

	assert.DirExists(t, l.Cache)
	assert.NoDirExists(t, l.App)
}

func TestMakeOwnerOnlyFailsOnAFileInTheWay(t *testing.T) {
	l := macOSUnder(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(l.Cache), 0o700))
	require.NoError(t, os.WriteFile(l.Cache, nil, 0o600))

	assert.Error(t, MakeOwnerOnly(l, l.Cache))
}

// An app folder that cannot be made fails the kind inside it.
func TestMakeOwnerOnlyFailsOnAFileWhereTheAppFolderGoes(t *testing.T) {
	l := macOSUnder(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(l.App), 0o700))
	require.NoError(t, os.WriteFile(l.App, nil, 0o600))

	assert.Error(t, MakeOwnerOnly(l, l.Data))
}

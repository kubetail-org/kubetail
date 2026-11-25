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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

// The profile folders come from the Known Folder API, whatever the
// environment says; every other variable from the environment.
func TestKnownFoldersReadsTheProfileFoldersFromTheAPI(t *testing.T) {
	stale := map[string]string{"LOCALAPPDATA": `C:\stale`, "APPDATA": `C:\stale`, "USERPROFILE": `C:\stale`, "TEMP": `C:\t`}
	getenv := KnownFolders(func(name string) string { return stale[name] })

	for name, id := range map[string]*windows.KNOWNFOLDERID{
		"LOCALAPPDATA": windows.FOLDERID_LocalAppData,
		"APPDATA":      windows.FOLDERID_RoamingAppData,
		"USERPROFILE":  windows.FOLDERID_Profile,
	} {
		want, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
		assert.NoError(t, err, name)
		assert.Equal(t, want, getenv(name), name)
	}
	assert.Equal(t, `C:\t`, getenv("TEMP"))
}

// A folder the API cannot name reads as unset, so the roots refuse it rather
// than take the environment's value.
func TestAKnownFolderTheAPICannotNameIsUnset(t *testing.T) {
	failing := func(*windows.KNOWNFOLDERID, uint32) (string, error) { return "", errors.New("no folder") }
	getenv := knownFolders(func(string) string { return `C:\stale` }, failing)

	assert.Empty(t, getenv("LOCALAPPDATA"))
}

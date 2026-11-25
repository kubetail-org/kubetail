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

import "golang.org/x/sys/windows"

// KnownFolders is getenv with LOCALAPPDATA, APPDATA and USERPROFILE read
// from the Known Folder API, as the host's path resolver reads them: a
// launcher's environment can hold stale values after a folder is redirected,
// or none. A folder the API cannot name reads as unset.
func KnownFolders(getenv func(string) string) func(string) string {
	return knownFolders(getenv, windows.KnownFolderPath)
}

// knownFolders is KnownFolders with the API injected, so a test can make it
// fail.
func knownFolders(getenv func(string) string, path func(*windows.KNOWNFOLDERID, uint32) (string, error)) func(string) string {
	ids := map[string]*windows.KNOWNFOLDERID{
		"LOCALAPPDATA": windows.FOLDERID_LocalAppData,
		"APPDATA":      windows.FOLDERID_RoamingAppData,
		"USERPROFILE":  windows.FOLDERID_Profile,
	}
	return func(name string) string {
		id, ok := ids[name]
		if !ok {
			return getenv(name)
		}
		dir, err := path(id, windows.KF_FLAG_DEFAULT)
		if err != nil {
			return ""
		}
		return dir
	}
}

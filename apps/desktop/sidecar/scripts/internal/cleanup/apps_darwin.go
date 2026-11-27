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

package cleanup

// FindApps is each installed bundle. A bundle is a folder of its own, so
// removing it is the uninstall.
func FindApps(home string) ([]App, []string, error) {
	paths, err := Existing(macOSApps(home))
	if err != nil {
		return nil, nil, err
	}
	var apps []App
	for _, path := range paths {
		apps = append(apps, App{Name: path, remove: func() error { return Remove(path) }})
	}
	return apps, nil, nil
}

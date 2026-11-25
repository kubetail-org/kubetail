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

//go:build windows

package google

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// gcloud's configuration lives under the roaming application data.
func TestFindProjectReadsTheAppDataGcloud(t *testing.T) {
	appData := t.TempDir()
	writeGcloudIn(t, filepath.Join(appData, "gcloud"), "", map[string]string{"default": "[core]\nproject = home-project\n"})

	project, _ := FindProject(env(map[string]string{"APPDATA": appData}))

	assert.Equal(t, "home-project", project)
}

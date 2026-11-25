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

package google

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// env is a getenv over a map.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// writeGcloud writes a gcloud configuration directory in a temporary one.
func writeGcloud(t *testing.T, active string, configs map[string]string) string {
	t.Helper()
	return writeGcloudIn(t, t.TempDir(), active, configs)
}

// writeGcloudIn writes a gcloud configuration directory at dir: the active
// configuration's name, when not empty, and each configuration's file.
func writeGcloudIn(t *testing.T, dir, active string, configs map[string]string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configurations"), 0o700))
	if active != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "active_config"), []byte(active+"\n"), 0o600))
	}
	for name, body := range configs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "configurations", "config_"+name), []byte(body), 0o600))
	}
	return dir
}

// A global location is Vertex AI's global host; any other is its regional one.
func TestBaseURLIsTheProjectsOpenAIEndpoint(t *testing.T) {
	assert.Equal(t, testBase, BaseURL("my-project", "global"))
	assert.Equal(t,
		"https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/endpoints/openapi",
		BaseURL("my-project", "us-central1"))
	assert.Equal(t,
		"https://aiplatform.googleapis.com/v1/projects/example.com:my-project/locations/global/endpoints/openapi",
		BaseURL("example.com:my-project", "global"))
	assert.Equal(t,
		"https://aiplatform.googleapis.com/v1/projects/123456789012/locations/global/endpoints/openapi",
		BaseURL("123456789012", "global"), "a project number")
}

// A project or location that is not one names no endpoint, so the environment
// cannot move the host or the path.
func TestBaseURLRefusesWhatIsNotAProjectOrLocation(t *testing.T) {
	for _, tc := range []struct{ project, location string }{
		{"", "global"},
		{"my-project", ""},
		{"My-Project", "global"},
		{"my-project/../x", "global"},
		{"short", "global"},
		{"example.com:123456789012", "global"},
		{"123456789012345678901", "global"},
		{"my-project", "evil.com/x"},
		{"my-project", "us-central1.evil.com"},
		{"my-project?x=1", "global"},
	} {
		assert.Empty(t, BaseURL(tc.project, tc.location), "%q in %q", tc.project, tc.location)
	}
}

// The environment's project wins, then gcloud's override variable, then the
// active gcloud configuration's; the location is the environment's, else global.
func TestFindProject(t *testing.T) {
	gcloud := writeGcloud(t, "work", map[string]string{
		"default": "[core]\nproject = default-project\n",
		"work":    "[core]\naccount = ada@example.com\nproject = work-project\n\n[compute]\nregion = europe-west1\n",
	})

	for name, tc := range map[string]struct {
		vars              map[string]string
		project, location string
	}{
		"the environment's": {map[string]string{"GOOGLE_CLOUD_PROJECT": "env-project", "CLOUDSDK_CORE_PROJECT": "sdk-project", "CLOUDSDK_CONFIG": gcloud, "GOOGLE_CLOUD_LOCATION": "us-east5"}, "env-project", "us-east5"},
		"gcloud's variable": {map[string]string{"CLOUDSDK_CORE_PROJECT": "sdk-project", "CLOUDSDK_CONFIG": gcloud}, "sdk-project", "global"},
		"the active config": {map[string]string{"CLOUDSDK_CONFIG": gcloud}, "work-project", "global"},
		"a named config":    {map[string]string{"CLOUDSDK_CONFIG": gcloud, "CLOUDSDK_ACTIVE_CONFIG_NAME": "default"}, "default-project", "global"},
		"none":              {map[string]string{}, "", "global"},
	} {
		t.Run(name, func(t *testing.T) {
			project, location := FindProject(env(tc.vars))
			assert.Equal(t, tc.project, project)
			assert.Equal(t, tc.location, location)
		})
	}
}

// With no active_config gcloud uses its default configuration; a file that
// names no project, or none at all, finds none.
func TestFindProjectInGcloudsDefaults(t *testing.T) {
	withDefault := writeGcloud(t, "", map[string]string{"default": "[core]\nproject=default-project\n"})
	project, _ := FindProject(env(map[string]string{"CLOUDSDK_CONFIG": withDefault}))
	assert.Equal(t, "default-project", project)

	elsewhere := writeGcloud(t, "", map[string]string{"default": "[compute]\nproject = not-core\n"})
	project, _ = FindProject(env(map[string]string{"CLOUDSDK_CONFIG": elsewhere}))
	assert.Empty(t, project)

	project, _ = FindProject(env(map[string]string{"CLOUDSDK_CONFIG": t.TempDir()}))
	assert.Empty(t, project)
}

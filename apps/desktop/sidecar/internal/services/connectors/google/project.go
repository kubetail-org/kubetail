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
	"bufio"
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// projectID is a Google Cloud project id, a domain-scoped one, or a project
	// number, which Vertex AI's paths take as well.
	projectID = regexp.MustCompile(`^(([a-z0-9.-]+:)?[a-z][a-z0-9-]{4,28}[a-z0-9]|[0-9]{1,20})$`)
	// locationID is a Vertex AI location: global or a region.
	locationID = regexp.MustCompile(`^[a-z]+(-[a-z0-9]+)*$`)
)

// BaseURL is Vertex AI's OpenAI-compatible endpoint for project in location,
// empty when either is not a well-formed id: they come from the environment,
// and must not move the host or the path.
func BaseURL(project, location string) string {
	if !projectID.MatchString(project) || !locationID.MatchString(location) {
		return ""
	}
	host := "aiplatform.googleapis.com"
	if location != "global" {
		host = location + "-" + host
	}
	return "https://" + host + "/v1/projects/" + project + "/locations/" + location + "/endpoints/openapi"
}

// FindProject is the Google Cloud project and location the models run in, as
// Google's own tools find them: GOOGLE_CLOUD_PROJECT, then gcloud's
// CLOUDSDK_CORE_PROJECT, then the active gcloud configuration's project; and
// GOOGLE_CLOUD_LOCATION, else global. The project is empty when none is set.
func FindProject(getenv func(string) string) (project, location string) {
	project = cmp.Or(getenv("GOOGLE_CLOUD_PROJECT"), getenv("CLOUDSDK_CORE_PROJECT"))
	if project == "" {
		project = gcloudProject(getenv)
	}
	return project, cmp.Or(getenv("GOOGLE_CLOUD_LOCATION"), "global")
}

// gcloudProject is the project the active gcloud configuration names, empty
// when there is none or it names no project.
func gcloudProject(getenv func(string) string) string {
	dir := cmp.Or(getenv("CLOUDSDK_CONFIG"), defaultGcloudDir(getenv))
	if dir == "" {
		return ""
	}
	name := getenv("CLOUDSDK_ACTIVE_CONFIG_NAME")
	if name == "" {
		active, _ := os.ReadFile(filepath.Join(dir, "active_config"))
		name = cmp.Or(strings.TrimSpace(string(active)), "default")
	}
	ini, err := os.ReadFile(filepath.Join(dir, "configurations", "config_"+name))
	if err != nil {
		return ""
	}
	return iniValue(ini, "core", "project")
}

// iniValue is key's value in section of an INI file, empty when absent.
func iniValue(ini []byte, section, key string) string {
	var current string
	lines := bufio.NewScanner(bytes.NewReader(ini))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && current == section && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

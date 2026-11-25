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

package bash

import (
	"os"
	"path"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// toolHomeVars is each folder of the chat's tool home and the variable that
// points a tool at it, since HOME is the workspace and a tool would otherwise
// write its cache there.
var toolHomeVars = []struct{ name, dir string }{
	{"XDG_CACHE_HOME", "xdg/cache"}, {"XDG_CONFIG_HOME", "xdg/config"}, {"XDG_DATA_HOME", "xdg/data"},
	{"HELM_CACHE_HOME", "helm/cache"}, {"HELM_CONFIG_HOME", "helm/config"}, {"HELM_DATA_HOME", "helm/data"},
	{"NPM_CONFIG_CACHE", "npm"},
	{"PIP_CACHE_DIR", "pip"},
	{"GOCACHE", "go/build"}, {"GOMODCACHE", "go/mod"},
	{"CARGO_HOME", "cargo"},
	{"CLOUDSDK_CONFIG", "gcloud"},
}

// toolHomeEnv is the variables that point each tool into home.
func toolHomeEnv(home string) []string {
	env := make([]string, len(toolHomeVars))
	for i, v := range toolHomeVars {
		env[i] = v.name + "=" + path.Join(home, v.dir)
	}
	return env
}

// makeToolHome makes the chat's tool home and its folders through the chat's
// root, one level at a time, each opened through the last, so a link a
// command swaps in at any level is refused.
func makeToolHome(dir tools.ChatDir) error {
	home, err := tools.OpenToolHome(dir, true)
	if err != nil {
		return err
	}
	defer home.Close()
	for _, v := range toolHomeVars {
		if err := makeIn(home, v.dir); err != nil {
			return err
		}
	}
	return nil
}

// makeIn makes each level of rel, a slash-separated path, under root.
func makeIn(root *os.Root, rel string) error {
	parent := root
	for name := range strings.SplitSeq(rel, "/") {
		next, err := rootdir.Open(parent, name, true)
		if parent != root {
			parent.Close()
		}
		if err != nil {
			return err
		}
		parent = next
	}
	return parent.Close()
}

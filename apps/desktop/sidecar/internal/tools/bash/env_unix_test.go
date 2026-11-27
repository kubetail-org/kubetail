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

//go:build !windows

package bash

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// A sandboxed run's environment is the table and nothing else: with the
// sidecar's holding every name NeverEnv matches and every LC_*, none of them
// passes, and nothing in it is a name no run may hold. The user's variables
// follow LANG and TZ, and every name but those is one the user cannot set.
func TestTheSandboxedEnvironmentIsFixed(t *testing.T) {
	t.Parallel()

	rd := &runDir{path: "/run/kubetail/runs/10-1", tmp: "/cache/tmp/10-2"}
	cluster := &target{context: "prod", cacheDir: "/cache/kubectl/7/abc"}
	environ := append(slices.Clone(processEnv), "TZ=Europe/Paris", "LC_MESSAGES=C", "LC_NUMERIC=de_DE.UTF-8")
	for _, r := range sandbox.NeverEnv() {
		environ = append(environ, r.Name+r.Prefix+"=leaked")
	}
	toolchain := []string{"NVM_DIR=/home/ana/.nvm", "ASDF_NODEJS_VERSION=20.1.0"}

	user := []string{"HELM_NAMESPACE=apps", "EDITOR=vi"}

	got := sandboxedRunEnv(environ, kubetailVars, "/opt/bin:/usr/bin", "/data/ws", "/data/ws/sub", rd, cluster, "/data/th", user, toolchain, nil)

	assert.Equal(t, []string{
		"PATH=/opt/bin:/usr/bin",
		"HOME=/data/ws",
		"PWD=/data/ws/sub",
		"TMPDIR=/cache/tmp/10-2",
		"ZDOTDIR=/run/kubetail/runs/10-1",
		"KUBECONFIG=/run/kubetail/runs/10-1/kubeconfig",
		"KUBECACHEDIR=/cache/kubectl/7/abc",
		"LANG=en_US.UTF-8",
		"TZ=Europe/Paris",
		"HELM_NAMESPACE=apps",
		"EDITOR=vi",
		"TERM=dumb",
		"XDG_CACHE_HOME=/data/th/xdg/cache",
		"XDG_CONFIG_HOME=/data/th/xdg/config",
		"XDG_DATA_HOME=/data/th/xdg/data",
		"HELM_CACHE_HOME=/data/th/helm/cache",
		"HELM_CONFIG_HOME=/data/th/helm/config",
		"HELM_DATA_HOME=/data/th/helm/data",
		"NPM_CONFIG_CACHE=/data/th/npm",
		"PIP_CACHE_DIR=/data/th/pip",
		"GOCACHE=/data/th/go/build",
		"GOMODCACHE=/data/th/go/mod",
		"CARGO_HOME=/data/th/cargo",
		"CLOUDSDK_CONFIG=/data/th/gcloud",
		"NVM_DIR=/home/ana/.nvm",
		"ASDF_NODEJS_VERSION=20.1.0",
		"KUBETAIL=1",
		"KUBETAIL_SIDECAR_PID=10",
		"KUBETAIL_DESKTOP_PID=9",
	}, got)
	for _, kv := range got {
		name, _, _ := strings.Cut(kv, "=")
		assert.False(t, sandbox.Unpassable(name), name)
		if !slices.Contains([]string{"LANG", "TZ", "HELM_NAMESPACE", "EDITOR"}, name) {
			assert.True(t, sandbox.KubetailSets(name), "%s is the table's, so the user cannot set it", name)
		}
	}
}

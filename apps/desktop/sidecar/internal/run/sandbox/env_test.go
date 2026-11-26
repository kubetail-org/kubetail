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

package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A rule matches its name exactly, or every name its prefix starts but those
// it excepts.
func TestUnpassableMatchesNamesAndPrefixes(t *testing.T) {
	rules := []EnvRule{{Name: "SECRET"}, {Prefix: "CLOUD_", Except: []string{"CLOUD_DIR"}}}
	for name, want := range map[string]bool{
		"SECRET": true, "SECRETS": false, "MY_SECRET": false,
		"CLOUD_TOKEN": true, "CLOUD_": true, "CLOUD": false,
		"CLOUD_DIR": false, "CLOUD_DIRS": true,
		"PATH": false,
	} {
		assert.Equal(t, want, unpassable(rules, name), name)
	}
}

// The rules name what lets a run reach past the sandbox or change what a
// program loads, every AWS variable, and every Azure variable but
// AZURE_CONFIG_DIR.
func TestNeverEnvHoldsTheRules(t *testing.T) {
	for _, name := range []string{
		"SSH_AUTH_SOCK", "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_ENTERPRISE_TOKEN", "DOCKER_HOST", "DYLD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES",
		"DYLD_FRAMEWORK_PATH", "LD_LIBRARY_PATH", "LD_PRELOAD", "LD_AUDIT", "BASH_ENV", "ENV", "PROMPT_COMMAND",
		"AWS_SESSION_TOKEN", "AWS_PROFILE",
		"AZURE_CLIENT_SECRET", "AZURE_CLIENT_CERTIFICATE_PASSWORD", "AZURE_STORAGE_CONNECTION_STRING",
		"AZURE_DEVOPS_EXT_PAT", "ARM_CLIENT_SECRET", "ARM_ACCESS_KEY",
		"AAD_SERVICE_PRINCIPAL_CLIENT_SECRET", "MSI_SECRET",
	} {
		assert.True(t, Unpassable(name), name)
	}
	assert.False(t, Unpassable("PATH"))
	assert.False(t, Unpassable("AZURE_CONFIG_DIR"), "the one Azure name a run may hold")
}

// NeverEnv hands out a copy, so no caller changes the rules.
func TestNeverEnvIsACopy(t *testing.T) {
	rules := NeverEnv()
	rules[0].Name = "PATH"

	assert.False(t, Unpassable("PATH"))
}

// What the run's table sets is refused by name, and the prefixes take every
// variable of their tool's config.
func TestKubetailSetsNamesTheTablesVariables(t *testing.T) {
	for _, name := range []string{
		"PATH", "HOME", "PWD", "TMPDIR", "ZDOTDIR", "KUBECONFIG", "KUBECACHEDIR", "TERM",
		"KUBETAIL", "KUBETAIL_SIDECAR_PID", "KUBETAIL_DESKTOP_PID", "ASDF_KUBECTL_VERSION", "CLOUDSDK_CORE_PROJECT",
		"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "AWS_ENDPOINT_URL", "npm_config_cache", "AZURE_CONFIG_DIR", "HTTPS_PROXY", "no_proxy",
	} {
		assert.True(t, KubetailSets(name), name)
	}
	for _, name := range []string{"LANG", "TZ", "HELM_NAMESPACE", "KUBECTL_EXTERNAL_DIFF", "EDITOR", "KUBETAIL_NAMESPACE"} {
		assert.False(t, KubetailSets(name), name)
	}
}

// Every variable a toolchain location sets is one the table sets.
func TestKubetailSetsEveryToolchainVariable(t *testing.T) {
	for _, l := range sharedLists.Toolchain {
		for name := range l.Env {
			assert.True(t, KubetailSets(name), "%s's %s", l.Name, name)
		}
	}
}

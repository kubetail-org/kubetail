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
	"slices"
	"strings"
)

// EnvRule matches an environment variable no sandboxed run may hold: by its
// name, or every name starting with Prefix but those Except lists.
type EnvRule struct {
	Name   string
	Prefix string
	Except []string
}

// neverEnv is what no sandboxed run may hold: what reaches past the sandbox,
// what changes what a program loads, what a shell runs on its own, and every
// AWS and Azure variable, Terraform's and kubelogin's Azure credentials among
// them. Azure's credential variables keep growing, so they go by prefix;
// AZURE_CONFIG_DIR, where az keeps its login, is the one a run may hold.
var neverEnv = []EnvRule{
	{Name: "SSH_AUTH_SOCK"}, {Name: "GITHUB_TOKEN"}, {Name: "GH_TOKEN"}, {Name: "DOCKER_HOST"},
	{Name: "GITHUB_ENTERPRISE_TOKEN"}, {Name: "GH_ENTERPRISE_TOKEN"},
	{Name: "DYLD_LIBRARY_PATH"}, {Name: "DYLD_INSERT_LIBRARIES"}, {Name: "DYLD_FRAMEWORK_PATH"},
	{Name: "LD_LIBRARY_PATH"}, {Name: "LD_PRELOAD"}, {Name: "LD_AUDIT"},
	{Name: "BASH_ENV"}, {Name: "ENV"}, {Name: "PROMPT_COMMAND"},
	{Prefix: "AWS_"},
	{Prefix: "AZURE_", Except: []string{"AZURE_CONFIG_DIR"}}, {Prefix: "ARM_"},
	{Name: "AAD_SERVICE_PRINCIPAL_CLIENT_SECRET"}, {Name: "MSI_SECRET"},
}

// NeverEnv is the rules, a copy each call.
func NeverEnv() []EnvRule { return slices.Clone(neverEnv) }

// Unpassable reports whether name matches a rule.
func Unpassable(name string) bool { return unpassable(neverEnv, name) }

func unpassable(rules []EnvRule, name string) bool {
	return slices.ContainsFunc(rules, func(r EnvRule) bool {
		if r.Prefix != "" {
			return strings.HasPrefix(name, r.Prefix) && !slices.Contains(r.Except, name)
		}
		return name == r.Name
	})
}

// kubetailEnv is what a sandboxed run's table sets itself, so no variable of
// the user's may name it: the run's own paths and terminal, the variables
// that point each tool into the tool home, each toolchain location's (Lists'
// Env, asdf's versions included), each cloud platform's mount's, and the
// proxy variables. AWS's, Google Cloud's and Git's config variables go by
// prefix, since each also redirects where its tool reads its login. npm reads
// its config variables in either case.
var kubetailEnv = []EnvRule{
	{Name: "PATH"}, {Name: "HOME"}, {Name: "PWD"}, {Name: "TMPDIR"}, {Name: "ZDOTDIR"},
	{Name: "KUBECONFIG"}, {Name: "KUBECACHEDIR"}, {Name: "TERM"},
	{Name: "KUBETAIL"}, {Name: "KUBETAIL_SIDECAR_PID"}, {Name: "KUBETAIL_DESKTOP_PID"},
	{Name: "XDG_CACHE_HOME"}, {Name: "XDG_CONFIG_HOME"}, {Name: "XDG_DATA_HOME"},
	{Name: "HELM_CACHE_HOME"}, {Name: "HELM_CONFIG_HOME"}, {Name: "HELM_DATA_HOME"}, {Name: "HELM_PLUGINS"},
	{Name: "NPM_CONFIG_CACHE"}, {Name: "npm_config_cache"}, {Name: "PIP_CACHE_DIR"}, {Name: "GOCACHE"}, {Name: "GOMODCACHE"}, {Name: "CARGO_HOME"},
	{Prefix: "ASDF_"}, {Name: "MISE_DATA_DIR"}, {Name: "MISE_CONFIG_DIR"}, {Name: "RUSTUP_HOME"},
	{Name: "NVM_DIR"}, {Name: "PYENV_ROOT"}, {Name: "RBENV_ROOT"}, {Name: "VOLTA_HOME"},
	{Name: "AQUA_ROOT_DIR"}, {Name: "AQUA_GLOBAL_CONFIG"}, {Name: "KREW_ROOT"},
	{Prefix: "AWS_"}, {Prefix: "CLOUDSDK_"},
	{Name: "GIT_CONFIG"}, {Prefix: "GIT_CONFIG_"}, {Name: "GIT_TERMINAL_PROMPT"},
	{Name: "GH_CONFIG_DIR"}, {Name: "GH_HOST"}, {Name: "GH_PROMPT_DISABLED"},
	{Name: "AZURE_CONFIG_DIR"}, {Name: "IDENTITY_ENDPOINT"}, {Name: "IDENTITY_HEADER"}, {Name: "REQUESTS_CA_BUNDLE"},
	{Name: "HTTP_PROXY"}, {Name: "HTTPS_PROXY"}, {Name: "ALL_PROXY"}, {Name: "NO_PROXY"},
	{Name: "http_proxy"}, {Name: "https_proxy"}, {Name: "all_proxy"}, {Name: "no_proxy"},
}

// KubetailSets reports whether a sandboxed run's table sets name itself.
func KubetailSets(name string) bool { return unpassable(kubetailEnv, name) }

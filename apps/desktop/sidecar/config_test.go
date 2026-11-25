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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/ipc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// What a bare command line yields: the production endpoints.
// productionConfig is what an empty command line gives over rootsEnv.
var productionConfig = func() config {
	l := layoutOver(rootsEnv, debugBuild)
	keychain := ""
	if debugBuild {
		keychain = "Kubetail-dev"
	}
	return config{
		Folders: namedOver(rootsEnv, l),
		App: app.Config{
			OAuthIssuerURL:  "https://oauth.kubetail.com",
			OAuthClientID:   "kubetail-desktop",
			AddFake:         debugBuild,
			KeychainService: keychain,
		},
		Socket: ipc.DefaultSocketPath(),
	}
}()

// namedOver is the folders configFromArgs names for l over e's roots.
func namedOver(e env, l layout.Layout) app.Named {
	roots, err := layout.RootsFromEnv(layout.Current(), e.get)
	if err != nil {
		panic(err)
	}
	return app.Named{Platform: layout.Current(), Layout: l, Data: l.Data, Cache: l.Cache, Profile: roots.Home}
}

func assertConfig(t *testing.T, want, got config, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, want, got, msgAndArgs...)
}

// env is the environment a test hands the config: what it holds and nothing
// else, so a key exported in the shell running the tests is never read.
type env map[string]string

// get reads name from e, else from rootsEnv, so every test's environment
// names the roots the sidecar places its folders on.
func (e env) get(name string) string {
	if v, ok := e[name]; ok {
		return v
	}
	return rootsEnv[name]
}

// layoutOver is the layout a sidecar of the dev or release profile places on
// e's roots.
func layoutOver(e env, dev bool) layout.Layout {
	roots, err := layout.RootsFromEnv(layout.Current(), e.get)
	if err != nil {
		panic(err)
	}
	return layout.Of(layout.Current(), dev, roots)
}

// noEnv is an environment with nothing in it.
var noEnv = env{}

func mustConfigFromArgs(t *testing.T, args []string, e env) config {
	t.Helper()
	cfg, err := configFromArgs(args, e.get)
	if err != nil {
		t.Fatalf("configFromArgs(%q): %v", args, err)
	}
	return cfg
}

func TestConfigFromArgsDefaultsToProduction(t *testing.T) {
	assertConfig(t, productionConfig, mustConfigFromArgs(t, nil, noEnv))
}

// Pins the tag boundary. `go test ./...` builds untagged, so the ordinary run
// asserts the environment never reaches the config; `go test -tags debug`
// asserts the override a `make build-sidecar-dev` build honours.
func TestConfigFromArgsIgnoresEnvironment(t *testing.T) {
	e := env{
		"KUBETAIL_OAUTH_ISSUER":         "https://issuer.override",
		"KUBETAIL_OAUTH_CLIENT_ID":      "override",
		"KUBETAIL_DATA_DIR":             "/tmp/override",
		"KUBETAIL_CACHE_DIR":            "/tmp/override-cache",
		"KUBETAIL_RUNTIME_DIR":          "/tmp/override-run",
		"KUBETAIL_KEYCHAIN_SERVICE":     "Kubetail-override",
		"KUBETAIL_FAULTS":               "get api=refused",
		"KUBETAIL_CHATGPT_ISSUER_URL":   "http://127.0.0.1:1",
		"KUBETAIL_CHATGPT_BASE_URL":     "http://127.0.0.1:2/v1",
		"KUBETAIL_OPENROUTER_AUTH_URL":  "http://127.0.0.1:3",
		"KUBETAIL_AWS_SIGNIN_URL":       "http://127.0.0.1:4",
		"KUBETAIL_GITHUB_AUTH_URL":      "http://127.0.0.1:5",
		"KUBETAIL_GITHUB_CLIENT_ID":     "Ov23liOverride",
		"KUBETAIL_GOOGLE_CLIENT_ID":     "google-override",
		"KUBETAIL_GOOGLE_CLIENT_SECRET": "google-secret-override",
	}

	want := productionConfig
	if debugBuild {
		want.App.Faults = "get api=refused"
		want.App.OAuthIssuerURL = "https://issuer.override"
		want.App.OAuthClientID = "override"
		want.Folders.Data = "/tmp/override"
		want.Folders.Cache = "/tmp/override-cache"
		want.Folders.Runtime = "/tmp/override-run"
		want.App.KeychainService = "Kubetail-override"
		want.App.GitHubClientID = "Ov23liOverride"
		want.App.GoogleClientID = "google-override"
		want.App.GoogleClientSecret = "google-secret-override"
		want.App.ConnectorAuthURLs = map[string]string{
			"chatgpt": "http://127.0.0.1:1", "openrouter": "http://127.0.0.1:3", "aws": "http://127.0.0.1:4", "github": "http://127.0.0.1:5",
		}
		want.App.LLMBaseURLs = map[string]string{"chatgpt": "http://127.0.0.1:2/v1"}
	}
	assertConfig(t, want, mustConfigFromArgs(t, nil, e))
}

func TestConfigFromArgsReadsFlags(t *testing.T) {
	folders := productionConfig.Folders
	folders.Runtime = "/tmp/run"
	want := config{
		Folders: folders,
		App: app.Config{
			KubeconfigPath:     "/tmp/kubeconfig",
			OAuthIssuerURL:     "https://issuer.example",
			OAuthClientID:      "other",
			GitHubClientID:     "Ov23liFlag",
			GoogleClientID:     "google-flag",
			GoogleClientSecret: "google-secret-flag",
			KeychainService:    "Kubetail-dev",
			AddFake:            debugBuild,
			HostPID:            42,
		},
		Socket:    "/tmp/sock",
		HostPID:   42,
		LogStderr: true,
	}
	cfg := mustConfigFromArgs(t, []string{
		"--socket", "/tmp/sock",
		"--host-pid", "42",
		"--kubeconfig", "/tmp/kubeconfig",
		"--runtime-dir", "/tmp/run",
		"--oauth-issuer", "https://issuer.example",
		"--oauth-client-id", "other",
		"--github-client-id", "Ov23liFlag",
		"--google-client-id", "google-flag",
		"--google-client-secret", "google-secret-flag",
		"--keychain-service", "Kubetail-dev",
		"--log-stderr",
	}, noEnv)
	assertConfig(t, want, cfg)
}

// A dev profile's sign-ins stay apart from the release's unless a name is
// passed.
func TestConfigFromArgsTakesTheProfilesKeychainService(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--dev=true"}, "Kubetail-dev"},
		{[]string{"--dev=false"}, ""},
		{[]string{"--dev=true", "--keychain-service", "Other"}, "Other"},
	} {
		assert.Equal(t, c.want, mustConfigFromArgs(t, c.args, noEnv).App.KeychainService, c.args)
	}
}

// The sidecar logs in the resolved layout's log folder, the one the host logs
// in, and copies its records to stderr only when asked.
func TestResolveFoldersLogsInTheLayoutsLogFolder(t *testing.T) {
	cfg := runArgs(t, "--runtime-dir", t.TempDir())
	assert.Equal(t, filepath.Join(cfg.App.Folders.Layout.Logs, "sidecar.log"), cfg.LogFile)
	assert.False(t, cfg.LogStderr)
}

// The sidecar places every folder itself, from the roots in its environment:
// the host names none but the runtime directory.
func TestConfigFromArgsPlacesTheFoldersFromTheEnvironment(t *testing.T) {
	cfg := mustConfigFromArgs(t, nil, noEnv)

	want := layoutOver(rootsEnv, debugBuild)
	assert.Equal(t, want, cfg.Folders.Layout)
	assert.Equal(t, want.Data, cfg.Folders.Data)
	assert.Equal(t, want.Cache, cfg.Folders.Cache)
}

func TestConfigFromArgsRejectsAHostFolderFlag(t *testing.T) {
	for _, flag := range []string{"--data-dir", "--cache-dir", "--config-dir", "--state-dir", "--app-dir"} {
		_, err := configFromArgs([]string{flag, "/tmp/x"}, noEnv.get)
		assert.Error(t, err, flag)
	}
}

// A root that cannot be read stops the start: the folders it would hold could
// not be fenced.
func TestConfigFromArgsRefusesAnUnreadableRoot(t *testing.T) {
	_, err := configFromArgs(nil, unreadableRoots.get)
	assert.Error(t, err)
}

func TestConfigFromArgsRejectsAnUnknownFlag(t *testing.T) {
	if _, err := configFromArgs([]string{"--nope"}, noEnv.get); err == nil {
		t.Error("configFromArgs(--nope) = nil error, want one")
	}
}

// A set key is kept under its provider, in every build; an unset one is not. The
// config holds the key, so the environment can be cleared behind it.
func TestConfigFromArgsReadsTheProviderKeys(t *testing.T) {
	assert.Empty(t, mustConfigFromArgs(t, nil, noEnv).App.LLMKeys)

	cfg := mustConfigFromArgs(t, nil, env{"ANTHROPIC_API_KEY": "sk-ant-test-0123456789"})

	assert.Equal(t, map[string]string{"anthropic": "sk-ant-test-0123456789"}, cfg.App.LLMKeys)
}

// A provider's base URL moves under `-tags debug` alone, by the variable
// catalog.BaseURLVars names for it: the ordinary run asserts the environment
// never reaches LLMBaseURLs.
func TestConfigFromArgsMovesAProvidersBaseURLInADebugBuild(t *testing.T) {
	cfg := mustConfigFromArgs(t, nil, env{"KUBETAIL_ANTHROPIC_BASE_URL": "https://anthropic.override"})

	if debugBuild {
		assert.Equal(t, map[string]string{"anthropic": "https://anthropic.override"}, cfg.App.LLMBaseURLs)
	} else {
		assert.Nil(t, cfg.App.LLMBaseURLs)
	}
	assert.Nil(t, mustConfigFromArgs(t, nil, noEnv).App.LLMBaseURLs, "none set is none moved")
}

// Every key read is registered with the redactor before the logger exists.
func TestConfigFromArgsRegistersEveryKeyWithTheRedactor(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	mustConfigFromArgs(t, nil, env{"ANTHROPIC_API_KEY": "sk-ant-test-0123456789"})

	assert.NotContains(t, safe.String("key sk-ant-test-0123456789 here"), "sk-ant-test-0123456789")
}

// The clear touches the table's variables alone: the config keeps the key it read,
// and a variable beside it survives.
func TestTakeProviderKeysClearsOnlyTheTable(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-0123456789")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:11434")
	cfg, err := configFromArgs(nil, os.Getenv)
	require.NoError(t, err)

	takeProviderKeys()

	_, set := os.LookupEnv("ANTHROPIC_API_KEY")
	assert.False(t, set, "the key is out of the environment")
	assert.Equal(t, "http://127.0.0.1:11434", os.Getenv("OLLAMA_HOST"), "another variable is left alone")
	assert.Equal(t, "sk-ant-test-0123456789", cfg.App.LLMKeys["anthropic"], "the config still holds it")
}

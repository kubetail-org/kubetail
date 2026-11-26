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

package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlitemigrate/sqlitemigratetest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/aws"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/chatgpt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/github"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/google"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/openrouter"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// The app keeps every service it built, so a caller reaches one through the
// runtime rather than through a constructor's arguments.
func TestNewKeepsTheRuntime(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	rt := a.rt
	for name, svc := range map[string]any{
		"DB": rt.DB, "Poke": rt.Poke, "Kubeconfig": rt.Kubeconfig, "Cluster": rt.Cluster,
		"KubetailCloud": rt.KubetailCloud, "LLM": rt.LLM, "Security": rt.Settings,
		"GitHub": rt.GitHub,
		"Memory": rt.Memory, "Chat": rt.Chat, "ChatGPT": rt.ChatGPT, "OpenRouter": rt.OpenRouter,
		"AWS": rt.AWS, "Google": rt.Google,
	} {
		assert.NotNil(t, svc, name)
	}
}

// A runtime is built apart from the servers over it, and closes what it built
// on its own. SQLite deletes the -wal beside a database when its last connection
// closes, so the sibling's absence shows app.db was released.
func TestARuntimeClosesWhatItBuilt(t *testing.T) {
	dir := t.TempDir()
	rt, err := build(t.Context(), withDirs(t, Config{Folders: Folders{Data: dir}}))
	require.NoError(t, err)

	wal := filepath.Join(dir, "app.db-wal")
	require.FileExists(t, wal)
	require.NoError(t, rt.Close())
	assert.NoFileExists(t, wal)
}

// An app.db that cannot open fails the build, saying so.
func TestAnAppDBThatCannotOpenFailsTheBuild(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "app.db"), 0o700))

	_, err := build(t.Context(), withDirs(t, Config{Folders: Folders{Data: dir}}))

	assert.ErrorContains(t, err, "open app database")
}

// An app.db a newer build migrated fails the build with the text the start screen
// shows: the file and both versions.
func TestANewerAppDBFailsTheBuildNamingTheVersions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := appdb.Open(path, 0)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	sqlitemigratetest.StampNewer(t, path)

	_, err = build(t.Context(), withDirs(t, Config{Folders: Folders{Data: dir}}))

	require.Error(t, err)
	assert.Regexp(t, `^app\.db was written by a newer Kubetail \(schema 999; this build reads up to \d+\)$`, err.Error())
	requireUnlocked(t, dir)
}

// With no shell the resolver has no prober. The comparison is to a nil
// interface: a nil *bash.Tool stored in one is not nil, and the resolver would
// call through it.
func TestTheResolverHasNoProberWithoutAShell(t *testing.T) {
	rt := &Runtime{SandboxState: sandbox.State{Available: true}}

	assert.True(t, rt.resolver().Executables == nil)
}

// Settings lists the catalog's tools that this machine's box holds, in the
// catalog's order, each on until the user turns it off.
func TestToolSettingsListTheToolsTheBoxHolds(t *testing.T) {
	rt, err := build(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, rt.Close()) })
	var want []toolsettings.Setting
	for _, l := range catalog.ToolListings() {
		if rt.Tools.Has(l.Name) {
			want = append(want, toolsettings.Setting{Listing: l, Enabled: true})
		}
	}
	got, err := rt.ToolSettings.List(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.Equal(t, want, got)
}

// The resolver and the chat service list the build's cloud platforms in its
// order.
func TestCloudTargetsKeepTheBuildsOrder(t *testing.T) {
	got := cloudTargets([]bash.CloudPlatform{targetOnly(permissions.GCP), targetOnly(permissions.AWS)})
	assert.Equal(t, []permissions.Target{permissions.GCP, permissions.AWS}, got)
	assert.Empty(t, cloudTargets(nil))
}

// With a sandbox the cloud platforms are the build's, or a test's in their
// place; without one there are none.
func TestCloudPlatformsNeedASandbox(t *testing.T) {
	sandboxed := sandbox.State{Available: true}
	configured := []bash.CloudPlatform{targetOnly(permissions.AWS)}
	assert.Equal(t, cloudTargets(builtCloudPlatforms(nil, nil)), cloudTargets(cloudPlatformsFor(sandboxed, nil, nil, nil)))
	assert.Equal(t, []permissions.Target{permissions.AWS}, cloudTargets(cloudPlatformsFor(sandboxed, configured, nil, nil)))
	assert.Empty(t, cloudPlatformsFor(sandbox.State{Reason: "no bwrap"}, nil, nil, nil))
	assert.Empty(t, cloudPlatformsFor(sandbox.State{}, configured, nil, nil))
}

// targetOnly is a cloud platform that names its target and mounts nothing.
type targetOnly permissions.Target

func (p targetOnly) Target() permissions.Target { return permissions.Target(p) }

func (targetOnly) Mount(context.Context, bash.CloudRun) (bash.CloudMount, error) {
	return bash.CloudMount{}, nil
}

// Every keyed provider is in llm.Service from launch; one with no credential lists
// no model, a save lists its written catalog on modelsStateWatch and a delete
// empties it.
func TestAKeyedRowListsItsModelsWhileItHasACredential(t *testing.T) {
	rt, err := build(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, rt.Close()) })
	stop, err := lifecycle.StartAll(t.Context(), rt.parts)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	for id := range catalog.KeyVars() {
		_, ok := rt.LLM.Provider(id)
		assert.True(t, ok, "%s is listed from launch", id)
	}

	frames, cancel := rt.LLM.WatchProvidersState()
	defer cancel()
	listed := func() bool {
		p, _ := rt.LLM.Provider("groq")
		return len(p.Catalog) > 0
	}
	assert.False(t, listed(), "no credential, no model")
	awaitRestored(t, rt)
	require.NoError(t, rt.ProviderCredentials.Save("groq", "gsk-saved-0123456789abcdef"))
	for !listed() {
		testutil.Recv(t, frames, "groq's models")
	}
	require.NoError(t, rt.ProviderCredentials.Delete("groq"))
	for listed() {
		testutil.Recv(t, frames, "groq's models leave")
	}
}

// With OPENROUTER_API_KEY set the connector still connects and disconnects,
// and the provider signs with the saved key, then the environment key.
func TestOpenRouterFallsBackAfterADisconnect(t *testing.T) {
	const envKey, savedKey = "sk-or-env-0123456789abcdef", "sk-or-saved-0123456789abcdef"
	rt, err := build(t.Context(), withDirs(t, Config{LLMKeys: map[string]string{openrouter.ID: envKey}}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, rt.Close()) })
	awaitRestored(t, rt)
	require.NoError(t, rt.OpenRouter.Disconnect(), "the environment key no longer refuses it")

	p, ok := rt.LLM.Provider(openrouter.ID)
	require.True(t, ok)
	assert.Equal(t, rt.ProviderCredentials.Signer(openrouter.ID), p.Signer)
	assert.Empty(t, p.Key)
	assert.NotEmpty(t, p.Catalog)

	bearer := func() string {
		t.Helper()
		target, err := rt.LLM.Resolve(openrouter.ID, p.Catalog[0].ID, p.Catalog[0].DefaultEffort)
		require.NoError(t, err)
		r, err := http.NewRequest(http.MethodPost, openrouter.APIBaseURL+"/chat/completions", nil)
		require.NoError(t, err)
		require.NoError(t, target.Provider.Signer.Sign(r))
		return r.Header.Get("Authorization")
	}
	assert.Equal(t, "Bearer "+envKey, bearer())
	require.NoError(t, rt.ProviderCredentials.Save(openrouter.ID, savedKey))
	assert.Equal(t, "Bearer "+savedKey, bearer())
	require.NoError(t, rt.ProviderCredentials.Delete(openrouter.ID))
	assert.Equal(t, "Bearer "+envKey, bearer())
}

// The service signs the google row at the project's Vertex AI endpoint
// whenever the launch found a project, a Gemini key or not.
func TestTheGoogleRowFollowsTheProject(t *testing.T) {
	vertexBase := google.BaseURL("my-project", "global")
	rows := func(cfg Config) (*Runtime, llm.Provider, bool) {
		t.Helper()
		rt, err := build(t.Context(), withDirs(t, cfg))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, rt.Close()) })
		p, ok := rt.LLM.Provider(google.ID)
		return rt, p, ok
	}

	rt, p, ok := rows(Config{Getenv: googleEnv("my-project")})
	require.True(t, ok)
	assert.Equal(t, rt.ProviderCredentials.Signer(google.ID), p.Signer)
	assert.Equal(t, vertexBase, p.BaseURL)
	assert.Equal(t, "my-project", rt.Google.Project())

	_, _, ok = rows(Config{})
	assert.False(t, ok, "no project")

	_, _, ok = rows(Config{Getenv: googleEnv("my-project"), LLMKeys: map[string]string{"gemini": "gm-0123456789abcdef"}})
	assert.True(t, ok, "a Gemini key does not hide Vertex AI")
}

// The state watch lists exactly the providers the catalog holds, less the fake, with
// and without a project, and the service signs each one. One part syncs every
// provider's catalog.
func TestTheStateWatchListsEveryProviderTheCatalogHolds(t *testing.T) {
	for _, getenv := range []func(string) string{nil, googleEnv("my-project")} {
		a, err := New(t.Context(), withDirs(t, Config{Getenv: getenv, AddFake: true}))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, a.Close()) })
		rt := a.rt

		var held, listed []string
		for _, p := range testutil.Current(t, rt.LLM.WatchProvidersState, "the providers") {
			if p.ID != "fake" {
				held = append(held, p.ID)
				assert.Equal(t, rt.ProviderCredentials.Signer(p.ID), p.Signer, p.ID)
			}
		}
		for _, st := range testutil.Current(t, rt.ProviderCredentials.WatchCredentialsState, "the credentials") {
			listed = append(listed, st.ID)
		}
		assert.Equal(t, held, listed)
		assert.Contains(t, listed, chatgpt.ID)
		assert.Equal(t, getenv != nil, slices.Contains(listed, google.ID))
		assert.GreaterOrEqual(t, partIndex(t, a, "provider catalogs"), 0)
		for _, gone := range []string{"chatgpt catalog", "google catalog"} {
			assert.False(t, slices.ContainsFunc(rt.parts, func(p lifecycle.Part) bool { return p.Name == gone }), gone)
		}
	}
}

// A debug build's OpenRouter base that does not parse fails the build.
func TestABadOpenRouterBaseFailsTheBuild(t *testing.T) {
	_, err := build(t.Context(), withDirs(t, Config{LLMBaseURLs: map[string]string{openrouter.ID: "http://[::1"}}))

	assert.ErrorContains(t, err, "http://[::1")
}

// A debug build's AWS sign-in URL that does not parse fails the build.
func TestABadAWSSigninURLFailsTheBuild(t *testing.T) {
	_, err := build(t.Context(), withDirs(t, Config{ConnectorAuthURLs: map[string]string{aws.ID: "http://[::1"}}))

	assert.ErrorContains(t, err, "http://[::1")
}

// A debug build's GitHub origin that does not parse fails the build.
func TestABadGitHubAuthURLFailsTheBuild(t *testing.T) {
	_, err := build(t.Context(), withDirs(t, Config{ConnectorAuthURLs: map[string]string{github.ID: "http://[::1"}}))

	assert.ErrorContains(t, err, "http://[::1")
}

// The wire reaches the app's OpenRouter connector, whatever key the
// environment holds, and the provider keys service, once the keyring is read.
func TestTheWireReachesTheOpenRouterConnector(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{LLMKeys: map[string]string{openrouter.ID: "sk-or-env-0123456789abcdef"}}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	awaitRestored(t, a.rt)
	ts := serveApp(t, a)

	assert.JSONEq(t, `{"data":{"openRouterConnectionStateWatch":{"phase":"SignedOut"}}}`, firstFrame(t, ts.URL, `subscription { openRouterConnectionStateWatch { phase } }`))
	assert.Contains(t, firstFrame(t, ts.URL, `subscription { providerCredentialsStateWatch { id source } }`), `{"id":"openrouter","source":"EnvironmentKey"}`)
}

// The wire reaches the app's AWS connector, once the keyring is read.
func TestTheWireReachesTheAWSConnector(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	awaitRestored(t, a.rt)
	ts := serveApp(t, a)

	assert.JSONEq(t, `{"data":{"awsConnectionStateWatch":{"phase":"SignedOut"}}}`, firstFrame(t, ts.URL, `subscription { awsConnectionStateWatch { phase } }`))
}

// The wire reaches the app's GitHub connector, once the keyring is read.
func TestTheWireReachesTheGitHubConnector(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	awaitRestored(t, a.rt)
	ts := serveApp(t, a)

	assert.JSONEq(t, `{"data":{"gitHubConnectionStateWatch":{"phase":"SignedOut"}}}`, firstFrame(t, ts.URL, `subscription { gitHubConnectionStateWatch { phase } }`))
}

// The wire reaches the app's Google connector, once the keyring is read.
func TestTheWireReachesTheGoogleConnector(t *testing.T) {
	a, err := New(t.Context(), withDirs(t, Config{Getenv: googleEnv("my-project")}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	startApp(t, a)
	awaitRestored(t, a.rt)
	ts := serveApp(t, a)

	assert.JSONEq(t, `{"data":{"googleConnectionStateWatch":{"phase":"SignedOut","project":"my-project"}}}`, firstFrame(t, ts.URL, `subscription { googleConnectionStateWatch { phase project } }`))
}

// googleEnv is an environment naming project as GOOGLE_CLOUD_PROJECT.
func googleEnv(project string) func(string) string {
	return func(name string) string {
		if name == "GOOGLE_CLOUD_PROJECT" {
			return project
		}
		return ""
	}
}

// The project is read through Getenv as Google's own tools read it, and one
// that builds no endpoint is none, so the row says to set one.
func TestFindGoogleProject(t *testing.T) {
	env := map[string]string{"GOOGLE_CLOUD_PROJECT": "my-project", "GOOGLE_CLOUD_LOCATION": "us-east5"}
	project, base := findGoogleProject(func(name string) string { return env[name] })
	assert.Equal(t, "my-project", project)
	assert.Equal(t, "https://us-east5-aiplatform.googleapis.com/v1/projects/my-project/locations/us-east5/endpoints/openapi", base)

	project, base = findGoogleProject(googleEnv("My-Project"))
	assert.Empty(t, project, "malformed")
	assert.Empty(t, base, "malformed")

	project, base = findGoogleProject(nil)
	assert.Empty(t, project, "no Getenv")
	assert.Empty(t, base, "no Getenv")
}

// A debug build's keyed base with no host fails the build, naming the provider.
func TestABadKeyedBaseFailsTheBuild(t *testing.T) {
	_, err := build(t.Context(), withDirs(t, Config{LLMBaseURLs: map[string]string{"groq": "localhost:8080"}}))

	assert.ErrorContains(t, err, "groq")
}

// Every service reads its restored entry from the one keyring build makes.
func TestEveryServiceReadsTheOneKeyring(t *testing.T) {
	k := oskeyringtest.New()
	a, err := New(t.Context(), withDirs(t, Config{Keyring: k}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	awaitRestored(t, a.rt)

	read := map[string]bool{}
	for _, c := range k.Calls() {
		if c.Kind == oskeyringtest.Get {
			read[c.Account] = true
		}
	}
	for _, account := range []string{
		"kubetail-cloud-refresh-token", "chatgpt-refresh-token", "google-refresh-token",
		"openrouter-api-key", "github-token", "aws-session", "openrouter-saved-api-key",
	} {
		assert.True(t, read[account], "%s is read from the one keyring", account)
	}
}

// awaitRestored waits for every service that reads the keyring to publish
// what it read.
func awaitRestored(t *testing.T, rt *Runtime) {
	t.Helper()
	for name, restored := range map[string]<-chan struct{}{
		"kubetail cloud": rt.KubetailCloud.Restored(), "chatgpt": rt.ChatGPT.Restored(), "google": rt.Google.Restored(),
		"openrouter": rt.OpenRouter.Restored(), "github": rt.GitHub.Restored(), "aws": rt.AWS.Restored(),
		"provider credentials": rt.ProviderCredentials.Restored(),
	} {
		testutil.Wait(t, restored, name+"'s restore")
	}
}

// connectorPhases is every connector's phase, by name.
func connectorPhases(rt *Runtime) map[string]connectors.Phase {
	return map[string]connectors.Phase{
		"kubetail cloud": rt.KubetailCloud.State().Phase, "chatgpt": rt.ChatGPT.State().Phase, "google": rt.Google.State().Phase,
		"openrouter": rt.OpenRouter.State().Phase, "github": rt.GitHub.State().Phase, "aws": rt.AWS.State().Phase,
	}
}

// everyConnectorIs is connectorPhases with every connector in phase.
func everyConnectorIs(phase connectors.Phase) map[string]connectors.Phase {
	return map[string]connectors.Phase{
		"kubetail cloud": phase, "chatgpt": phase, "google": phase, "openrouter": phase, "github": phase, "aws": phase,
	}
}

// savedEntry is a saved provider key's entry.
const savedEntry = `{"v":1,"key":"sk-saved-0123456789abcdef"}`

// New returns while the keyring holds every call, with every connector and
// every keyed provider Restoring, and closes with the reads still held.
func TestTheLaunchDoesNotWaitForTheKeyring(t *testing.T) {
	k := oskeyringtest.New()
	release := k.Hold()
	t.Cleanup(release)

	a, err := New(t.Context(), withDirs(t, Config{Keyring: k}))

	require.NoError(t, err)
	assert.Equal(t, everyConnectorIs(connectors.Restoring), connectorPhases(a.rt))
	credentials := testutil.Current(t, a.rt.ProviderCredentials.WatchCredentialsState, "the credentials")
	require.NotEmpty(t, credentials)
	for _, st := range credentials {
		assert.Equal(t, providercredentials.SourceRestoring, st.Source, st.ID)
	}
	testutil.WaitReturn(t, func() { assert.NoError(t, a.Close()) }, "Close")
	release()
}

// A provider with an environment key lists its models before the keyring
// answers; one with a saved key alone lists them once it does.
func TestAnEnvironmentKeyListsItsModelsAtOnce(t *testing.T) {
	k := oskeyringtest.New()
	require.NoError(t, k.Set("anthropic-api-key", savedEntry))
	release := k.Hold(oskeyringtest.Get)
	t.Cleanup(release)
	a, err := New(t.Context(), withDirs(t, Config{Keyring: k, LLMKeys: map[string]string{"openai": "sk-env-0123456789abcdef"}}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })
	startApp(t, a)
	listed := func(id string) bool {
		p, _ := a.rt.LLM.Provider(id)
		return len(p.Catalog) > 0
	}

	require.Eventually(t, func() bool { return listed("openai") }, testutil.Timeout, time.Millisecond, "the environment key's provider")
	assert.False(t, listed("anthropic"), "the saved key is unread")

	release()
	require.Eventually(t, func() bool { return listed("anthropic") }, testutil.Timeout, time.Millisecond, "the saved key's provider")
}

// One entry the keyring fails to read signs out its own service alone.
func TestABadEntrySignsOutItsOwnServiceAlone(t *testing.T) {
	k := oskeyringtest.New()
	require.NoError(t, k.Set("openrouter-api-key", `{"v":1,"key":"sk-or-0123456789abcdef"}`))
	require.NoError(t, k.Set("github-token", `{"v":1,"token":"ghs_0123456789abcdef","login":"octocat"}`))
	k.FailGet("github-token", errors.New("read failed"))
	a, err := New(t.Context(), withDirs(t, Config{Keyring: oskeyring.Gate(k, oskeyring.Pace{})}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	awaitRestored(t, a.rt)

	assert.Equal(t, connectors.SignedOut, a.rt.GitHub.State().Phase)
	assert.Equal(t, connectors.Connected, a.rt.OpenRouter.State().Phase)
}

// A dismissed prompt is asked once for the whole launch: every read fails with
// no second probe, every connector is signed out, nothing is deleted, and a
// save afterwards still reaches the keyring.
func TestADismissedPromptIsAskedOnce(t *testing.T) {
	k := oskeyringtest.New()
	require.NoError(t, k.Set("anthropic-api-key", savedEntry))
	seeded := len(k.Calls())
	k.FailGets(1, errors.New("dismissed"))
	a, err := New(t.Context(), withDirs(t, Config{Keyring: oskeyring.Gate(k, oskeyring.Pace{})}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	awaitRestored(t, a.rt)

	assert.Equal(t, everyConnectorIs(connectors.SignedOut), connectorPhases(a.rt))
	credentials := testutil.Current(t, a.rt.ProviderCredentials.WatchCredentialsState, "the credentials")
	require.NotEmpty(t, credentials)
	for _, st := range credentials {
		assert.False(t, st.HasSavedKey, st.ID)
	}
	assert.Equal(t, []oskeyringtest.Call{{Kind: oskeyringtest.Get, Account: "keyring-probe"}}, k.Calls()[seeded:])

	require.NoError(t, a.rt.ProviderCredentials.Save("groq", "sk-groq-0123456789abcdef"))
	assert.Contains(t, k.Calls(), oskeyringtest.Call{Kind: oskeyringtest.Set, Account: "groq-api-key"})
}

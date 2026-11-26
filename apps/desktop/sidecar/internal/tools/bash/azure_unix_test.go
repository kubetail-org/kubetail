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
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

const azSub = "00000000-0000-0000-0000-000000000001"

// fakeLogin is the user's az as the mount reads it, whose token, if set, is
// token.
type fakeLogin struct {
	account azureproxy.Account
	ok      bool
	token   string
}

func (f fakeLogin) Account(context.Context) (azureproxy.Account, bool) { return f.account, f.ok }

func (f fakeLogin) Token(context.Context, string) (string, error) {
	if f.token == "" {
		return "", errors.New("no token")
	}
	return f.token, nil
}

func (fakeLogin) Drop(string, string) {}

func azureAccount(name string) azureproxy.Account {
	return azureproxy.Account{
		Cloud: azureproxy.Cloud{
			Name: "AzureCloud",
			Endpoints: map[string]string{
				"resourceManager":           "https://management.azure.com/",
				"activeDirectoryResourceId": "https://management.core.windows.net/",
				"activeDirectory":           "https://login.microsoftonline.com",
				"portal":                    "https://portal.azure.com with space",
				"gallery":                   "https://gallery.azure.com/%(x)s",
				"bad key!":                  "https://x",
			},
			Suffixes: map[string]string{"storageEndpoint": "core.windows.net", "keyvaultDns": ".vault.azure.net"},
		},
		Subscriptions: []azureproxy.Subscription{
			{ID: azSub, Name: name, TenantID: "0000000a-0000-0000-0000-000000000000", IsDefault: true},
			{ID: "00000000-0000-0000-0000-000000000002", Name: "Dev", TenantID: "0000000b-0000-0000-0000-000000000000"},
		},
	}
}

func azureRun(t *testing.T) CloudRun {
	dir := t.TempDir()
	return CloudRun{On: switchedOn, Port: 6443, Dir: dir, TmpDir: t.TempDir(), CAFile: filepath.Join(dir, "ca.pem"), Session: session.Session{Kind: session.Chat}, Refusal: "kubetail: no"}
}

// The mount writes az's three files under TMPDIR and sets four variables,
// none holding the grant's token but IDENTITY_HEADER.
func TestAnAzureMountWritesItsFilesAndVariables(t *testing.T) {
	run := azureRun(t)
	m, err := NewAzure(fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true}, nil).Mount(t.Context(), run)
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	dir := filepath.Join(run.TmpDir, "azure")

	assert.Equal(t, permissions.Azure, m.Grant.Target())
	require.Len(t, m.Routes, 1)
	assert.Equal(t, "127.0.0.1:6443", m.Routes[0].Host)
	assert.True(t, m.Routes[0].TLS)
	assert.Equal(t, []string{
		"AZURE_CONFIG_DIR=" + dir,
		"IDENTITY_ENDPOINT=https://127.0.0.1:6443/kubetail/identity",
		"IDENTITY_HEADER=" + m.Grant.Token(),
		"REQUESTS_CA_BUNDLE=" + run.CAFile,
	}, m.Env)
	assertNoUserCanSet(t, m.Env)

	clouds, err := os.ReadFile(filepath.Join(dir, "clouds.config"))
	require.NoError(t, err)
	assert.Equal(t, "[kubetail]\n"+
		"endpoint_active_directory = https://login.microsoftonline.com\n"+
		"endpoint_active_directory_resource_id = https://management.core.windows.net/\n"+
		"endpoint_resource_manager = https://127.0.0.1:6443/\n"+
		"suffix_keyvault_dns = .vault.azure.net\n"+
		"suffix_storage_endpoint = core.windows.net\n", string(clouds))
	config, err := os.ReadFile(filepath.Join(dir, "config"))
	require.NoError(t, err)
	assert.Equal(t, "[cloud]\nname = kubetail\n\n[core]\ncollect_telemetry = false\n", string(config))

	var profile struct {
		Subscriptions []map[string]any `json:"subscriptions"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "azureProfile.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &profile))
	require.Len(t, profile.Subscriptions, 2)
	assert.Equal(t, map[string]any{
		"id": azSub, "name": "Pay-As-You-Go", "tenantId": "0000000a-0000-0000-0000-000000000000",
		"state": "Enabled", "isDefault": true, "environmentName": "kubetail",
		"user": map[string]any{"name": "systemAssignedIdentity", "type": "servicePrincipal", "assignedIdentityInfo": "MSI"},
	}, profile.Subscriptions[0])
	assert.Equal(t, false, profile.Subscriptions[1]["isDefault"])

	for _, name := range []string{"clouds.config", "config", "azureProfile.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
		b, _ := os.ReadFile(filepath.Join(dir, name))
		assert.NotContains(t, string(b), m.Grant.Token(), name)
	}
}

// A subscription's name is the user's text: it reaches azureProfile.json
// through the encoder alone, and the other two files never.
func TestTheAzureFilesHoldNoName(t *testing.T) {
	name := "a \"quoted\"\n[kubetail]\nendpoint_resource_manager = https://evil.example ]"
	run := azureRun(t)
	m, err := NewAzure(fakeLogin{account: azureAccount(name), ok: true}, nil).Mount(t.Context(), run)
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	dir := filepath.Join(run.TmpDir, "azure")
	plainRun := azureRun(t)
	plain, err := NewAzure(fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true}, nil).Mount(t.Context(), plainRun)
	require.NoError(t, err)
	t.Cleanup(plain.Grant.End)

	for _, f := range []string{"clouds.config", "config"} {
		got, err := os.ReadFile(filepath.Join(dir, f))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join(plainRun.TmpDir, "azure", f))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), f)
	}
	var profile struct {
		Subscriptions []struct{ Name string } `json:"subscriptions"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "azureProfile.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &profile))
	assert.Equal(t, name, profile.Subscriptions[0].Name)
}

// With no login the mount writes none of az's files and sets no variable;
// the route stays, over TLS with ca.pem written, and answers the sign-in
// sentence.
func TestARunWithNoAccountHasTheRouteAlone(t *testing.T) {
	p := NewAzure(fakeLogin{}, nil)
	r := startRun(t, cloudTool(t, switchesOn(permissions.Azure), p), chatRuntime(t, session.Chat, nil), false)
	assert.NoDirExists(t, filepath.Join(r.dir.tmp, "azure"))
	for _, kv := range r.run.Env {
		for _, name := range []string{"AZURE_CONFIG_DIR=", "IDENTITY_", "REQUESTS_CA_BUNDLE="} {
			assert.False(t, strings.HasPrefix(kv, name), kv)
		}
	}
	bundle, err := os.ReadFile(r.dir.caFile())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(bundle))

	token := r.proxy.mounts[0].Grant.Token()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:6443/subscriptions?api-version=1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := tlsOverSocket(r, roots).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Contains(t, string(body), "az login")
}

// fakeResourceManager is an upstream over TLS that keeps each Authorization
// it was sent, and a transport that reaches it whatever host it names.
func fakeResourceManager(t *testing.T) (*httptest.Server, chan string, http.RoundTripper) {
	seen := make(chan string, 8)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"value": [{"name": "aks-1"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, seen, testutil.DialAnywhere(srv)
}

// Through the real sandbox, a command that prints its environment and reads
// every file under the run's directory, its TMPDIR, the tool home and the
// workspace finds the user's token nowhere, while Resource Manager received
// it for the command's read.
func TestNoAzureCredentialIsInTheRun(t *testing.T) {
	const usersToken = "users-own-entra-token-0123456789"
	_, seen, transport := fakeResourceManager(t)
	tl := confiningTool(t)
	withTool(t, tl, "curl")
	login := fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true, token: usersToken}
	tl.cloud = Cloud{Platforms: []CloudPlatform{NewAzure(login, transport)}, Switches: switchesOn(permissions.Azure).read}
	rt := chatRuntime(t, session.Chat, nil)

	text, isError := tl.Run(t.Context(), rt, command(
		`door=${IDENTITY_ENDPOINT%/kubetail/identity}; `+
			`curl -sS --cacert "$REQUESTS_CA_BUNDLE" -H "Authorization: Bearer $IDENTITY_HEADER" "$door/subscriptions/`+azSub+`/providers/Microsoft.ContainerService/managedClusters?api-version=1"; echo; `+
			`{ env; for d in "$ZDOTDIR" "$TMPDIR" "$XDG_CONFIG_HOME/../.." "$HOME"; do find "$d" -type f ! -name dump -exec cat {} + 2>/dev/null; done; } > "$HOME/dump"`))

	require.False(t, isError, text)
	assert.Contains(t, text, `"aks-1"`)
	dump, err := os.ReadFile(filepath.Join(tools.WorkspacePath(rt.Dir), "dump"))
	require.NoError(t, err)
	assert.Contains(t, string(dump), "IDENTITY_HEADER=")
	assert.Contains(t, string(dump), "endpoint_resource_manager")
	assert.Contains(t, string(dump), "BEGIN CERTIFICATE")
	assert.NotContains(t, string(dump), usersToken)
	assert.NotContains(t, text, usersToken)
	assert.Equal(t, "Bearer "+usersToken, testutil.Recv(t, seen, "the upstream's request"))
}

// A mount whose configuration directory cannot be made fails.
func TestAnAzureMountThatCannotWriteFails(t *testing.T) {
	run := azureRun(t)
	require.NoError(t, os.Mkdir(filepath.Join(run.TmpDir, "azure"), 0o700))
	_, err := NewAzure(fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true}, nil).Mount(t.Context(), run)
	assert.Error(t, err)
}

// A run's az always has a default subscription: the user's, else the first,
// when discovery left the user's out.
func TestARunsAzHasADefaultSubscription(t *testing.T) {
	acct := azureAccount("Pay-As-You-Go")
	acct.Subscriptions[0].IsDefault = false
	run := azureRun(t)
	m, err := NewAzure(fakeLogin{account: acct, ok: true}, nil).Mount(t.Context(), run)
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	var profile struct {
		Subscriptions []struct{ IsDefault bool } `json:"subscriptions"`
	}
	raw, err := os.ReadFile(filepath.Join(run.TmpDir, "azure", "azureProfile.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &profile))
	assert.True(t, profile.Subscriptions[0].IsDefault)
	assert.False(t, profile.Subscriptions[1].IsDefault)
}

// waitingLogin is a login whose discovery never ends before its caller gives
// up.
type waitingLogin struct{ fakeLogin }

func (waitingLogin) Account(ctx context.Context) (azureproxy.Account, bool) {
	<-ctx.Done()
	return azureproxy.Account{}, false
}

// A mount waits for the login a bounded time, then mounts with none: a run
// that never touches az does not wait on a slow one.
func TestAMountWaitsForTheLoginABoundedTime(t *testing.T) {
	p := NewAzure(waitingLogin{}, nil).(*azurePlatform)
	p.wait = time.Millisecond
	m, err := p.Mount(t.Context(), azureRun(t))
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	assert.Empty(t, m.Env)
	assert.Len(t, m.Routes, 1)
}

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
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// fakeGoogle is Google as a TLS server reached whatever the host: it keeps
// each request's Authorization and answers an empty list.
type fakeGoogle struct {
	srv   *httptest.Server
	auths chan string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{auths: make(chan string, 8)}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.auths <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"logNames":[]}`)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGoogle) RoundTrip(r *http.Request) (*http.Response, error) {
	out := r.Clone(r.Context())
	out.URL.Host = g.srv.Listener.Addr().String()
	return g.srv.Client().Transport.RoundTrip(out)
}

// A run started with Google off holds none of gcloud's variables; one
// started with it on holds them.
func TestGcloudsVariablesRideOnlyARunWithGoogleOn(t *testing.T) {
	t.Parallel()

	p := NewGCP(gcpauth.New(helperAnswering("users-token", "proj-1")), nil)

	off := startRun(t, cloudTool(t, switchesOn(), p), chatRuntime(t, session.Chat, nil), false)
	assert.NotContains(t, strings.Join(off.run.Env, "\n"), "CLOUDSDK_AUTH_ACCESS_TOKEN")
	assert.NotContains(t, strings.Join(off.run.Env, "\n"), "CLOUDSDK_API_ENDPOINT_OVERRIDES")

	on := startRun(t, cloudTool(t, switchesOn(permissions.GCP), p), chatRuntime(t, session.Chat, nil), false)
	assert.Contains(t, strings.Join(on.run.Env, "\n"), "CLOUDSDK_AUTH_ACCESS_TOKEN=")
	assert.Contains(t, strings.Join(on.run.Env, "\n"), "CLOUDSDK_API_ENDPOINT_OVERRIDES_LOGGING=http://logging.gcp.kubetail.invalid/")
}

// Through the real sandbox, a run with Google on reaches it as the user, and
// neither the user's token nor the sidecar's Google variables are anywhere
// the run can read: its environment, its directory, the tool home or the
// workspace.
func TestNoGCPCredentialEntersTheSandbox(t *testing.T) {
	// Serial: it sets the process's environment, which the sidecar's is.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/kubetail-test/adc-key-file.json")
	t.Setenv("CLOUDSDK_CONFIG", "/kubetail-test/users-gcloud-config")
	const usersToken = "users-own-google-access-token"
	google := newFakeGoogle(t)
	p := NewGCP(gcpauth.New(helperAnswering(usersToken, "proj-1")), google)
	tl := confiningTool(t)
	withTool(t, tl, "curl")
	tl.cloud = Cloud{Platforms: []CloudPlatform{p}, Switches: switchesOn(permissions.GCP).read}
	rt := chatRuntime(t, session.Chat, nil)
	home := tools.ToolHomePath(rt.Dir)

	// Each value the search looks for is spelled in base64, so the command's
	// own text holds none of them.
	spelled := func(v string) string {
		return "-e \"$(echo " + base64.StdEncoding.EncodeToString([]byte(v)) + " | base64 -d)\""
	}
	var patterns []string
	for _, v := range []string{usersToken, "/kubetail-test/adc-key-file.json", "/kubetail-test/users-gcloud-config"} {
		patterns = append(patterns, spelled(v))
	}
	text, isError := tl.Run(t.Context(), rt, command(
		`curl -sS -x "http://127.0.0.1:$CLOUDSDK_PROXY_PORT" -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" `+
			`http://logging.gcp.kubetail.invalid/v2/projects/p/logs; echo; `+
			// The control: the same spelling finds a variable that is there.
			`echo "control $(env | grep -cF `+spelled("CLOUDSDK_PROXY_TYPE=http")+`)"; `+
			`env | grep -F `+strings.Join(patterns, " ")+` || echo "env clean"; `+
			`grep -rlF `+strings.Join(patterns, " ")+` "$ZDOTDIR" '`+home+`' "$HOME" 2>/dev/null || echo "files clean"`))

	require.False(t, isError, text)
	assert.Equal(t, "Bearer "+usersToken, testutil.Recv(t, google.auths, "the request upstream"))
	assert.Contains(t, text, `{"logNames":[]}`)
	assert.Contains(t, text, "control 1")
	assert.Contains(t, text, "env clean")
	assert.Contains(t, text, "files clean")
	assert.NotContains(t, text, usersToken)
}

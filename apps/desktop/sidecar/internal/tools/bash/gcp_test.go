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
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/gcpproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
)

// helperAnswering is a gcloud config-helper that answers token in project.
func helperAnswering(token, project string) func(context.Context, bool) ([]byte, error) {
	return func(context.Context, bool) ([]byte, error) {
		return fmt.Appendf(nil, `{"configuration":{"properties":{"core":{"account":"dev@example.com","project":%q}}},`+
			`"credential":{"access_token":%q,"token_expiry":%q}}`, project, token, time.Now().Add(time.Hour).Format(time.RFC3339)), nil
	}
}

// mountGCP mounts p for a foreground chat run on port 6443.
func mountGCP(t *testing.T, p CloudPlatform) CloudMount {
	t.Helper()
	m, err := p.Mount(t.Context(), CloudRun{On: switchedOn, Port: 6443, Session: session.Session{Kind: session.Chat}, Refusal: "kubetail: no"})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	return m
}

func TestGCPMountsARoutePerService(t *testing.T) {
	t.Parallel()

	p := NewGCP(gcpauth.New(helperAnswering("users-token", "proj-1")), nil)
	assert.Equal(t, permissions.GCP, p.Target())
	m := mountGCP(t, p)

	assert.Equal(t, permissions.GCP, m.Grant.Target())
	require.Len(t, m.Routes, len(gcpproxy.Services()))
	for i, name := range gcpproxy.Services() {
		assert.Equal(t, name+".gcp.kubetail.invalid", m.Routes[i].Host)
		assert.NotNil(t, m.Routes[i].Handler)
	}
}

func TestTheSandboxedEnvironmentNamesGcloud(t *testing.T) {
	t.Parallel()

	m := mountGCP(t, NewGCP(gcpauth.New(helperAnswering("users-token", "proj-1")), nil))

	assert.Equal(t, []string{
		"CLOUDSDK_AUTH_ACCESS_TOKEN=" + m.Grant.Token(),
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_COMPUTE=http://compute.gcp.kubetail.invalid/",
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINER=http://container.gcp.kubetail.invalid/",
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_IAM=http://iam.gcp.kubetail.invalid/",
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_LOGGING=http://logging.gcp.kubetail.invalid/",
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_STORAGE=http://storage.gcp.kubetail.invalid/",
		"CLOUDSDK_API_ENDPOINT_OVERRIDES_CLOUDRESOURCEMANAGER=http://cloudresourcemanager.gcp.kubetail.invalid/",
		"CLOUDSDK_PROXY_TYPE=http", "CLOUDSDK_PROXY_ADDRESS=127.0.0.1", "CLOUDSDK_PROXY_PORT=6443",
		"CLOUDSDK_CORE_PROJECT=proj-1",
		"CLOUDSDK_CORE_DISABLE_PROMPTS=1",
		"CLOUDSDK_CORE_DISABLE_USAGE_REPORTING=true",
		"CLOUDSDK_COMPONENT_MANAGER_DISABLE_UPDATE_CHECK=true",
	}, m.Env)
	assertNoUserCanSet(t, m.Env)
	assert.NotContains(t, strings.Join(m.Env, "\n"), "users-token")
}

func TestAFailedBorrowLeavesTheProjectOut(t *testing.T) {
	t.Parallel()

	src := gcpauth.New(func(context.Context, bool) ([]byte, error) { return nil, &exec.ExitError{} })
	m := mountGCP(t, NewGCP(src, nil))
	assert.NotContains(t, strings.Join(m.Env, "\n"), "CLOUDSDK_CORE_PROJECT")
}

func TestASlowHelperDoesNotDelayTheRun(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	src := gcpauth.New(func(context.Context, bool) ([]byte, error) {
		<-release
		return nil, &exec.ExitError{}
	})
	p := NewGCP(src, nil).(*gcpPlatform)
	p.projectWait = time.Millisecond

	m := mountGCP(t, p)

	assert.NotContains(t, strings.Join(m.Env, "\n"), "CLOUDSDK_CORE_PROJECT")
}

func TestTheSourceSpeaksTheProxysSentinels(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		run  func(context.Context, bool) ([]byte, error)
		want error
	}{
		"no gcloud":  {func(context.Context, bool) ([]byte, error) { return nil, exec.ErrNotFound }, gcpproxy.ErrNoCLI},
		"no account": {func(context.Context, bool) ([]byte, error) { return nil, &exec.ExitError{} }, gcpproxy.ErrNoAccount},
		"a timeout":  {func(context.Context, bool) ([]byte, error) { return nil, context.DeadlineExceeded }, gcpproxy.ErrTimeout},
		"no answer":  {func(context.Context, bool) ([]byte, error) { return []byte("not json"), nil }, gcpproxy.ErrUnreadable},
	}
	for name, tc := range cases {
		_, err := gcpSource{gcpauth.New(tc.run)}.Token(t.Context())
		assert.ErrorIs(t, err, tc.want, name)
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	blocked := gcpauth.New(func(context.Context, bool) ([]byte, error) { <-release; return nil, nil })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := gcpSource{blocked}.Token(ctx)
	assert.ErrorIs(t, err, context.Canceled, "any other error passes as it is")

	s := gcpSource{gcpauth.New(helperAnswering("users-token-value", "p"))}
	tok, err := s.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, gcpproxy.Token{Value: "users-token-value", Account: "dev@example.com"}, tok)
	s.Drop(tok.Value)
}

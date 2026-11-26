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
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/gcpproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
)

// gcpProjectWait is how long a mount waits to read the project. A slow
// gcloud leaves the run with no default project rather than late.
const gcpProjectWait = 5 * time.Second

// GCPLogin is the user's Google login as the mount reads it: the active
// account's token, and a way to drop one Google refused.
type GCPLogin interface {
	Token(ctx context.Context) (gcpauth.Token, error)
	Drop(token string)
}

// gcpPlatform is Google Cloud on a sandboxed run: gcloud pointed at the run's
// door, with the grant's token as its own.
type gcpPlatform struct {
	login       GCPLogin
	transport   http.RoundTripper
	projectWait time.Duration
}

// NewGCP is Google's cloud platform over login, sending through transport;
// nil is a transport of its own, one per sidecar, that honours the proxy the
// login shell imported.
func NewGCP(login GCPLogin, transport http.RoundTripper) CloudPlatform {
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	return &gcpPlatform{login: login, transport: transport, projectWait: gcpProjectWait}
}

func (p *gcpPlatform) Target() permissions.Target { return permissions.GCP }

// Mount is Google's grant, a route per service and gcloud's variables: its
// token is the grant's, each service's endpoint a route, and its proxy the
// run's door, so no other tool's requests change. It writes no file.
func (p *gcpPlatform) Mount(ctx context.Context, run CloudRun) (CloudMount, error) {
	g := runproxy.NewGrant(permissions.GCP, run.On, run.Session, run.Asker, run.Refusal, cloudLimits)
	h := gcpproxy.New(g, gcpSource{p.login}, p.transport)
	env := []string{"CLOUDSDK_AUTH_ACCESS_TOKEN=" + g.Token()}
	var routes []Route
	for _, name := range gcpproxy.Services() {
		host := gcpproxy.Host(name)
		routes = append(routes, Route{Host: host, Handler: h})
		env = append(env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_"+strings.ToUpper(name)+"=http://"+host+"/")
	}
	env = append(env, "CLOUDSDK_PROXY_TYPE=http", "CLOUDSDK_PROXY_ADDRESS=127.0.0.1", "CLOUDSDK_PROXY_PORT="+strconv.Itoa(run.Port))
	if project := p.project(ctx); project != "" {
		env = append(env, "CLOUDSDK_CORE_PROJECT="+project)
	}
	env = append(env,
		"CLOUDSDK_CORE_DISABLE_PROMPTS=1",
		// Neither host is a route, so neither call would get through.
		"CLOUDSDK_CORE_DISABLE_USAGE_REPORTING=true",
		"CLOUDSDK_COMPONENT_MANAGER_DISABLE_UPDATE_CHECK=true",
	)
	return CloudMount{Routes: routes, Env: env, Grant: g}, nil
}

// project is the active configuration's project, or "" when the token
// cannot be read within projectWait. The read goes on and is kept for the
// next request.
func (p *gcpPlatform) project(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, p.projectWait)
	defer cancel()
	tok, err := p.login.Token(ctx)
	if err != nil {
		return ""
	}
	return tok.Project
}

// gcpSource is the login as the proxy reads it, its sentinels the proxy's,
// so neither package imports the other.
type gcpSource struct{ login GCPLogin }

func (s gcpSource) Token(ctx context.Context) (gcpproxy.Token, error) {
	tok, err := s.login.Token(ctx)
	switch {
	case errors.Is(err, gcpauth.ErrNoCLI):
		return gcpproxy.Token{}, gcpproxy.ErrNoCLI
	case errors.Is(err, gcpauth.ErrNoAccount):
		return gcpproxy.Token{}, gcpproxy.ErrNoAccount
	case errors.Is(err, gcpauth.ErrTimeout):
		return gcpproxy.Token{}, gcpproxy.ErrTimeout
	case errors.Is(err, gcpauth.ErrUnreadable):
		return gcpproxy.Token{}, gcpproxy.ErrUnreadable
	case err != nil:
		return gcpproxy.Token{}, err
	}
	return gcpproxy.Token{Value: tok.Value, Account: tok.Account}, nil
}

func (s gcpSource) Drop(token string) { s.login.Drop(token) }

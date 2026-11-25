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

// Package azureproxy is the proxy a sandboxed run reaches Azure Resource
// Manager through: it checks the run's token, classifies each request, puts
// a change to the user, and forwards with the user's own token, which never
// enters the run. It knows no tool and no service.
package azureproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// IdentityPath is the run's managed-identity endpoint. az asks it for a
// token and gets the grant's own.
const IdentityPath = "/kubetail/identity"

// identityLife is how long the identity endpoint says the grant's token
// lasts. The grant dies with the run long before.
const identityLife = 24 * time.Hour

// What the proxy answers for itself.
const (
	refusedSignIn              = "kubetail: Azure sign-in is needed. Ask the user to run `az login`."
	refusedResource            = "kubetail: only Azure Resource Manager is reachable from the sandbox."
	refusedPath                = "kubetail: the sandbox reaches Azure Resource Manager's own paths alone."
	refusedSubscription        = "kubetail: this subscription is not one the user's az login holds."
	refusedOutsideSubscription = "kubetail: a change outside the user's subscriptions, such as a management group's, is refused."
	refusedUnreachable         = "kubetail: Azure Resource Manager could not be reached."
)

// Clouds is each cloud served, by az's name for it: its Resource Manager
// host and the resource its tokens are for. The upstream comes from here,
// never from az or the request.
var Clouds = map[string]Upstream{
	"AzureCloud":        {Host: "management.azure.com", Resource: "https://management.core.windows.net/"},
	"AzureUSGovernment": {Host: "management.usgovcloudapi.net", Resource: "https://management.core.usgovcloudapi.net/"},
	"AzureChinaCloud":   {Host: "management.chinacloudapi.cn", Resource: "https://management.core.chinacloudapi.cn/"},
}

// Upstream is one cloud's Resource Manager.
type Upstream struct {
	Host     string
	Resource string
}

// Account is what the user's az is signed into: the active cloud, and its
// enabled subscriptions, at least one.
type Account struct {
	Cloud         Cloud
	Subscriptions []Subscription
}

// Cloud is the active cloud as az shows it. The proxy reads its name; the
// tables are for the run's own az.
type Cloud struct {
	Name      string            // a key of Clouds
	Endpoints map[string]string // by az's key (resourceManager)
	Suffixes  map[string]string
}

// Subscription is one subscription the login holds.
type Subscription struct {
	ID        string
	Name      string // the user's text
	TenantID  string
	IsDefault bool
}

// Tokens is the user's Resource Manager tokens, held outside the run.
type Tokens interface {
	Token(ctx context.Context, tenant string) (string, error)
	// Drop forgets token, which the upstream answered 401.
	Drop(tenant, token string)
}

// Config is one run's proxy.
type Config struct {
	Grant *runproxy.Grant
	Port  int // the door's port, which the answers' links name
	// Account is the login as the run was mounted, nil for none: every
	// request then answers the sign-in sentence.
	Account   *Account
	Tokens    Tokens
	Transport http.RoundTripper // the upstream's, shared by every run
}

// proxy is the handler of one run's Azure route.
type proxy struct {
	Config
	upstream Upstream
	link     func(string) string // a link to the upstream, as one to the door
}

// New is the handler of one run's Azure route.
func New(cfg Config) http.Handler {
	p := &proxy{Config: cfg}
	if cfg.Account != nil {
		p.upstream = Clouds[cfg.Account.Cloud.Name]
	}
	p.link = linkRewriter(p.upstream.Host, cfg.Port)
	return p
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity := r.URL.Path == IdentityPath
	var token string
	if identity {
		token = r.Header.Get("X-Identity-Header")
	} else if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		token = v
	}
	r, done, ok := p.Grant.Admit(w, r, token)
	if !ok {
		return
	}
	defer done()
	switch {
	case p.Account == nil:
		refuse(w, http.StatusForbidden, refusedSignIn)
	case identity:
		p.identity(w, r)
	default:
		p.resourceManager(w, r)
	}
}

// identity answers az's managed-identity request with the grant's token.
// Nothing is dialled.
func (p *proxy) identity(w http.ResponseWriter, r *http.Request) {
	resource := r.URL.Query().Get("resource")
	if r.Method != http.MethodGet || !p.isResourceManager(resource) {
		refuse(w, http.StatusBadRequest, refusedResource)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": p.Grant.Token(),
		"expires_on":   strconv.FormatInt(time.Now().Add(identityLife).Unix(), 10),
		"resource":     resource,
		"token_type":   "Bearer",
	})
}

// isResourceManager is whether resource names the cloud's Resource Manager:
// its token resource or its endpoint, with or without the last slash.
func (p *proxy) isResourceManager(resource string) bool {
	resource = strings.TrimSuffix(resource, "/")
	return resource == strings.TrimSuffix(p.upstream.Resource, "/") || resource == "https://"+p.upstream.Host
}

// resourceManager serves a request for Resource Manager: the path policy,
// the classifier, the subscription, then a read forwards unasked and anything
// else is decided.
func (p *proxy) resourceManager(w http.ResponseWriter, r *http.Request) {
	a, ok := parsePath(r.Method, r.URL.Path)
	query, err := url.ParseQuery(r.URL.RawQuery)
	if !ok || r.URL.RawPath != "" || err != nil {
		refuse(w, http.StatusForbidden, refusedPath)
		return
	}
	v := classify(r.Method, a, query)
	if v.refusal != "" {
		refuse(w, http.StatusForbidden, v.refusal)
		return
	}
	sub, ok := p.subscriptionOf(a)
	switch {
	case !ok:
		refuse(w, http.StatusForbidden, refusedSubscription)
		return
	case a.subscription == "" && r.Method != http.MethodGet && r.Method != http.MethodHead:
		refuse(w, http.StatusForbidden, refusedOutsideSubscription)
		return
	}
	if v.level != permissions.Read {
		p.decide(w, r, a, v, query, sub)
		return
	}
	release, status := p.Grant.Slot(r.Context())
	defer release()
	if status != 0 {
		refuse(w, statusOf(status), runproxy.SlotRefusal(status))
		return
	}
	p.forward(w, r, sub.TenantID, nil)
}

// decide has the grant decide a request that is not a plain read, with its
// body, and forwards it once it may run.
func (p *proxy) decide(w http.ResponseWriter, r *http.Request, a armPath, v verdict, query url.Values, sub Subscription) {
	body, release, status := p.Grant.Body(w, r)
	defer release()
	if status != 0 {
		refuse(w, statusOf(status), runproxy.BodyRefusal(status))
		return
	}
	req := runproxy.Request{
		Action: action(r.Method, a, v, query, body, sub),
		Sent: runproxy.Sent{
			Host: p.upstream.Host, Method: r.Method, Path: r.URL.RequestURI(),
			ContentType: r.Header.Get("Content-Type"), Body: body, Details: details(r, a, sub),
		},
	}
	out := p.Grant.Decide(r.Context(), req, func() error {
		p.forward(w, r, sub.TenantID, body)
		return nil
	})
	if !out.Forwarded {
		refuse(w, statusOf(out.Status), out.Message)
	}
}

// subscriptionOf is the subscription the path names, else the default; a
// subscription the account does not hold is none.
func (p *proxy) subscriptionOf(a armPath) (Subscription, bool) {
	subs := p.Account.Subscriptions
	if a.subscription == "" && len(subs) > 0 {
		for _, s := range subs {
			if s.IsDefault {
				return s, true
			}
		}
		return subs[0], true
	}
	for _, s := range p.Account.Subscriptions {
		if strings.EqualFold(s.ID, a.subscription) {
			return s, true
		}
	}
	return Subscription{}, false
}

// forward sends r to the cloud's host with the user's token for tenant and
// body, and relays the answer. An upstream 401 drops the token and answers
// the sign-in sentence: az hands back the token it has cached until it
// expires, so sending again would fail the same way.
func (p *proxy) forward(w http.ResponseWriter, r *http.Request, tenant string, body []byte) {
	token, err := p.Tokens.Token(r.Context(), tenant)
	if err != nil {
		refuse(w, http.StatusForbidden, refusedSignIn)
		return
	}
	out, err := http.NewRequestWithContext(r.Context(), r.Method, "https://"+p.upstream.Host+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil { // coverage:ignore the method and the URL already parsed
		refuse(w, http.StatusForbidden, refusedUnreachable)
		return
	}
	out.Header = runproxy.Outbound(r.Header, forwarded)
	out.Header.Set("User-Agent", version.Prefix(out.Header.Get("User-Agent")))
	out.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.Transport.RoundTrip(out)
	if err != nil {
		refuse(w, http.StatusForbidden, refusedUnreachable)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		p.Tokens.Drop(tenant, token)
		refuse(w, http.StatusForbidden, refusedSignIn)
		return
	}
	relay(w, resp, p.link)
}

// forwarded are the request headers Resource Manager is sent. Nothing else
// of the request's is: no other x-ms- header, since
// x-ms-authorization-auxiliary carries other tenants' tokens; no method
// override; no Accept-Encoding, so the answer is plain.
var forwarded = []string{
	"Accept", "Accept-Language", "Content-Type", "User-Agent", "x-ms-client-request-id",
	"If-Match", "If-None-Match",
}

// statusOf is the status the proxy answers for one the grant gave: az
// retries a 429, so a full grant answers 403.
func statusOf(status int) int {
	if status == http.StatusTooManyRequests {
		return http.StatusForbidden
	}
	return status
}

// refuse answers status with msg in Resource Manager's error shape, which az
// prints.
func refuse(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "KubetailRefused", "message": msg}})
}

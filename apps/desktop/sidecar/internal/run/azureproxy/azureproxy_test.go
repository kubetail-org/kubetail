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

package azureproxy

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

const (
	sub2    = "00000000-0000-0000-0000-000000000002"
	tenantA = "0000000a-0000-0000-0000-000000000000"
	tenantB = "0000000b-0000-0000-0000-000000000000"
	door    = "127.0.0.1:6443"
)

var testAccount = &Account{Cloud: Cloud{Name: "AzureCloud"}, Subscriptions: []Subscription{
	{ID: sub, Name: "Pay-As-You-Go", TenantID: tenantA, IsDefault: true},
	{ID: sub2, Name: "Dev", TenantID: tenantB},
}}

// fakeTokens hands out tokens in order, one per call, and keeps what was
// asked and dropped.
type fakeTokens struct {
	mu      sync.Mutex
	next    []string
	err     error
	asked   []string // tenants
	dropped []string // tokens
}

func (f *fakeTokens) Token(_ context.Context, tenant string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, tenant)
	if f.err != nil {
		return "", f.err
	}
	tok := f.next[0]
	if len(f.next) > 1 {
		f.next = f.next[1:]
	}
	return tok, nil
}

func (f *fakeTokens) Drop(_, tok string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropped = append(f.dropped, tok)
}

// seen is one request the upstream received.
type seen struct {
	method, uri string
	header      http.Header
	body        string
}

// upstream is a fake Resource Manager over TLS, keeping each request, and
// answering with answer.
type upstream struct {
	*httptest.Server
	mu     sync.Mutex
	seen   []seen
	answer http.HandlerFunc
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{answer: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"value": []}`)
	}}
	u.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.seen = append(u.seen, seen{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(b)})
		answer := u.answer
		u.mu.Unlock()
		answer(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) requests() []seen {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]seen{}, u.seen...)
}

func (u *upstream) setAnswer(h http.HandlerFunc) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.answer = h
}

// transport dials u whatever host a request names.
func (u *upstream) transport() http.RoundTripper { return testutil.DialAnywhere(u.Server) }

// fakeAsker hands each Ask to the test and waits for its answer. An
// approval holds for duration, once when unset.
type fakeAsker struct {
	asked     chan runproxy.Request
	answers   chan bool
	duration  permissions.Duration
	recorded  chan runproxy.Request
	decisions chan permissions.Decision
}

func newFakeAsker() *fakeAsker {
	return &fakeAsker{
		asked: make(chan runproxy.Request, 8), answers: make(chan bool, 8),
		recorded: make(chan runproxy.Request, 8), decisions: make(chan permissions.Decision, 8),
	}
}

func (a *fakeAsker) Ask(ctx context.Context, r runproxy.Request) (permissions.Answer, error) {
	a.asked <- r
	select {
	case ok := <-a.answers:
		return permissions.Answer{Approved: ok, Duration: cmp.Or(a.duration, permissions.DurationOnce)}, nil
	case <-ctx.Done():
		return permissions.Answer{}, ctx.Err()
	}
}

func (a *fakeAsker) Record(_ context.Context, r runproxy.Request, d permissions.Decision, _ string) error {
	a.recorded <- r
	a.decisions <- d
	return nil
}

// harness is the proxy over a fake upstream, tokens and asker, in a chat
// whose every account is in policy, ask mode until a test changes it.
type harness struct {
	handler  http.Handler
	grant    *runproxy.Grant
	off      atomic.Bool // the grant's switch, on until a test turns it off
	asker    *fakeAsker
	tokens   *fakeTokens
	upstream *upstream
	mu       sync.Mutex
	policy   permissions.Policy
	accounts []string // each account a policy was read for
}

// setPolicy is the mode and rules the next decision reads.
func (h *harness) setPolicy(p permissions.Policy) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy = p
}

func (h *harness) readPolicy(_ context.Context, _ permissions.Target, account string) permissions.Policy {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accounts = append(h.accounts, account)
	return h.policy
}

func newHarness(t *testing.T, limits runproxy.Limits) *harness {
	t.Helper()
	if limits.QPS == 0 {
		limits = runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8}
	}
	h := &harness{asker: newFakeAsker(), tokens: &fakeTokens{next: []string{"users-own-token"}}, upstream: newUpstream(t),
		policy: permissions.Policy{Mode: permissions.ModeAsk}}
	sess := session.Session{Kind: session.Chat, Policy: h.readPolicy}
	h.grant = runproxy.NewGrant(permissions.Azure, func(context.Context) bool { return !h.off.Load() }, sess, h.asker, "", limits)
	t.Cleanup(h.grant.End)
	h.handler = New(Config{Grant: h.grant, Port: 6443, Account: testAccount, Tokens: h.tokens, Transport: h.upstream.transport()})
	return h
}

// request is method on target at the door, carrying the grant's token as a
// bearer.
func (h *harness) request(method, target, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "https://"+door+target, nil)
	} else {
		r = httptest.NewRequest(method, "https://"+door+target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer "+h.grant.Token())
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// refusal is the message of a Resource Manager error answer.
func refusal(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e), w.Body.String())
	assert.Equal(t, "KubetailRefused", e.Error.Code)
	return e.Error.Message
}

func identityRequest(h *harness, resource, header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://"+door+IdentityPath+"?api-version=2019-08-01&resource="+resource, nil)
	if header != "" {
		r.Header.Set("X-IDENTITY-HEADER", header)
	}
	return r
}

// With the switch off, a request with the grant's token is refused 401 and
// nothing reaches Azure.
func TestASwitchedOffPlatformIsUnauthorized(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.off.Store(true)
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=2022-12-01", ""))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, h.upstream.requests())
}

// The identity endpoint answers the grant's own token for Resource Manager,
// and dials nothing.
func TestTheIdentityEndpointAnswersTheGrantsToken(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	for _, resource := range []string{"https://management.core.windows.net/", "https://management.core.windows.net", "https://management.azure.com/"} {
		w := serve(h.handler, identityRequest(h, resource, h.grant.Token()))
		require.Equal(t, http.StatusOK, w.Code, resource)
		var answer map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
		assert.Equal(t, h.grant.Token(), answer["access_token"])
		assert.Equal(t, "Bearer", answer["token_type"])
		assert.Equal(t, resource, answer["resource"])
		assert.NotEmpty(t, answer["expires_on"])
	}

	assert.Equal(t, http.StatusUnauthorized, serve(h.handler, identityRequest(h, "https://management.core.windows.net/", "")).Code)
	w := serve(h.handler, identityRequest(h, "https://vault.azure.net", h.grant.Token()))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, refusal(t, w), "Resource Manager")
	assert.Empty(t, h.upstream.requests())
	assert.Empty(t, h.tokens.asked, "the user's token is never asked for")
}

// The bearer opens Resource Manager's paths, the identity header the
// identity endpoint, and neither the other.
func TestEachPathAcceptsItsOwnSlotAlone(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	bearer := identityRequest(h, "https://management.core.windows.net/", "")
	bearer.Header.Set("Authorization", "Bearer "+h.grant.Token())
	assert.Equal(t, http.StatusUnauthorized, serve(h.handler, bearer).Code)

	r := httptest.NewRequest(http.MethodGet, "https://"+door+"/subscriptions?api-version=1", nil)
	r.Header.Set("X-IDENTITY-HEADER", h.grant.Token())
	assert.Equal(t, http.StatusUnauthorized, serve(h.handler, r).Code)

	r = httptest.NewRequest(http.MethodGet, "https://"+door+"/subscriptions?api-version=1", nil)
	r.Header.Set("Authorization", h.grant.Token())
	assert.Equal(t, http.StatusUnauthorized, serve(h.handler, r).Code, "a token outside the Bearer scheme")
	assert.Empty(t, h.upstream.requests())
}

// A read goes to the cloud's host with the user's token for the tenant of
// the subscription it names, never the grant's.
func TestARequestCarriesTheUsersTokenUpstream(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions/"+sub2+"/providers/Microsoft.Compute/virtualMachines?api-version=2024-07-01", ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"value": []}`, w.Body.String())

	got := h.upstream.requests()
	require.Len(t, got, 1)
	assert.Equal(t, "/subscriptions/"+sub2+"/providers/Microsoft.Compute/virtualMachines?api-version=2024-07-01", got[0].uri)
	assert.Equal(t, "Bearer users-own-token", got[0].header.Get("Authorization"))
	assert.NotContains(t, strings.Join(got[0].header.Values("Authorization"), " "), h.grant.Token())
	assert.Equal(t, []string{tenantB}, h.tokens.asked)

	// A path naming no subscription takes the default's tenant.
	serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=2022-12-01", ""))
	assert.Equal(t, []string{tenantB, tenantA}, h.tokens.asked)
}

// The upstream sees the listed headers alone.
func TestOnlyTheListedHeadersAreForwarded(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	r := h.request(http.MethodGet, "/subscriptions/"+sub+"/resourcegroups?api-version=1", "")
	for name, value := range map[string]string{
		"Accept": "application/json", "Accept-Language": "en-US", "User-Agent": "AZURECLI/2.93.0",
		"x-ms-client-request-id": "c1", "x-ms-authorization-auxiliary": "Bearer other-tenant",
		"X-HTTP-Method-Override": "DELETE", "X-HTTP-Method": "DELETE", "X-Method-Override": "DELETE",
		"Accept-Encoding": "br", "CommandName": "group list", "Proxy-Authorization": "Basic x",
	} {
		r.Header.Set(name, value)
	}
	require.Equal(t, http.StatusOK, serve(h.handler, r).Code)

	got := h.upstream.requests()[0].header
	assert.Equal(t, "application/json", got.Get("Accept"))
	assert.Equal(t, "en-US", got.Get("Accept-Language"))
	assert.Equal(t, version.UserAgent()+" AZURECLI/2.93.0", got.Get("User-Agent"), "Kubetail named ahead of the command")
	assert.Equal(t, "c1", got.Get("X-Ms-Client-Request-Id"))
	for _, name := range []string{
		"X-Ms-Authorization-Auxiliary", "X-Http-Method-Override", "X-Http-Method", "X-Method-Override",
		"Commandname", "Proxy-Authorization",
	} {
		assert.Empty(t, got.Values(name), name)
	}
	assert.NotEqual(t, "br", got.Get("Accept-Encoding"), "the client's encoding is not forwarded")
}

func TestAnotherSubscriptionIsRefused(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions/00000000-0000-0000-0000-00000000000f/resourcegroups?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, refusal(t, w), "subscription")
	assert.Empty(t, h.upstream.requests())
	assert.Empty(t, h.tokens.asked)
}

// A token az cannot give is the one sign-in sentence.
func TestAFailedTokenIsOneFixedSentence(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.tokens.err = errors.New("AADSTS70043: refresh token expired for ana@example.com")
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, refusedSignIn, refusal(t, w))
	assert.Empty(t, h.upstream.requests())
}

// An upstream 401 drops the token. A read is sent once more with a fresh
// An upstream 401 drops the token and answers the sign-in sentence, for a
// read and a change alike: az would hand back the token it has cached, so a
// retry would fail the same way. The next request asks for a token again.
func TestA401DropsTheToken(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	h.tokens.next = []string{"stale-token", "fresh-token"}
	h.upstream.setAnswer(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, refusedSignIn, refusal(t, w))
	assert.Equal(t, []string{"stale-token"}, h.tokens.dropped)
	assert.Len(t, h.upstream.requests(), 1, "a read is sent once")

	w = serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusOK, w.Code, "the next read asks for a token again")

	h.upstream.setAnswer(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	done := serveAsync(h.handler, h.request(http.MethodDelete, compute+"?api-version=1", ""))
	testutil.Recv(t, h.asker.asked, "the delete")
	h.asker.answers <- true
	w = testutil.Recv(t, done, "the delete's answer")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, refusedSignIn, refusal(t, w))
	assert.Len(t, h.upstream.requests(), 3, "a change is sent once")
}

// A change on a path that names no subscription, a management group's, is
// refused: it reaches past the account the run's az shows. A read there passes.
func TestAChangeOutsideASubscriptionIsRefused(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	for _, target := range []string{
		"/providers/Microsoft.Management/managementGroups/mg/providers/Microsoft.Authorization/roleAssignments/ra",
		"/providers/Microsoft.Management/managementGroups/mg",
	} {
		w := serve(h.handler, h.request(http.MethodPut, target+"?api-version=1", "{}"))
		assert.Equal(t, http.StatusForbidden, w.Code, target)
		assert.Equal(t, refusedOutsideSubscription, refusal(t, w), target)
	}
	assert.Empty(t, h.asker.asked)
	assert.Empty(t, h.upstream.requests())

	w := serve(h.handler, h.request(http.MethodGet, "/providers/Microsoft.Management/managementGroups/mg?api-version=1", ""))
	assert.Equal(t, http.StatusOK, w.Code)
}

// With no login every request that holds the token, the identity endpoint's
// included, is the sign-in sentence.
func TestWithNoAccountEveryRequestIsTheSignInSentence(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	handler := New(Config{Grant: h.grant, Port: 6443, Tokens: h.tokens, Transport: h.upstream.transport()})
	for _, r := range []*http.Request{
		h.request(http.MethodGet, "/subscriptions?api-version=1", ""),
		identityRequest(h, "https://management.core.windows.net/", h.grant.Token()),
	} {
		w := serve(handler, r)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, refusedSignIn, refusal(t, w))
	}
	assert.Empty(t, h.tokens.asked)
}

// A request off the grammar is refused before the user's token is asked.
func TestAPathOffTheGrammarIsRefused(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	for _, target := range []string{"/batch?api-version=1", "/subscriptions/" + sub + "/a/b/c", "/subscriptions?a=%zz", "/subscriptions/%2e%2e/x"} {
		w := serve(h.handler, h.request(http.MethodGet, target, ""))
		assert.Equal(t, http.StatusForbidden, w.Code, target)
	}
	assert.Empty(t, h.upstream.requests())
	assert.Empty(t, h.tokens.asked)
}

// An action that answers a credential is refused: never asked, never
// recorded, never sent.
func TestACredentialActionIsRefusedUnasked(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	for _, target := range []string{
		rg + "/providers/Microsoft.Storage/storageAccounts/sa/listKeys",
		rg + "/providers/Microsoft.Storage/storageAccounts/sa/regenerateKey",
		aks + "/listClusterUserCredential",
		aks + "/accessProfiles/clusterAdmin/listCredential",
		rg + "/providers/Microsoft.Network/networkWatchers/nw/listTopology",
		site + "/publishxml",
		site + "/slots/staging/publishxml",
	} {
		w := serve(h.handler, h.request(http.MethodPost, target+"?api-version=1", `{}`))
		assert.Equal(t, http.StatusForbidden, w.Code, target)
	}
	w := serve(h.handler, h.request(http.MethodGet, rg+"/providers/Microsoft.DocumentDB/databaseAccounts/db/readonlykeys?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code, "a GET that answers keys")
	assert.Empty(t, h.asker.asked)
	assert.Empty(t, h.asker.recorded)
	assert.Empty(t, h.upstream.requests())
}

// serveAsync serves r on a goroutine and answers its recorder once done.
func serveAsync(h http.Handler, r *http.Request) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serve(h, r) }()
	return done
}

// A change is put to the user whole, and reaches Azure only once approved.
func TestAChangeAsksAndOnlyAnApprovalForwards(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	body := `{"location": "westus"}`
	for _, approve := range []bool{false, true} {
		done := serveAsync(h.handler, h.request(http.MethodPut, compute+"?api-version=2024-07-01", body))
		req := testutil.Recv(t, h.asker.asked, "the change")
		assert.Equal(t, permissions.Write, req.Action.AccessLevel)
		assert.Equal(t, "Azure: PUT Microsoft.Compute/virtualMachines/vm-1 in rg", req.Action.Summary)
		assert.Equal(t, runproxy.Sent{
			Host: "management.azure.com", Method: http.MethodPut, Path: compute + "?api-version=2024-07-01",
			ContentType: "application/json", Body: []byte(body),
			Details: []runproxy.Detail{
				{Label: "Subscription", Value: "Pay-As-You-Go (" + sub + ")"},
				{Label: "Resource group", Value: "rg"},
				{Label: "Resource", Value: "Microsoft.Compute/virtualMachines/vm-1"},
			},
		}, req.Sent)
		assert.Empty(t, h.upstream.requests(), "nothing is sent before the user decides")
		h.asker.answers <- approve
		w := testutil.Recv(t, done, "the answer")
		if !approve {
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Empty(t, h.upstream.requests())
			continue
		}
		assert.Equal(t, http.StatusOK, w.Code)
		got := h.upstream.requests()
		require.Len(t, got, 1)
		assert.Equal(t, http.MethodPut, got[0].method)
		assert.Equal(t, body, got[0].body)
	}
}

// Deleting a resource group is destructive and names the subscription.
func TestAGroupDeleteIsDestructive(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	done := serveAsync(h.handler, h.request(http.MethodDelete, rg+"?api-version=2024-11-01", ""))
	req := testutil.Recv(t, h.asker.asked, "the delete")
	assert.Equal(t, permissions.Destructive, req.Action.AccessLevel)
	assert.Equal(t, "Azure: delete resource group rg in Pay-As-You-Go", req.Action.Summary)
	h.asker.answers <- false
	testutil.Recv(t, done, "the answer")
}

func TestAConditionalHeaderIsDrawn(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	r := h.request(http.MethodPut, compute+"?api-version=1", `{}`)
	r.Header.Set("If-Match", `"etag-1"`)
	r.Header.Set("If-None-Match", "*")
	done := serveAsync(h.handler, r)
	req := testutil.Recv(t, h.asker.asked, "the change")
	assert.Contains(t, req.Sent.Details, runproxy.Detail{Label: "If-Match", Value: `"etag-1"`})
	assert.Contains(t, req.Sent.Details, runproxy.Detail{Label: "If-None-Match", Value: "*"})
	h.asker.answers <- true
	testutil.Recv(t, done, "the answer")
	got := h.upstream.requests()[0].header
	assert.Equal(t, `"etag-1"`, got.Get("If-Match"))
	assert.Equal(t, "*", got.Get("If-None-Match"))
}

// Each row of the secret-metadata list asks as a data read; a deployment's
// polling does not.
func TestSecretMetadataAsksAsADataRead(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	for target, summary := range map[string]string{
		site + "/config/appsettings/list?api-version=1":          "Azure: read app settings of Microsoft.Web/sites/shop/config/appsettings in rg",
		compute + "?api-version=1&$expand=userData":              "Azure: read user data of Microsoft.Compute/virtualMachines/vm-1 in rg",
		rg + "/providers/Microsoft.Resources/deployments/d1?a=1": "Azure: read a deployment of Microsoft.Resources/deployments/d1 in rg",
	} {
		method := http.MethodGet
		if strings.Contains(target, "/list") {
			method = http.MethodPost
		}
		done := serveAsync(h.handler, h.request(method, target, ""))
		req := testutil.Recv(t, h.asker.asked, target)
		assert.Equal(t, permissions.DataRead, req.Action.AccessLevel, target)
		assert.Equal(t, summary, req.Action.Summary)
		h.asker.answers <- true
		assert.Equal(t, http.StatusOK, testutil.Recv(t, done, target).Code)
	}
	w := serve(h.handler, h.request(http.MethodGet, rg+"/providers/Microsoft.Resources/deployments/d1/operationStatuses/op?api-version=1", ""))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, h.asker.asked, "polling asks nothing")
}

// Every refusal and a full grant are 403, which az does not retry; a body
// past the cap is 413.
func TestARefusalAndAFullGrantAre403(t *testing.T) {
	h := newHarness(t, runproxy.Limits{QPS: 1000, Burst: 1000, Open: 1, Bodies: 16})

	release, status := h.grant.Slot(t.Context())
	require.Zero(t, status)
	w := serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, runproxy.SlotRefusal(http.StatusTooManyRequests), refusal(t, w))
	release()

	// One change waits on the user and eight wait for it; the tenth finds
	// Decide's queue full, and is the only one answered.
	queued := make(chan *httptest.ResponseRecorder, 10)
	for range 10 {
		go func() { queued <- serve(h.handler, h.request(http.MethodPut, compute+"?api-version=1", "{}")) }()
	}
	w = testutil.Recv(t, queued, "the change past the queue")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, refusal(t, w), "waiting on the user")
	for range 9 {
		testutil.Recv(t, h.asker.asked, "a queued change")
		h.asker.answers <- false
		testutil.Recv(t, queued, "a denied change")
	}

	var releases []func()
	for range 16 {
		held := httptest.NewRequest(http.MethodPut, "https://"+door+"/", strings.NewReader("{}"))
		_, releaseBody, status := h.grant.Body(httptest.NewRecorder(), held)
		require.Zero(t, status)
		releases = append(releases, releaseBody)
	}
	w = serve(h.handler, h.request(http.MethodPut, compute+"?api-version=1", "{}"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, runproxy.BodyRefusal(http.StatusTooManyRequests), refusal(t, w))
	for _, releaseBody := range releases {
		releaseBody()
	}

	w = serve(h.handler, h.request(http.MethodPut, compute+"?api-version=1", `"`+strings.Repeat("x", 1<<20)+`"`))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	for _, target := range []string{"/batch", rg + "/providers/Microsoft.Storage/storageAccounts/sa/listKeys"} {
		w = serve(h.handler, h.request(http.MethodPost, target+"?api-version=1", "{}"))
		assert.Equal(t, http.StatusForbidden, w.Code, target)
	}
	h.upstream.Close()
	w = serve(h.handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code, "an upstream failure")
	assert.Equal(t, refusedUnreachable, refusal(t, w))
}

// A grant with no asker refuses a change in its own words, as 403.
func TestAChangeWithNoAskerIsRefused(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	g := runproxy.NewGrant(permissions.Azure, func(context.Context) bool { return true }, session.Session{Kind: session.Chat}, nil, "kubetail: this sandbox reads Azure and changes nothing.", runproxy.Limits{QPS: 1, Burst: 1, Open: 1, Bodies: 1})
	t.Cleanup(g.End)
	handler := New(Config{Grant: g, Port: 6443, Account: testAccount, Tokens: h.tokens, Transport: h.upstream.transport()})
	r := h.request(http.MethodDelete, rg+"?api-version=1", "")
	r.Header.Set("Authorization", "Bearer "+g.Token())
	w := serve(handler, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "kubetail: this sandbox reads Azure and changes nothing.", refusal(t, w))
	assert.Empty(t, h.upstream.requests())
}

// With no default subscription, a path naming none takes the first one's
// tenant.
func TestWithNoDefaultTheFirstSubscriptionIsTaken(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	handler := New(Config{Grant: h.grant, Port: 6443, Tokens: h.tokens, Transport: h.upstream.transport(),
		Account: &Account{Cloud: Cloud{Name: "AzureCloud"}, Subscriptions: []Subscription{{ID: sub2, Name: "Dev", TenantID: tenantB}}}})
	assert.Equal(t, http.StatusOK, serve(handler, h.request(http.MethodGet, "/subscriptions?api-version=1", "")).Code)
	assert.Equal(t, []string{tenantB}, h.tokens.asked)
}

// An encoded change is refused unread: the user could not read it.
func TestAnEncodedChangeIsRefused(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	r := h.request(http.MethodPut, compute+"?api-version=1", "{}")
	r.Header.Set("Content-Encoding", "gzip")
	w := serve(h.handler, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, runproxy.BodyRefusal(http.StatusForbidden), refusal(t, w))
	assert.Empty(t, h.asker.asked)
}

// An account with no subscription holds none a path could take.
func TestAnAccountWithNoSubscriptionRefuses(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	handler := New(Config{Grant: h.grant, Port: 6443, Tokens: h.tokens, Transport: h.upstream.transport(),
		Account: &Account{Cloud: Cloud{Name: "AzureCloud"}}})
	w := serve(handler, h.request(http.MethodGet, "/subscriptions?api-version=1", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, h.upstream.requests())
}

// A read is classified as it is today and forwarded unasked and unrecorded,
// under every mode, a tenant-level read included: no policy is read for it.
func TestAReadIsClassifiedAsToday(t *testing.T) {
	for _, mode := range permissions.Modes {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: mode})
		for _, target := range []string{
			"/subscriptions?api-version=1",
			"/tenants?api-version=1",
			"/providers/Microsoft.Management/managementGroups/mg?api-version=1",
			compute + "?api-version=1",
		} {
			w := serve(h.handler, h.request(http.MethodGet, target, ""))
			assert.Equal(t, http.StatusOK, w.Code, "%s %s", mode, target)
		}
		assert.Empty(t, h.asker.asked)
		assert.Empty(t, h.asker.recorded)
		assert.Empty(t, h.accounts, "a read reads no policy")
	}
}

// allowWrites is an allow of writes in the test subscription's group rg.
var allowWrites = permissions.Rule{ID: "a", Effect: permissions.Allow, AccessLevel: permissions.Write, Principal: "*", Target: "azure",
	Scope: sub + "/rg", Service: "*", Operation: "*", Resource: "*"}

func TestARuleAndAModeDecideAnAzureAction(t *testing.T) {
	start := compute + "/start?api-version=1"
	forwards := func(t *testing.T, h *harness, method, target, body string) {
		t.Helper()
		w := serve(h.handler, h.request(method, target, body))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, permissions.Allowed, testutil.Recv(t, h.asker.decisions, "the record"))
		assert.Empty(t, h.asker.asked)
	}
	t.Run("auto forwards a write unasked, recorded", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		forwards(t, h, http.MethodPost, start, "")
		assert.Len(t, h.upstream.requests(), 1)
		assert.Equal(t, []string{sub}, h.accounts, "the policy is the subscription's")
	})
	t.Run("read-only denies", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: permissions.ModeReadOnly})
		w := serve(h.handler, h.request(http.MethodPost, start, ""))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, refusal(t, w), "this account is read-only")
		assert.Equal(t, permissions.Denied, testutil.Recv(t, h.asker.decisions, "the record"))
		assert.Empty(t, h.upstream.requests())
	})
	t.Run("an allow rule forwards", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAsk, Rules: []permissions.Rule{allowWrites}})
		forwards(t, h, http.MethodPost, start, "")
	})
	t.Run("a deny rule refuses", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		deny := allowWrites
		deny.Effect = permissions.Deny
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{deny}})
		w := serve(h.handler, h.request(http.MethodPost, start, ""))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, refusal(t, w), "a rule denies it")
		assert.Empty(t, h.upstream.requests())
	})
	// A subscription's role assignment is an access control change, which an
	// access control deny refuses though a delete at that scope is otherwise
	// destructive.
	t.Run("an access control deny refuses a subscription's role assignment delete", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		deny := permissions.Rule{ID: "d", Effect: permissions.Deny, AccessLevel: permissions.AccessControl, Principal: "*",
			Target: "azure", Scope: "**", Service: "*", Operation: "*", Resource: "*"}
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAsk, Rules: []permissions.Rule{deny}})
		w := serve(h.handler, h.request(http.MethodDelete,
			"/subscriptions/"+sub+"/providers/Microsoft.Authorization/roleAssignments/ra?api-version=1", ""))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, refusal(t, w), "a rule denies it")
		assert.Empty(t, h.asker.asked)
		assert.Empty(t, h.upstream.requests())
	})
	t.Run("a command answer allows the next", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.asker.duration = permissions.DurationCommand
		done := serveAsync(h.handler, h.request(http.MethodPost, start, ""))
		req := testutil.Recv(t, h.asker.asked, "the start")
		assert.True(t, req.Grantable)
		assert.NotEmpty(t, req.CommandRule)
		assert.NotEmpty(t, req.ChatRule)
		h.asker.answers <- true
		assert.Equal(t, http.StatusOK, testutil.Recv(t, done, "the answer").Code)
		forwards(t, h, http.MethodPost, start, "")
	})
	t.Run("an in-account data read runs under auto", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		forwards(t, h, http.MethodGet, compute+"?api-version=1&$expand=userData", "")
	})
	t.Run("a data read naming another subscription asks once", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto})
		target := site + "/config/appsettings/list?api-version=1"
		done := serveAsync(h.handler, h.request(http.MethodPost, target, `{"x":"/subscriptions/`+sub2+`"}`))
		req := testutil.Recv(t, h.asker.asked, "the read")
		assert.Equal(t, sub+"/rg", req.Action.Scope)
		assert.True(t, req.Action.Once)
		assert.False(t, req.Grantable)
		h.asker.answers <- false
		testutil.Recv(t, done, "the answer")
	})
	t.Run("a data read deny for one group refuses a subscription-wide list", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		deny := permissions.Rule{ID: "d", Effect: permissions.Deny, AccessLevel: permissions.DataRead, Principal: "*",
			Target: "azure", Scope: sub + "/rg", Service: "*", Operation: "*", Resource: "*"}
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{deny}})
		w := serve(h.handler, h.request(http.MethodGet,
			"/subscriptions/"+sub+"/providers/Microsoft.ContainerInstance/containerGroups?api-version=1", ""))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, refusal(t, w), "a rule denies it")
		assert.Empty(t, h.upstream.requests())
	})
	t.Run("a deny rule for the subscription refuses a change naming another", func(t *testing.T) {
		h := newHarness(t, runproxy.Limits{})
		deny := allowWrites
		deny.Effect = permissions.Deny
		h.setPolicy(permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{deny}})
		w := serve(h.handler, h.request(http.MethodPost, start+"&target=/subscriptions/"+sub2, ""))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, refusal(t, w), "a rule denies it")
		assert.Empty(t, h.asker.asked)
		assert.Empty(t, h.upstream.requests())
	})
}

// The action the grant records and asks with holds every field the
// classifier filled: the proxy drops none before the decision.
func TestNoProxyDropsAFieldBeforeTheDecision(t *testing.T) {
	h := newHarness(t, runproxy.Limits{})
	target := rg + "/providers/Microsoft.Authorization/roleAssignments/ra?api-version=1"
	done := serveAsync(h.handler, h.request(http.MethodPut, target, `{}`))
	req := testutil.Recv(t, h.asker.asked, "the assignment")
	h.asker.answers <- false
	testutil.Recv(t, done, "the answer")

	a, _ := parsePath(http.MethodPut, rg+"/providers/Microsoft.Authorization/roleAssignments/ra")
	want := action(http.MethodPut, a, classify(http.MethodPut, a, nil), nil, []byte(`{}`), testAccount.Subscriptions[0])
	want.Principal = permissions.Chat
	assert.Equal(t, want, req.Action)
	assert.Equal(t, permissions.Action{
		Principal: permissions.Chat, Target: permissions.Azure, Scope: sub + "/rg", AccountName: sub,
		Service: "microsoft.authorization", Operation: "write", Resource: "roleassignments", Name: "ra",
		AccessLevel: permissions.AccessControl, Confined: true,
		Summary: "Azure: PUT Microsoft.Authorization/roleAssignments/ra in rg",
	}, req.Action)
}

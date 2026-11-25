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

// Package gcpproxy is the proxy a sandboxed gcloud reaches Google Cloud
// through: it holds the run's grant for Google, reads each request on six
// services, puts each change and each read of stored data to the user, and
// sends what passes with the user's token, which the sandbox never holds. It
// imports nothing of tools or services.
package gcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// Token is the user's token and the account a request draws.
type Token struct {
	Value   string
	Account string
}

// TokenSource is the user's token, borrowed at use.
type TokenSource interface {
	Token(ctx context.Context) (Token, error)
	Drop(token string)
}

// The ways a TokenSource fails, each answered with its own sentence.
var (
	ErrNoCLI      = errors.New("gcloud is not installed")
	ErrNoAccount  = errors.New("gcloud has no signed-in account")
	ErrTimeout    = errors.New("gcloud did not answer in time")
	ErrUnreadable = errors.New("gcloud's answer could not be read")
)

// forwardedHeaders is every header of a request that reaches Google. Nothing
// else does: X-HTTP-Method-Override, which Google honours, would run a GET as
// a change, and an API key or encryption key is a credential of the
// command's own.
var forwardedHeaders = []string{
	"Accept", "Accept-Encoding", "Content-Type", "User-Agent", "X-Goog-Api-Client",
	"Range", "X-Goog-User-Project",
}

// What the handler answers a request it sends nothing for.
const (
	refusedWriteBack      = "kubetail: this change would overwrite a value the sandbox cannot read; make it outside the sandbox."
	refusedNoCLI          = "kubetail: gcloud is not installed on this machine."
	refusedNoAccount      = "kubetail: gcloud has no signed-in account; the user can run `gcloud auth login`."
	refusedTimeout        = "kubetail: gcloud did not answer in time."
	refusedUnreadable     = "kubetail: gcloud's answer could not be read."
	refusedSignIn         = "kubetail: the request ended before the user's Google Cloud sign-in was read."
	refusedAccountChanged = "kubetail: the signed-in Google account changed after this request was approved, so it was not sent. Run the command again."
	refusedReach          = "kubetail: Google Cloud could not be reached."
	refusedAnswer         = "kubetail: Google Cloud's answer could not be checked, so it was not passed on."
)

// Handler is Google's routes on a run's door, over one grant.
type Handler struct {
	grant *runproxy.Grant
	src   TokenSource
	proxy *httputil.ReverseProxy
}

// errRedirect is an upstream's redirect, answered as an upstream failure.
var errRedirect = errors.New("a redirect")

// forwarding is what the reverse proxy reads of the request it sends.
type forwarding struct {
	svc   service
	token string
}

type forwardingKey struct{}

// New is the handler over grant, sending with src's token through transport.
func New(grant *runproxy.Grant, src TokenSource, transport http.RoundTripper) *Handler {
	h := &Handler{grant: grant, src: src}
	h.proxy = &httputil.ReverseProxy{
		Rewrite:        rewriteRequest,
		Transport:      transport,
		FlushInterval:  -1,
		ModifyResponse: h.checkAnswer,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, errUnreadable) {
				refuse(w, refusedAnswer)
				return
			}
			refuse(w, refusedReach)
		},
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		bearer = ""
	}
	r, done, ok := h.grant.Admit(w, r, bearer)
	if !ok {
		return
	}
	defer done()
	svc, ok := serviceOf(r.Host)
	if !ok {
		refuse(w, refusedPath)
		return
	}
	segs, why := checkPath(svc, r.URL.EscapedPath())
	if why != "" {
		refuse(w, why)
		return
	}
	q, why := checkRequest(svc, r)
	if why != "" {
		refuse(w, why)
		return
	}
	body, release, status := h.grant.Body(w, r)
	// A read gives its slot up before it is sent, so the call is made once.
	release = sync.OnceFunc(release)
	defer release()
	if status != 0 {
		refuse(w, runproxy.BodyRefusal(status))
		return
	}
	v := classify(svc.name, r.Method, segs, q, r.Header, body)
	switch {
	case v.kind == refused:
		refuse(w, v.why)
		return
	case v.kind == read:
		release()
		h.read(w, r, svc)
		return
	case svc.rewrites() && holdsABlankedValue(body):
		refuse(w, refusedWriteBack)
		return
	}
	tok, err := h.src.Token(r.Context())
	if err != nil {
		refuse(w, sourceRefusal(err))
		return
	}
	out := h.grant.Decide(r.Context(), request(svc, r, v, body, tok), func() error {
		h.forward(w, r, svc, &tok)
		return nil
	})
	if !out.Forwarded {
		refuse(w, out.Message)
	}
}

// read forwards a request that asks nobody, in one of the grant's slots.
func (h *Handler) read(w http.ResponseWriter, r *http.Request, svc service) {
	release, status := h.grant.Slot(r.Context())
	switch status {
	case 0:
	case http.StatusTooManyRequests:
		refuse(w, runproxy.SlotRefusal(status))
		return
	default:
		h.grant.Challenge(w)
		return
	}
	defer release()
	h.forward(w, r, svc, nil)
}

// forward borrows the user's token and sends r to svc's host. A request the
// user approved carries the token it drew, nil for a read, and goes as that
// token's account alone: while the user read it, the token may have been
// refreshed and the active account switched.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, svc service, drawn *Token) {
	tok, err := h.src.Token(r.Context())
	if err != nil {
		refuse(w, sourceRefusal(err))
		return
	}
	if drawn != nil && tok.Account != drawn.Account {
		refuse(w, refusedAccountChanged)
		return
	}
	ctx := context.WithValue(r.Context(), forwardingKey{}, forwarding{svc: svc, token: tok.Value})
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

// rewriteRequest is the request sent: over HTTPS to the table's host, the
// path and query as sent, the listed headers and the user's token.
func rewriteRequest(pr *httputil.ProxyRequest) {
	f := pr.In.Context().Value(forwardingKey{}).(forwarding)
	in := pr.In.URL
	pr.Out.URL = &url.URL{Scheme: "https", Host: f.svc.upstream, Path: in.Path, RawPath: in.RawPath, RawQuery: in.RawQuery}
	pr.Out.Host = ""
	pr.Out.Header = runproxy.Outbound(pr.In.Header, forwardedHeaders)
	pr.Out.Header.Set("User-Agent", version.Prefix(pr.Out.Header.Get("User-Agent")))
	if f.svc.rewrites() {
		// So the transport asks for gzip itself and hands back plain bytes,
		// and an answer is whole JSON.
		pr.Out.Header.Del("Accept-Encoding")
		pr.Out.Header.Del("Range")
	}
	pr.Out.Header.Set("Authorization", "Bearer "+f.token)
}

// checkAnswer drops a token Google says is dead, refuses a redirect, which
// would send the command elsewhere, and rewrites what holds secrets.
func (h *Handler) checkAnswer(resp *http.Response) error {
	f := resp.Request.Context().Value(forwardingKey{}).(forwarding)
	if resp.StatusCode == http.StatusUnauthorized {
		h.src.Drop(f.token)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return errRedirect
	}
	if f.svc.rewrites() {
		return rewriteAnswer(f.svc, resp)
	}
	return nil
}

// request is what the user is asked to approve.
func request(svc service, r *http.Request, v verdict, body []byte, tok Token) runproxy.Request {
	details := []runproxy.Detail{{Label: "Account", Value: tok.Account}, {Label: "Service", Value: svc.name}}
	if p := r.Header.Get("X-Goog-User-Project"); p != "" {
		details = append(details, runproxy.Detail{Label: "Quota project", Value: p})
	}
	return runproxy.Request{
		Action: v.act,
		Sent: runproxy.Sent{
			Host: svc.upstream, Method: r.Method, Path: r.URL.RequestURI(),
			ContentType: r.Header.Get("Content-Type"), Body: body, Details: details,
		},
	}
}

// summary is the request in a line, from its parsed path, never its body:
// the method, the service and the resource past the API's version.
func summary(svc, method string, segs []string) string {
	return "Google Cloud: " + method + " " + svc + " " + strings.Join(pastVersion(segs), "/")
}

// pastVersion is segs past the first that names an API version, all of segs
// when none does.
func pastVersion(segs []string) []string {
	for i, s := range segs {
		if isVersion(s) {
			return segs[i+1:]
		}
	}
	return segs
}

// isVersion is whether seg names an API version: v1, v1beta1, beta, alpha.
func isVersion(seg string) bool {
	if seg == "beta" || seg == "alpha" {
		return true
	}
	return len(seg) > 1 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9'
}

// holdsABlankedValue is whether a change's body holds a value the rewriter
// blanked, so sending it would overwrite the real one. A body that is not
// JSON is searched as text.
func holdsABlankedValue(body []byte) bool {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return bytes.Contains(body, []byte(safe.Redacted))
	}
	return holdsRedacted(v)
}

// holdsRedacted is whether any string in v, keys included, holds
// safe.Redacted.
func holdsRedacted(v any) bool {
	switch v := v.(type) {
	case string:
		return strings.Contains(v, safe.Redacted)
	case []any:
		return slices.ContainsFunc(v, holdsRedacted)
	case map[string]any:
		for k, e := range v {
			if holdsRedacted(k) || holdsRedacted(e) {
				return true
			}
		}
	}
	return false
}

// sourceRefusal is a TokenSource's failure in the user's words.
func sourceRefusal(err error) string {
	switch {
	case errors.Is(err, ErrNoCLI):
		return refusedNoCLI
	case errors.Is(err, ErrNoAccount):
		return refusedNoAccount
	case errors.Is(err, ErrTimeout):
		return refusedTimeout
	case errors.Is(err, ErrUnreadable):
		return refusedUnreadable
	}
	return refusedSignIn
}

// googleError is Google's error shape, which gcloud prints.
type googleError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// refuse answers message as Google's 403. gcloud retries a 429 and a 5xx, so
// every refusal is a 403, which it prints once.
func refuse(w http.ResponseWriter, message string) {
	var e googleError
	e.Error.Code, e.Error.Message, e.Error.Status = http.StatusForbidden, message, "PERMISSION_DENIED"
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(e)
}

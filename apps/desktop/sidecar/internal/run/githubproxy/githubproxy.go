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

// Package githubproxy is the proxy a sandboxed command reaches GitHub
// through: git's smart HTTP protocol on GitHost and gh's REST and GraphQL on
// APIHost, each forwarded to its fixed origin with the user's token, which the
// sandbox never holds. A read is forwarded unasked; a change, a push and a
// read of stored data go through the grant's Decide; a request whose answer is
// a credential is refused. It knows no tool and no service: the token comes
// through a TokenSource.
package githubproxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// The hosts a run's git and gh dial. gh speaks plain HTTP to github.localhost
// alone, and sends its API requests to api.github.localhost; neither name
// resolves, since both tools send each request to the proxy in absolute form.
const (
	GitHost = "github.localhost"
	APIHost = "api.github.localhost"
)

// TokenSource is the user's GitHub token.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	// Refused says GitHub answered a request carrying token 401.
	Refused(token string)
}

// Upstreams is where the proxy sends each host's requests, and the client it
// sends them with.
type Upstreams struct {
	Git, API string // origins: a scheme and a host, no path
	Client   *http.Client
}

// GitHub is github.com's upstreams, dialled with the sidecar's roots.
func GitHub() Upstreams {
	return Upstreams{
		Git: "https://github.com", API: "https://api.github.com",
		Client: &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
	}
}

// route is one of the proxy's two hosts, and the upstream it reaches.
type route struct {
	origin   string   // the upstream's
	upstream *url.URL // origin, parsed
	name     string   // the route's host, as the run dials it
	api      bool     // the API host, which answers in GitHub's JSON shape
}

func newRoute(origin, name string, api bool) route {
	u, _ := url.Parse(origin)
	return route{origin: origin, upstream: u, name: name, api: api}
}

// The headers forwarded, by exact name. Each that changes what a request does
// is one the classifier reads or a request that asks draws.
var (
	apiHeaders = []string{
		"Accept", "X-GitHub-Api-Version", "Content-Type", "Content-Length",
		"Accept-Encoding", "User-Agent", "If-None-Match", "If-Modified-Since",
	}
	// GraphQL-Features turns on preview fields, and is drawn as a detail.
	graphQLHeaders = slices.Concat(apiHeaders, []string{"GraphQL-Features"})
	gitHeaders     = []string{
		"Accept", "Content-Type", "Content-Length",
		"Accept-Encoding", "User-Agent", "If-None-Match", "If-Modified-Since",
	}
	// Git-Protocol picks the protocol version, and rides a fetch alone, which
	// is a read in every version, so a push arrives in the form the push
	// reader reads. A fetch's body is compressed and streams unread.
	refsHeaders  = slices.Concat(gitHeaders, []string{"Git-Protocol"})
	fetchHeaders = slices.Concat(gitHeaders, []string{"Git-Protocol", "Content-Encoding"})
)

// answerDropped are the answer's headers the proxy never hands back: the
// upstream's cookies, and those of its connection rather than the answer.
var answerDropped = map[string]bool{
	"Set-Cookie": true, "Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Connection": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
}

type proxy struct {
	grant    *runproxy.Grant
	tokens   TokenSource
	client   *http.Client
	git, api route
}

// New is the proxy over grant, reaching up with the token tokens answers.
// It follows no redirect.
func New(grant *runproxy.Grant, tokens TokenSource, up Upstreams) http.Handler {
	client := *up.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &proxy{
		grant: grant, tokens: tokens, client: &client,
		git: newRoute(up.Git, GitHost, false),
		api: newRoute(up.API, APIHost, true),
	}
}

// ServeHTTP checks the grant's token, the proxy credentials' password, then
// hands r to its host.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r, done, ok := p.grant.Admit(w, r, proxyPassword(r))
	if !ok {
		return
	}
	defer done()
	switch r.Host {
	case APIHost:
		p.serveAPI(w, r)
	case GitHost:
		p.serveGit(w, r)
	default:
		http.Error(w, refusedHost, http.StatusForbidden)
	}
}

// proxyPassword is the password of r's Basic proxy credentials, or "".
func proxyPassword(r *http.Request) string {
	scheme, credentials, _ := strings.Cut(r.Header.Get("Proxy-Authorization"), " ")
	if !strings.EqualFold(scheme, "Basic") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		return ""
	}
	_, password, _ := strings.Cut(string(raw), ":")
	return password
}

// unreadable is why r cannot be sent as the classifier read it, or "": a
// path that reads differently decoded, raw or resolved, and an encoded body
// where encoded is false.
func unreadable(r *http.Request, encoded bool) string {
	switch {
	case r.URL.RawPath != "" || path.Clean(r.URL.Path) != r.URL.Path:
		return refusedPath
	case !encoded && len(r.Header.Values("Content-Encoding")) > 0:
		return runproxy.BodyRefusal(http.StatusForbidden)
	}
	return ""
}

func (p *proxy) serveAPI(w http.ResponseWriter, r *http.Request) {
	rt := &p.api
	if why := unreadable(r, false); why != "" {
		rt.refuse(w, http.StatusForbidden, why)
		return
	}
	if r.URL.Path == "/graphql" {
		p.serveGraphQL(w, r)
		return
	}
	v := classifyAPI(r.Method, r.URL.Path, r.Header.Get("Accept"))
	switch {
	case v.refusal != "":
		rt.refuse(w, http.StatusForbidden, v.refusal)
	case v.act.AccessLevel == permissions.Read:
		p.read(w, r, rt, apiHeaders)
	case v.act.AccessLevel == permissions.DataRead:
		p.ask(w, r, rt, apiRequest(r, rt.upstream.Host, v.act, nil), apiHeaders)
	default:
		body, release, status := p.grant.Body(w, r)
		defer release()
		if status != 0 {
			rt.refuse(w, status, runproxy.BodyRefusal(status))
			return
		}
		v = v.withBody(r.URL.Path, body)
		p.ask(w, r, rt, apiRequest(r, rt.upstream.Host, v.act, body), apiHeaders)
	}
}

// serveGraphQL reads a GraphQL request's body, forwards a query unasked and
// asks for a mutation. The body is the whole request: GitHub reads a query
// from the URL when the body is not JSON, so a query string or another media
// type would run what the classifier never read.
func (p *proxy) serveGraphQL(w http.ResponseWriter, r *http.Request) {
	rt := &p.api
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || mediaType != "application/json" {
		rt.refuse(w, http.StatusForbidden, refusedGraphQL)
		return
	}
	body, release, status := p.grant.Body(w, r)
	defer release()
	if status != 0 {
		rt.refuse(w, status, runproxy.BodyRefusal(status))
		return
	}
	g, ok := readGraphQL(body)
	if !ok {
		rt.refuse(w, http.StatusForbidden, refusedGraphQL)
		return
	}
	if g.destructive() && g.changesAccess() {
		// One request is decided at one level, which would leave a rule for the
		// other unmatched.
		rt.refuse(w, http.StatusForbidden, refusedGraphQLMixed)
		return
	}
	if g.action().AccessLevel == permissions.Read {
		p.read(w, r, rt, graphQLHeaders)
		return
	}
	p.ask(w, r, rt, graphQLRequest(r, rt.upstream.Host, g, body), graphQLHeaders)
}

func (p *proxy) serveGit(w http.ResponseWriter, r *http.Request) {
	rt := &p.git
	repo, svc, ok := classifyGit(r.Method, r.URL.Path, r.URL.RawQuery)
	if why := unreadable(r, ok && svc == uploadPack); why != "" {
		rt.refuse(w, http.StatusForbidden, why)
		return
	}
	if !ok {
		rt.refuse(w, http.StatusForbidden, refusedGitRoute)
		return
	}
	switch svc {
	case uploadPackRefs:
		p.read(w, r, rt, refsHeaders)
	case uploadPack:
		// A fetch's path says what it is, so its body streams unread.
		p.read(w, r, rt, fetchHeaders)
	case receivePackRefs:
		p.read(w, r, rt, gitHeaders)
	case receivePack:
		p.push(w, r, repo)
	}
}

// push reads a push's opening, forwards git's probe unasked, and asks for
// the rest, whose pack then streams to GitHub unread.
func (p *proxy) push(w http.ResponseWriter, r *http.Request, repo string) {
	rt := &p.git
	// The opening is read and held in a body slot, as Grant.Body holds a
	// body, until the push ends.
	release, status := p.grant.BodySlot(w, r)
	defer release()
	if status != 0 {
		rt.refuse(w, status, runproxy.BodyRefusal(status))
		return
	}
	opening, pu, err := readPush(r.Body)
	switch {
	case errors.Is(err, errPushTooLarge):
		rt.refuse(w, http.StatusForbidden, refusedPushTooLarge)
		return
	case err != nil:
		rt.refuse(w, http.StatusForbidden, refusedPush)
		return
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(opening), r.Body))
	if pu.probe {
		p.read(w, r, rt, gitHeaders)
		return
	}
	p.ask(w, r, rt, pushRequest(r, rt.upstream.Host, repo, pu), gitHeaders)
}

// read forwards a read in one of the grant's open slots.
func (p *proxy) read(w http.ResponseWriter, r *http.Request, rt *route, headers []string) {
	release, status := p.grant.Slot(r.Context())
	if status != 0 {
		rt.refuse(w, status, runproxy.SlotRefusal(status))
		return
	}
	defer release()
	p.forward(w, r, rt, headers, false)
}

// ask has the grant decide req and forwards it once it may run. A data
// read's answer, once allowed or approved, may hand back a link to another
// host.
func (p *proxy) ask(w http.ResponseWriter, r *http.Request, rt *route, req runproxy.Request, headers []string) {
	out := p.grant.Decide(r.Context(), req, func() error {
		p.forward(w, r, rt, headers, req.Action.AccessLevel == permissions.DataRead)
		return nil
	})
	if !out.Forwarded {
		rt.refuse(w, out.Status, out.Message)
	}
}

// forward sends r to rt's upstream with the headers named and the user's
// token, and hands the answer back. A GET and a HEAD go up with no body, since
// none of theirs is read. It follows no redirect: a link to the upstream's own
// origin is handed back as the route's, and one to another host only where
// mayLeave, so no signed link reaches a command unasked.
func (p *proxy) forward(w http.ResponseWriter, r *http.Request, rt *route, headers []string, mayLeave bool) {
	// A body streamed to GitHub unread, a fetch's or a push's pack, would
	// hold the transport past the request's end: its write loop waits on the
	// client's next byte, which no context wakes.
	stop := runproxy.CutReadsOnEnd(w, r)
	defer stop()
	ctx := r.Context()
	token, err := p.tokens.Token(ctx)
	if err != nil {
		rt.refuse(w, http.StatusForbidden, refusedNoLogin)
		return
	}
	body, length := r.Body, r.ContentLength
	if length == 0 || r.Method == http.MethodGet || r.Method == http.MethodHead {
		body, length = http.NoBody, 0
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, rt.origin+r.URL.RequestURI(), body)
	if err != nil { // coverage:ignore a fixed origin and a request's own path and query always parse
		rt.refuse(w, http.StatusBadGateway, refusedUnreachable)
		return
	}
	out.ContentLength = length
	out.Header = runproxy.Outbound(r.Header, headers)
	out.Header.Set("User-Agent", version.Prefix(out.Header.Get("User-Agent")))
	out.Header.Set("Authorization", rt.authorization(token))
	resp, err := p.client.Do(out)
	if err != nil {
		rt.refuse(w, http.StatusBadGateway, refusedUnreachable)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		p.tokens.Refused(token)
	}
	if !rt.handBack(resp.Header, mayLeave) {
		rt.refuse(w, http.StatusForbidden, refusedLink)
		return
	}
	for name, values := range resp.Header {
		if !answerDropped[name] {
			w.Header()[name] = values
		}
	}
	w.WriteHeader(resp.StatusCode)
	copyFlushing(w, resp.Body)
}

// authorization is the user's token as rt's upstream reads it.
func (rt *route) authorization(token string) string {
	if rt.api {
		return "Bearer " + token
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
}

// handBack hands the links in an answer's Link and Location back as the run
// follows them, since gh and git follow a link as written: one on the
// upstream's origin names the route's, so a later page or a renamed
// repository comes back through the proxy. One to another host passes only
// where mayLeave; elsewhere a Link holding one is dropped, and a Location
// answers false.
func (rt *route) handBack(h http.Header, mayLeave bool) bool {
	var links []string
	for _, link := range h.Values("Link") {
		if link, ok := rt.handBackLink(link, mayLeave); ok {
			links = append(links, link)
		}
	}
	h.Del("Link")
	for _, link := range links {
		h.Add("Link", link)
	}
	location := h.Get("Location")
	if location == "" {
		return true
	}
	location, ok := rt.handBackURL(location, mayLeave)
	if ok {
		h.Set("Location", location)
	}
	return ok
}

// linkTarget is one link's URL in a Link header.
var linkTarget = regexp.MustCompile(`<([^>]*)>`)

// handBackLink is a Link header with each of its URLs handed back, and false
// if any of them may not be.
func (rt *route) handBackLink(link string, mayLeave bool) (string, bool) {
	ok := true
	link = linkTarget.ReplaceAllStringFunc(link, func(target string) string {
		target, kept := rt.handBackURL(target[1:len(target)-1], mayLeave)
		ok = ok && kept
		return "<" + target + ">"
	})
	return link, ok
}

// handBackURL is raw as the run follows it: the route's on the upstream's
// origin, however raw spells it; as it is when relative, or on another host
// where mayLeave; and false otherwise, and for a URL that does not parse.
func (rt *route) handBackURL(raw string, mayLeave bool) (string, bool) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", false
	case u.Scheme == "" && u.Host == "":
		return raw, true
	case strings.EqualFold(u.Scheme, rt.upstream.Scheme) && strings.EqualFold(u.Hostname(), rt.upstream.Hostname()) &&
		portOf(u) == portOf(rt.upstream):
		u.Scheme, u.Host = "http", rt.name
		return u.String(), true
	}
	return raw, mayLeave
}

// portOf is u's port, 443 when it names none: only an HTTPS URL, as both
// upstreams are, is compared.
func portOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return "443"
}

// copyFlushing copies an answer to w as it arrives, since a fetch and a log
// stream, and breaks the client's answer off where GitHub's breaks off.
func copyFlushing(w http.ResponseWriter, body io.Reader) {
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil { // coverage:ignore a client gone mid-answer
				return
			}
			_ = rc.Flush()
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			// Returning would end the answer as if it were whole.
			panic(http.ErrAbortHandler)
		}
	}
}

// refuse answers message with status in the shape rt's client prints: the
// API host's in GitHub's JSON, the Git host's as text.
func (rt *route) refuse(w http.ResponseWriter, status int, message string) {
	if !rt.api {
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Message string `json:"message"`
	}{message})
}

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

//go:build debug

// KUBETAIL_FAULTS, a debug build's injected request failures: the rules, and the transport
// wrapper that answers a matching request the way a failing server or network would, so every
// layer above the connection runs for real. A release build has the stubs in
// faults_release.go. → docs/development-switches.md.
package kubeconn

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/transport"
)

// Faults is the parsed KUBETAIL_FAULTS. The zero value injects nothing.
type Faults struct {
	rules []rule
	store bool
}

// rule fails a request whose verb is among verbs and whose resource is resource, or any
// resource for "*". code is the status a status fault answers with, and 0 for every other fault.
type rule struct {
	verbs    []string
	resource string
	fault    string
	code     int
}

// The faults a rule can name besides a status code.
const (
	faultRefused = "refused"
	faultTLS     = "tls"
	faultTimeout = "timeout"
	faultQuiet   = "quiet"
	faultStalled = "stalled"
)

// resourceName is a resource as a request path spells it, the discovery root's "api" among them.
var resourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// ParseFaults reads a KUBETAIL_FAULTS spec: rules split by ";", each `<verbs> <resource>=<fault>`
// or `store=fail`. Any rule it cannot read fails the whole spec, quoting the rule.
func ParseFaults(spec string) (Faults, error) {
	var f Faults
	for raw := range strings.SplitSeq(spec, ";") {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		lhs, fault, ok := strings.Cut(text, "=")
		if !ok {
			return Faults{}, fmt.Errorf("rule %q: want <verbs> <resource>=<fault>", text)
		}
		lhs, fault = strings.TrimSpace(lhs), strings.TrimSpace(fault)
		if lhs == "store" {
			if fault != "fail" {
				return Faults{}, fmt.Errorf("rule %q: the store's one fault is fail", text)
			}
			f.store = true
			continue
		}
		r, err := parseRule(lhs, fault)
		if err != nil {
			return Faults{}, fmt.Errorf("rule %q: %w", text, err)
		}
		f.rules = append(f.rules, r)
	}
	return f, nil
}

func parseRule(lhs, fault string) (rule, error) {
	i := strings.LastIndexAny(lhs, " \t")
	if i < 0 {
		return rule{}, errors.New("want <verbs> <resource>=<fault>")
	}
	r := rule{resource: strings.TrimSpace(lhs[i+1:]), fault: fault}
	if r.resource != "*" && !resourceName.MatchString(r.resource) {
		return rule{}, fmt.Errorf("resource %q is not a lower-case resource name or *", r.resource)
	}
	for verb := range strings.SplitSeq(lhs[:i], ",") {
		verb = strings.TrimSpace(verb)
		if verb != "get" && verb != "list" && verb != "watch" {
			return rule{}, fmt.Errorf("verb %q is not get, list or watch", verb)
		}
		r.verbs = append(r.verbs, verb)
	}

	if n, err := strconv.Atoi(fault); err == nil && statusReasons[n] != "" {
		r.code = n
		return r, nil
	}
	switch fault {
	case faultRefused, faultTLS:
	case faultTimeout:
		if slices.Contains(r.verbs, "watch") {
			return rule{}, errors.New("timeout is for get and list; a quiet watch is quiet")
		}
	case faultQuiet, faultStalled:
		if slices.ContainsFunc(r.verbs, func(v string) bool { return v != "watch" }) {
			return rule{}, fmt.Errorf("%s is for watch only", fault)
		}
	default:
		return rule{}, fmt.Errorf("fault %q is not 401, 403, 404, 429, 500, 503, refused, tls, timeout, quiet or stalled", fault)
	}
	return r, nil
}

// StoreFails reports whether every cache's store should refuse to open.
func (f Faults) StoreFails() bool { return f.store }

// readRequest reads the verb and resource the API server would see in req, and false for a
// request no rule can match: anything but a GET, a group or version listing, a subresource.
// prefix is the base URL's path, which the server never sees as part of the request.
func readRequest(req *http.Request, prefix string) (verb, resource string, ok bool) {
	if req.Method != http.MethodGet {
		return "", "", false
	}
	path := strings.TrimPrefix(req.URL.Path, strings.TrimSuffix(prefix, "/"))
	if path == "/api" {
		return "get", "api", true
	}

	segs := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case segs[0] == "api" && len(segs) > 2:
		segs = segs[2:]
	case segs[0] == "apis" && len(segs) > 3:
		segs = segs[3:]
	default:
		return "", "", false
	}
	if segs[0] == "namespaces" && len(segs) > 2 {
		if len(segs) == 3 && (segs[2] == "status" || segs[2] == "finalize") {
			return "", "", false
		}
		segs = segs[2:]
	}

	switch {
	case len(segs) > 2:
		return "", "", false
	case req.URL.Query().Get("watch") == "true":
		verb = "watch"
	case len(segs) == 2:
		verb = "get"
	default:
		verb = "list"
	}
	return verb, segs[0], true
}

// match is the first rule, in the order written, that fails verb on resource.
func (f Faults) match(verb, resource string) (rule, bool) {
	for _, r := range f.rules {
		if (r.resource == "*" || r.resource == resource) && slices.Contains(r.verbs, verb) {
			return r, true
		}
	}
	return rule{}, false
}

// wrapper is the transport wrapper for one connection, whose base URL carries prefix, and
// nil when no rule fails a request.
func (f Faults) wrapper(prefix string) transport.WrapperFunc {
	if len(f.rules) == 0 {
		return nil
	}
	return func(rt http.RoundTripper) http.RoundTripper {
		return &faultRoundTripper{
			base: rt, faults: f, prefix: prefix,
			afterFunc: context.AfterFunc, quiet: map[string]*quietSequence{},
		}
	}
}

// faultRoundTripper answers a request a rule matches as the transport would have, and passes
// every other request to base. One per connection, so a rebuilt connection starts every quiet
// collection afresh.
type faultRoundTripper struct {
	base   http.RoundTripper
	faults Faults
	prefix string
	// afterFunc registers a sequence's reset on its first open's context: context.AfterFunc,
	// or a test's own so it can hold the callback.
	afterFunc func(context.Context, func()) func() bool

	mu sync.Mutex
	// quiet is the sequence each quiet collection path is in; absent is Idle.
	quiet map[string]*quietSequence
}

// quietSequence is one collection's quiet watch: a silent first open, then a reopen that hangs.
// It ends when the first open's context does. Compared by pointer, so a late callback from an
// ended sequence never touches the one that replaced it.
type quietSequence struct {
	ctx context.Context
	// reopening is set once the first open's body is closed: the next open is the reopen.
	reopening bool
}

func (t *faultRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	verb, resource, ok := readRequest(req, t.prefix)
	if !ok {
		return t.base.RoundTrip(req)
	}
	r, ok := t.faults.match(verb, resource)
	if !ok {
		return t.base.RoundTrip(req)
	}
	if req.Body != nil {
		// A RoundTripper closes the body on every path, and this one never sends it.
		_ = req.Body.Close()
	}
	switch r.fault {
	case faultRefused:
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	case faultTLS:
		return nil, x509.UnknownAuthorityError{}
	case faultTimeout:
		return hang(req)
	case faultQuiet:
		return t.quietOpen(req)
	case faultStalled:
		return stalledResponse(req), nil
	default:
		return statusResponse(req, r.code), nil
	}
}

// quietOpen plays one collection's quiet sequence, which kubesync reads as Resuming: a silent
// first open proves the watch alive for a window, and the reopen after it hangs past a second.
//
// An open first retires a sequence whose first open's context has ended. A stopped worker ends
// that context however far it got, so a replacement never meets a reopen left by one gone.
func (t *faultRoundTripper) quietOpen(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	t.mu.Lock()
	seq := t.quiet[path]
	if seq != nil && seq.ctx.Err() != nil {
		seq = nil
	}
	switch {
	case seq == nil:
		seq = &quietSequence{ctx: req.Context()}
		t.quiet[path] = seq
		t.mu.Unlock()
		// Frees a collection nothing opens again. It decides nothing: the check above is
		// what an open relies on, since this runs asynchronously.
		t.afterFunc(seq.ctx, func() { t.retire(path, seq) })
		return silentResponse(req, func() { t.reopen(path, seq) }), nil
	case !seq.reopening:
		t.mu.Unlock()
		return silentResponse(req, nil), nil
	default:
		t.mu.Unlock()
		return hang(req)
	}
}

// reopen marks seq's first body closed, if seq is still the collection's sequence.
func (t *faultRoundTripper) reopen(path string, seq *quietSequence) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quiet[path] == seq {
		seq.reopening = true
	}
}

// retire drops seq, if it is still the collection's sequence.
func (t *faultRoundTripper) retire(path string, seq *quietSequence) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quiet[path] == seq {
		delete(t.quiet, path)
	}
}

// silentResponse is a watch the server opened and sends nothing on. onClose, if set, runs once
// when the body is closed.
func silentResponse(req *http.Request, onClose func()) *http.Response {
	body := &silentBody{ctx: req.Context(), closed: make(chan struct{}), onClose: onClose}
	// -1: unknown, as a streamed watch's is.
	return jsonResponse(req, http.StatusOK, body, -1)
}

// stalledResponse is a watch that sends one bookmark at the position it was opened at, then
// nothing, which kubesync reads as Stale. The bookmark's kind is a placeholder: the fault does
// not know the collection's, and kubesync reads only the position.
func stalledResponse(req *http.Request) *http.Response {
	bookmark, _ := json.Marshal(metav1.WatchEvent{
		Type: string(watch.Bookmark),
		Object: runtime.RawExtension{Raw: fmt.Appendf(nil,
			`{"kind":"Bookmark","apiVersion":"v1","metadata":{"resourceVersion":%q}}`,
			req.URL.Query().Get("resourceVersion"))},
	})
	silent := &silentBody{ctx: req.Context(), closed: make(chan struct{})}
	body := struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(append(bookmark, '\n')), silent), silent}
	return jsonResponse(req, http.StatusOK, body, -1)
}

// silentBody delivers nothing until it is closed or its request's context ends. Close must end
// a Read: kubesync stops a watcher on rotation without cancelling the worker's context.
type silentBody struct {
	ctx     context.Context
	closed  chan struct{}
	once    sync.Once
	onClose func()
}

func (b *silentBody) Read([]byte) (int, error) {
	select {
	case <-b.closed:
	case <-b.ctx.Done():
	}
	return 0, io.EOF
}

func (b *silentBody) Close() error {
	b.once.Do(func() {
		close(b.closed)
		if b.onClose != nil {
			b.onClose()
		}
	})
	return nil
}

// hang answers nothing until req's context ends: a probe's deadline, or the idle bound's
// cancel on a list.
func hang(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// statusResponse is what an API server refusing req with code sends: a Status of the same code,
// with the reason client-go reads it by. No Retry-After, so client-go never retries it on its own.
func statusResponse(req *http.Request, code int) *http.Response {
	body, _ := json.Marshal(metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure,
		Message:  "injected by KUBETAIL_FAULTS",
		Reason:   statusReasons[code],
		Code:     int32(code),
	})
	return jsonResponse(req, code, io.NopCloser(bytes.NewReader(body)), int64(len(body)))
}

// jsonResponse is a response to req as the API server sends one.
func jsonResponse(req *http.Request, code int, body io.ReadCloser, length int64) *http.Response {
	return &http.Response{
		Status:        strconv.Itoa(code) + " " + http.StatusText(code),
		StatusCode:    code,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          body,
		ContentLength: length,
		Request:       req,
	}
}

// statusReasons is the reason an API server gives with each code a rule can answer with.
var statusReasons = map[int]metav1.StatusReason{
	http.StatusUnauthorized:        metav1.StatusReasonUnauthorized,
	http.StatusForbidden:           metav1.StatusReasonForbidden,
	http.StatusNotFound:            metav1.StatusReasonNotFound,
	http.StatusTooManyRequests:     metav1.StatusReasonTooManyRequests,
	http.StatusInternalServerError: metav1.StatusReasonInternalError,
	http.StatusServiceUnavailable:  metav1.StatusReasonServiceUnavailable,
}

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

// Package browser is the browser half of an OAuth sign-in: the system browser
// opened on a URL the sidecar built, and the 127.0.0.1 listener its redirect
// lands on. It knows no issuer.
package browser

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/amorey/gochan/oneshot"
)

// Command is the per-OS opener for rawURL. Split from the exec so the mapping
// can be checked for every platform from any one of them.
func Command(goos, rawURL string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{rawURL}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	default: // linux, *bsd, etc.
		return "xdg-open", []string{rawURL}
	}
}

// Open opens rawURL in the default browser. The sidecar inherits the host's GUI
// session env, so the per-OS opener behaves as it would from the host.
func Open(rawURL string) error { // coverage:ignore it opens the real browser, which no test may do
	name, args := Command(runtime.GOOS, rawURL)
	return exec.Command(name, args...).Start()
}

// AuthorizationError is a redirect that declined the sign-in: the issuer's
// error code, such as access_denied.
type AuthorizationError struct {
	Code string
}

func (e *AuthorizationError) Error() string {
	return "oauth: authorization failed: " + e.Code
}

// Listener is the 127.0.0.1 listener catching the redirect. Bind it before
// building the authorize URL, so the redirect_uri matches.
type Listener struct {
	ln    net.Listener
	srv   *http.Server
	path  string
	state string // the redirect must echo this back

	// The first callback or failure to arrive is the flow's answer; a later one
	// finds the pair spent and is dropped.
	answer *oneshot.Sender[callback]
	wait   *oneshot.Receiver[callback]
}

// callback is what reached the listener: the redirect's query, or why none will.
type callback struct {
	query url.Values
	err   error
}

const (
	readTimeout = 10 * time.Second
	idleTimeout = 30 * time.Second
)

// Listen binds an ephemeral port and serves path. state must match what the
// caller put on the authorize URL, so a stray local request can't satisfy the
// one-shot callback.
func Listen(state, path string) (*Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { // coverage:ignore binding an ephemeral loopback port does not fail
		return nil, err
	}
	answer, wait := oneshot.New[callback]()
	l := &Listener{ln: ln, path: path, state: state, answer: answer, wait: wait}
	mux := http.NewServeMux()
	mux.HandleFunc(path, l.handleCallback)
	// The port is reachable by any local process, so a stalled peer must lose its
	// slot rather than hold one for the sign-in's lifetime. The browser's redirect
	// is a single small GET; these are generous for it and short against the flow.
	l.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
	go func() {
		if err := l.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			l.fail(err)
		}
	}()
	return l, nil
}

// callbackHTML is the page the browser lands on after a successful redirect.
const callbackHTML = "<!doctype html>" +
	"<html lang=en>" +
	"<head>" +
	"<meta charset=utf-8>" +
	"<title>kubetail — signed in</title>" +
	"<style>body{font:16px system-ui,sans-serif;text-align:center;padding:4em;color:#222}h1{font-weight:600;margin:0 0 .25em}p{color:#666}</style>" +
	"</head>" +
	"<body>" +
	"<h1>Signed in</h1>" +
	"<p>You can close this tab and return to the app.</p>" +
	"</body>" +
	"</html>"

func (l *Listener) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// State first, before consuming an error or a code: a stray local request must
	// not be able to abort (via ?error=) or consume the one-shot flow.
	if q.Get("state") != l.state {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if e := q.Get("error"); e != "" {
		l.fail(&AuthorizationError{Code: e})
		http.Error(w, e, http.StatusBadRequest)
		return
	}
	if q.Get("code") == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	_ = l.answer.Send(callback{query: q})
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(callbackHTML))
}

// fail answers the flow with err unless something answered it first.
func (l *Listener) fail(err error) {
	_ = l.answer.Send(callback{err: err})
}

// RedirectURL is the redirect_uri to register with the authorize request.
func (l *Listener) RedirectURL() string {
	return (&url.URL{Scheme: "http", Host: l.ln.Addr().String(), Path: l.path}).String()
}

// Wait blocks until the first callback or failure, or until ctx is cancelled,
// and answers the callback's whole query, code included. It answers once.
func (l *Listener) Wait(ctx context.Context) (url.Values, error) {
	c, err := l.wait.RecvContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.query, c.err
}

// Close shuts down the listener.
func (l *Listener) Close() error {
	return l.srv.Close()
}

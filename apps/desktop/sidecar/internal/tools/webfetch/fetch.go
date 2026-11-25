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

package webfetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// accept lets a server that can send markdown send it.
const accept = "text/markdown, text/html;q=0.9, text/plain;q=0.8, */*;q=0.1"

// cancelled answers a call whose context ended; the loop writes its own refusal
// in its place.
const cancelled = `{"error":"cancelled"}`

const errTooLarge refusal = "The page is larger than 8 MiB, which WebFetch does not read."

// fetch requests u and answers with the page, or why there is none.
func (t *Tool) fetch(ctx context.Context, rt tools.Runtime, u *url.URL) (string, bool) {
	fetchCtx, cancel := context.WithTimeout(ctx, t.bound)
	defer cancel()
	resp, err := t.follow(fetchCtx, u)
	if err != nil {
		return failure(ctx, err), true
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "The server answered " + status(resp.StatusCode) + ". The body was not read.", true
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, tools.FileLimit+1))
	if err != nil {
		return failure(ctx, err), true
	}
	if len(body) > tools.FileLimit {
		return string(errTooLarge), true
	}
	p, err := t.convertWithin(ctx, resp.Header.Get("Content-Type"), body, resp.Request.URL)
	var r refusal
	switch {
	case ctx.Err() != nil:
		return cancelled, true
	case errors.As(err, &r):
		return string(r), true
	case err != nil:
		return "WebFetch could not read the page.", true
	}
	header := "Fetched " + capURL(resp.Request.URL.String()) + " (" + p.mediaType + ", " + tools.FormatSize(len(body)) + ")\n"
	if p.title != "" {
		header += "Title: " + p.title + "\n"
	}
	header += "\n"
	return tools.Fit(tools.SaveTo(rt.Dir), header, p.text, "", 0, "page"), false
}

// convertWithin runs convert where the call can leave it: the loop waits for
// Run to return, and neither html.Parse nor the converter reads a context. On
// the context's end it answers at once, and the goroutine finishes on its own
// with what it returns dropped.
func (t *Tool) convertWithin(ctx context.Context, contentType string, body []byte, base *url.URL) (page, error) {
	type result struct {
		p   page
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := t.convert(contentType, body, base)
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		return r.p, r.err
	case <-ctx.Done():
		return page{}, ctx.Err()
	}
}

// get is one request of u, with no credential of any kind.
func (t *Tool) get(ctx context.Context, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil { // coverage:ignore u is a parsed http(s) URL, whose String parses again
		return nil, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", accept)
	return t.transport.RoundTrip(req)
}

// failure is what the model reads for a fetch that ended in err: a refusal in
// its own words, else the kind of failure and never the error's text, which
// can carry what the server sent.
func failure(ctx context.Context, err error) string {
	var (
		r       refusal
		dns     *net.DNSError
		netErr  net.Error
		cert    *tls.CertificateVerificationError
		alert   tls.AlertError
		record  tls.RecordHeaderError
		unknown x509.UnknownAuthorityError
		host    x509.HostnameError
	)
	switch {
	case ctx.Err() != nil:
		return cancelled
	case errors.As(err, &r):
		return string(r)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return unreached("timeout")
	case errors.As(err, &dns):
		return unreached("dns")
	case errors.As(err, &cert), errors.As(err, &alert), errors.As(err, &record),
		errors.As(err, &unknown), errors.As(err, &host):
		return unreached("tls")
	}
	return unreached("connection")
}

func unreached(kind string) string { return "WebFetch could not reach the page: " + kind }

// status is a status as a result spells it: the code and Go's text for it,
// never the server's reason phrase.
func status(code int) string {
	return "HTTP " + strconv.Itoa(code) + " " + http.StatusText(code)
}

// capURL is a server-supplied URL cut to maxURL bytes, past which target would
// refuse it anyway.
func capURL(s string) string { return capped(s, maxURL) }

// capped is s cut to n bytes on a rune boundary, ended with … when it was
// longer.
func capped(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:tools.RuneBoundary(s, n)] + "…"
}

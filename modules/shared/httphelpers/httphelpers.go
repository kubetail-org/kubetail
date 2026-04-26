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

// Package httphelpers provides shared utilities for working with net/http.
package httphelpers

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// IsSameOrigin reports whether r's Origin header is present and matches
// the request's scheme and host (per the WebSocket spec's same-origin
// rule). A missing Origin returns false.
//
// Host comparison is case-insensitive and normalizes the effective port,
// so https://example.com matches a request Host of example.com:443 (and
// likewise http://example.com matches example.com:80). This avoids
// false rejections from ingress/reverse proxies that forward an explicit
// default port in Host.
//
// Intended for use on WebSocket upgrade requests, where browsers always
// send Origin (so its absence indicates a non-browser client, which has
// no ambient browser credentials to abuse). Do not use on plain HTTP
// requests: browsers omit Origin on same-origin safe-method fetches.
func IsSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := requestScheme(r)
	if !strings.EqualFold(u.Scheme, scheme) {
		return false
	}
	originHost, originPort := splitHostPort(u.Host, u.Scheme)
	reqHost, reqPort := splitHostPort(r.Host, scheme)
	return strings.EqualFold(originHost, reqHost) && originPort == reqPort
}

// splitHostPort returns the host and effective port for a host[:port]
// string, falling back to scheme's default port when no port is present.
// IPv6 brackets are stripped so bracketed and unbracketed literals compare
// equal.
func splitHostPort(host, scheme string) (string, string) {
	if h, p, err := net.SplitHostPort(host); err == nil {
		return h, p
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return host, defaultPort(scheme)
}

// defaultPort returns the well-known port for an HTTP-family scheme.
func defaultPort(scheme string) string {
	switch strings.ToLower(scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// requestScheme returns the scheme the client used to reach r, accounting
// for TLS terminated at this process (r.TLS) or upstream (X-Forwarded-Proto
// set by a trusted reverse proxy). Defaults to "http". Browsers cannot
// override X-Forwarded-Proto on a WebSocket upgrade, so trusting it here
// is safe for this gate.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		if i := strings.IndexByte(p, ','); i >= 0 {
			p = p[:i]
		}
		return strings.ToLower(strings.TrimSpace(p))
	}
	return "http"
}

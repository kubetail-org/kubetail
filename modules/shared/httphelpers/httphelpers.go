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

// HeaderForwardedCSRFToken is the request header used to forward a
// dashboard-session-bound CSRF token from the dashboard reverse proxy to
// the cluster-api server, where it gates the GraphQL WebSocket InitFunc.
const HeaderForwardedCSRFToken = "X-Forwarded-CSRF-Token"

// IsSameOrigin reports whether r's Origin header is present and matches
// the request's scheme and host. A missing Origin returns false.
//
// The comparison uses r.Host and r.TLS only. X-Forwarded-Host,
// X-Forwarded-Proto, and the RFC 7239 Forwarded header are deliberately
// ignored: they are attacker-controllable in direct-access deployments
// and can be smuggled through proxies that append rather than overwrite
// client-supplied values. Deployments behind a reverse proxy that rewrites
// Host or terminates TLS without preserving scheme will need explicit
// origin allowlisting (see the upcoming allowed-origins config).
//
// Host comparison is case-insensitive and normalizes the effective port,
// so https://example.com matches a request Host of example.com:443 (and
// likewise http://example.com matches example.com:80). This avoids
// false rejections from clients/servers that emit an explicit default port.
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
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !strings.EqualFold(u.Scheme, scheme) {
		return false
	}
	originHost, originPort := splitHostPort(u.Host, u.Scheme)
	reqHost, reqPort := splitHostPort(r.Host, scheme)
	return strings.EqualFold(originHost, reqHost) && originPort == reqPort
}

// IsAllowedOrigin reports whether r passes IsSameOrigin OR its Origin
// matches one of allowedOrigins. Each allowedOrigins entry is a fully-
// qualified origin (scheme://host[:port]); comparison normalizes case
// and default ports the same way as IsSameOrigin.
//
// Use this for deployments where a reverse proxy rewrites Host or
// terminates TLS without preserving the scheme — situations in which
// IsSameOrigin alone would reject legitimate requests because r.Host
// and r.TLS no longer reflect what the browser sent. Operators
// enumerate the public-facing origin(s) the dashboard is served at.
//
// An empty allowedOrigins slice is equivalent to IsSameOrigin.
func IsAllowedOrigin(r *http.Request, allowedOrigins []string) bool {
	if IsSameOrigin(r) {
		return true
	}
	if len(allowedOrigins) == 0 {
		return false
	}
	raw := r.Header.Get("Origin")
	if raw == "" {
		return false
	}
	got, err := url.Parse(raw)
	if err != nil || got.Host == "" {
		return false
	}
	gotHost, gotPort := splitHostPort(got.Host, got.Scheme)
	for _, allowed := range allowedOrigins {
		u, err := url.Parse(allowed)
		if err != nil || u.Host == "" {
			continue
		}
		if !strings.EqualFold(u.Scheme, got.Scheme) {
			continue
		}
		aHost, aPort := splitHostPort(u.Host, u.Scheme)
		if strings.EqualFold(aHost, gotHost) && aPort == gotPort {
			return true
		}
	}
	return false
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

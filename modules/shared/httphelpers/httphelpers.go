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
	"net/http"
	"net/url"
	"strings"
)

// IsSameOrigin reports whether r's Origin header is present and matches
// the request's scheme and host (per the WebSocket spec's same-origin
// rule). A missing Origin returns false.
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
	if err != nil {
		return false
	}
	return u.Host == r.Host && u.Scheme == requestScheme(r)
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

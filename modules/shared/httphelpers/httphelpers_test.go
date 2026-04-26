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

package httphelpers

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		tls    bool
		xfp    string
		want   bool
	}{
		{"missing Origin is rejected", "example.com", "", false, "", false},
		{"http same-origin matches", "example.com", "http://example.com", false, "", true},
		{"https same-origin via TLS termination", "example.com", "https://example.com", true, "", true},
		{"https same-origin via X-Forwarded-Proto", "example.com", "https://example.com", false, "https", true},
		{"https Origin on plain http request is rejected", "example.com", "https://example.com", false, "", false},
		{"http Origin on https request is rejected", "example.com", "http://example.com", true, "", false},
		{"http Origin on https-by-proxy request is rejected", "example.com", "http://example.com", false, "https", false},
		{"same-host same-port matches", "example.com:8080", "http://example.com:8080", false, "", true},
		{"different host is rejected", "example.com", "http://evil.example.com", false, "", false},
		{"different port is rejected", "example.com:8080", "http://example.com:9090", false, "", false},
		{"malformed Origin is rejected", "example.com", "://not a url", false, "", false},
		{"X-Forwarded-Proto list takes first value", "example.com", "https://example.com", false, "https, http", true},
		{"https default port in Host matches Origin without port", "example.com:443", "https://example.com", true, "", true},
		{"http default port in Host matches Origin without port", "example.com:80", "http://example.com", false, "", true},
		{"https default port in Origin matches Host without port", "example.com", "https://example.com:443", true, "", true},
		{"http default port in Origin matches Host without port", "example.com", "http://example.com:80", false, "", true},
		{"default port via X-Forwarded-Proto matches", "example.com:443", "https://example.com", false, "https", true},
		{"non-default port mismatch is rejected even with default normalization", "example.com:8443", "https://example.com", true, "", false},
		{"host comparison is case-insensitive", "Example.COM", "http://example.com", false, "", true},
		{"Origin without host is rejected", "example.com", "http://", false, "", false},
		{"IPv6 same host matches", "[::1]:8080", "http://[::1]:8080", false, "", true},
		{"IPv6 default port matches", "[::1]:80", "http://[::1]", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				Host:   tt.host,
				Header: http.Header{},
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if tt.xfp != "" {
				r.Header.Set("X-Forwarded-Proto", tt.xfp)
			}
			assert.Equal(t, tt.want, IsSameOrigin(r))
		})
	}
}

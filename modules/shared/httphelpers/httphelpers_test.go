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
		name    string
		host    string
		headers http.Header
		tls     bool
		want    bool
	}{
		{
			name:    "missing Origin is rejected",
			host:    "example.com",
			headers: http.Header{},
			want:    false,
		},
		{
			name: "http same-origin matches",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			want: true,
		},
		{
			name: "https same-origin via TLS termination",
			host: "example.com",
			headers: http.Header{
				"Origin": {"https://example.com"},
			},
			tls:  true,
			want: true,
		},
		{
			name: "https same-origin via X-Forwarded-Proto",
			host: "example.com",
			headers: http.Header{
				"Origin":            {"https://example.com"},
				"X-Forwarded-Proto": {"https"},
			},
			want: true,
		},
		{
			name: "https Origin on plain http request is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"https://example.com"},
			},
			want: false,
		},
		{
			name: "http Origin on https request is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			tls:  true,
			want: false,
		},
		{
			name: "http Origin on https-by-proxy request is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin":            {"http://example.com"},
				"X-Forwarded-Proto": {"https"},
			},
			want: false,
		},
		{
			name: "same-host same-port matches",
			host: "example.com:8080",
			headers: http.Header{
				"Origin": {"http://example.com:8080"},
			},
			want: true,
		},
		{
			name: "different host is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://evil.example.com"},
			},
			want: false,
		},
		{
			name: "different port is rejected",
			host: "example.com:8080",
			headers: http.Header{
				"Origin": {"http://example.com:9090"},
			},
			want: false,
		},
		{
			name: "malformed Origin is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"://not a url"},
			},
			want: false,
		},
		{
			name: "X-Forwarded-Proto list takes first value",
			host: "example.com",
			headers: http.Header{
				"Origin":            {"https://example.com"},
				"X-Forwarded-Proto": {"https, http"},
			},
			want: true,
		},
		{
			name: "https default port in Host matches Origin without port",
			host: "example.com:443",
			headers: http.Header{
				"Origin": {"https://example.com"},
			},
			tls:  true,
			want: true,
		},
		{
			name: "http default port in Host matches Origin without port",
			host: "example.com:80",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			want: true,
		},
		{
			name: "https default port in Origin matches Host without port",
			host: "example.com",
			headers: http.Header{
				"Origin": {"https://example.com:443"},
			},
			tls:  true,
			want: true,
		},
		{
			name: "http default port in Origin matches Host without port",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://example.com:80"},
			},
			want: true,
		},
		{
			name: "default port via X-Forwarded-Proto matches",
			host: "example.com:443",
			headers: http.Header{
				"Origin":            {"https://example.com"},
				"X-Forwarded-Proto": {"https"},
			},
			want: true,
		},
		{
			name: "non-default port mismatch is rejected even with default normalization",
			host: "example.com:8443",
			headers: http.Header{
				"Origin": {"https://example.com"},
			},
			tls:  true,
			want: false,
		},
		{
			name: "host comparison is case-insensitive",
			host: "Example.COM",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			want: true,
		},
		{
			name: "Origin without host is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://"},
			},
			want: false,
		},
		{
			name: "IPv6 same host matches",
			host: "[::1]:8080",
			headers: http.Header{
				"Origin": {"http://[::1]:8080"},
			},
			want: true,
		},
		{
			name: "IPv6 default port matches",
			host: "[::1]:80",
			headers: http.Header{
				"Origin": {"http://[::1]"},
			},
			want: true,
		},
		// X-Forwarded-Host: proxy sets the public host, internal r.Host differs
		{
			name: "X-Forwarded-Host same-origin matches",
			host: "internal:8080",
			headers: http.Header{
				"Origin":           {"http://example.com"},
				"X-Forwarded-Host": {"example.com"},
			},
			want: true,
		},
		{
			name: "X-Forwarded-Host different host is rejected",
			host: "internal:8080",
			headers: http.Header{
				"Origin":           {"http://evil.com"},
				"X-Forwarded-Host": {"example.com"},
			},
			want: false,
		},
		{
			name: "X-Forwarded-Host with port matches",
			host: "internal:8080",
			headers: http.Header{
				"Origin":           {"http://example.com:9090"},
				"X-Forwarded-Host": {"example.com:9090"},
			},
			want: true,
		},
		{
			name: "X-Forwarded-Host list takes first value",
			host: "internal:8080",
			headers: http.Header{
				"Origin":           {"http://example.com"},
				"X-Forwarded-Host": {"example.com, other.com"},
			},
			want: true,
		},
		{
			name: "X-Forwarded-Host with X-Forwarded-Proto matches https",
			host: "internal:8080",
			headers: http.Header{
				"Origin":            {"https://example.com"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"example.com"},
			},
			want: true,
		},
		// Forwarded (RFC 7239)
		{
			name: "Forwarded host matches",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"http://example.com"},
				"Forwarded": {"host=example.com"},
			},
			want: true,
		},
		{
			name: "Forwarded host different is rejected",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"http://evil.com"},
				"Forwarded": {"host=example.com"},
			},
			want: false,
		},
		{
			name: "Forwarded host with other directives",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"http://example.com"},
				"Forwarded": {"for=1.2.3.4;host=example.com;proto=http"},
			},
			want: true,
		},
		{
			name: "Forwarded proto matches https",
			host: "example.com",
			headers: http.Header{
				"Origin":    {"https://example.com"},
				"Forwarded": {"proto=https"},
			},
			want: true,
		},
		{
			name: "Forwarded quoted proto matches https",
			host: "example.com",
			headers: http.Header{
				"Origin":    {"https://example.com"},
				"Forwarded": {`proto="https"`},
			},
			want: true,
		},
		{
			name: "Forwarded proto and host match https behind proxy",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"https://example.com"},
				"Forwarded": {"for=1.2.3.4;host=example.com;proto=https"},
			},
			want: true,
		},
		{
			name: "Forwarded proto takes first element",
			host: "example.com",
			headers: http.Header{
				"Origin":    {"https://example.com"},
				"Forwarded": {"proto=https, proto=http"},
			},
			want: true,
		},
		{
			name: "Forwarded host takes first element",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"http://example.com"},
				"Forwarded": {"host=example.com, host=other.com"},
			},
			want: true,
		},
		{
			name: "Forwarded quoted host matches",
			host: "internal:8080",
			headers: http.Header{
				"Origin":    {"http://example.com"},
				"Forwarded": {`host="example.com"`},
			},
			want: true,
		},
		// X-Forwarded-Host takes precedence over Forwarded
		{
			name: "X-Forwarded-Host takes precedence over Forwarded",
			host: "internal:8080",
			headers: http.Header{
				"Origin":           {"http://example.com"},
				"X-Forwarded-Host": {"example.com"},
				"Forwarded":        {"host=other.com"},
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto takes precedence over Forwarded proto",
			host: "example.com",
			headers: http.Header{
				"Origin":            {"https://example.com"},
				"X-Forwarded-Proto": {"https"},
				"Forwarded":         {"proto=http"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				Host:   tt.host,
				Header: tt.headers,
			}
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			assert.Equal(t, tt.want, IsSameOrigin(r))
		})
	}
}

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
		// Forwarded headers must NOT be honored: they're attacker-controllable
		// in direct-access deployments and through non-sanitizing proxies.
		{
			name: "forged X-Forwarded-Host with matching Origin is rejected",
			host: "kubetail.local",
			headers: http.Header{
				"Origin":            {"https://attacker.com"},
				"X-Forwarded-Host":  {"attacker.com"},
				"X-Forwarded-Proto": {"https"},
			},
			want: false,
		},
		{
			name: "forged Forwarded header with matching Origin is rejected",
			host: "kubetail.local",
			headers: http.Header{
				"Origin":    {"https://attacker.com"},
				"Forwarded": {"host=attacker.com;proto=https"},
			},
			want: false,
		},
		{
			name: "X-Forwarded-Proto cannot upgrade scheme on plaintext request",
			host: "kubetail.local",
			headers: http.Header{
				"Origin":            {"https://kubetail.local"},
				"X-Forwarded-Proto": {"https"},
			},
			want: false,
		},
		{
			name: "X-Forwarded-Host is ignored when Origin matches r.Host",
			host: "kubetail.local",
			headers: http.Header{
				"Origin":           {"http://kubetail.local"},
				"X-Forwarded-Host": {"attacker.com"},
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

func TestIsAllowedOrigin(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		headers        http.Header
		tls            bool
		allowedOrigins []string
		want           bool
	}{
		{
			name: "empty allowlist + same-origin request is allowed",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			allowedOrigins: nil,
			want:           true,
		},
		{
			name: "empty allowlist + cross-origin request is rejected",
			host: "example.com",
			headers: http.Header{
				"Origin": {"https://attacker.com"},
			},
			allowedOrigins: nil,
			want:           false,
		},
		{
			name: "allowlist match across hostname rewrite",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://kubetail.example.com"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           true,
		},
		{
			name: "allowlist scheme-mismatch is rejected",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"http://kubetail.example.com"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           false,
		},
		{
			name: "allowlist host-mismatch is rejected",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://attacker.example.com"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           false,
		},
		{
			name: "allowlist port-mismatch is rejected",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://kubetail.example.com:9090"},
			},
			allowedOrigins: []string{"https://kubetail.example.com:8443"},
			want:           false,
		},
		{
			name: "allowlist host compare is case-insensitive",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://KubeTail.Example.COM"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           true,
		},
		{
			name: "allowlist default-port: entry without port matches Origin with default port",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://kubetail.example.com:443"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           true,
		},
		{
			name: "allowlist default-port: entry with default port matches Origin without port",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://kubetail.example.com"},
			},
			allowedOrigins: []string{"https://kubetail.example.com:443"},
			want:           true,
		},
		{
			name: "allowlist IPv6 brackets normalize",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"http://[::1]:8080"},
			},
			allowedOrigins: []string{"http://[::1]:8080"},
			want:           true,
		},
		{
			name: "malformed allowlist entry is skipped, later entry still matches",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"https://kubetail.example.com"},
			},
			allowedOrigins: []string{"::not a url", "https://kubetail.example.com"},
			want:           true,
		},
		{
			name:           "missing Origin with non-empty allowlist is rejected",
			host:           "kubetail.svc.cluster.local",
			headers:        http.Header{},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           false,
		},
		{
			name: "malformed Origin with non-empty allowlist is rejected",
			host: "kubetail.svc.cluster.local",
			headers: http.Header{
				"Origin": {"://not a url"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           false,
		},
		{
			name: "same-origin still wins when allowlist is non-empty and unrelated",
			host: "example.com",
			headers: http.Header{
				"Origin": {"http://example.com"},
			},
			allowedOrigins: []string{"https://kubetail.example.com"},
			want:           true,
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
			assert.Equal(t, tt.want, IsAllowedOrigin(r, tt.allowedOrigins))
		})
	}
}

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

package testutil

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
)

// DialAnywhere is a transport that dials srv, a TLS test server, whatever
// host a request names, trusting srv's certificate: a proxy under test can
// keep its real upstream host and still reach the fake.
func DialAnywhere(srv *httptest.Server) http.RoundTripper {
	tlsConfig := srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsConfig.ServerName = "example.com" // the name httptest's certificate holds
	addr := srv.Listener.Addr().String()
	return &http.Transport{
		TLSClientConfig: tlsConfig,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

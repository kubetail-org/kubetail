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

package app

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/cluster-api/pkg/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// Create new base config for testing
func NewTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.BasePath = "/"
	cfg.Logging.AccessLog.Enabled = false
	return cfg
}

// NewTestApp builds an app with no connection manager and no gRPC dispatcher,
// whose aggregated routes trust front-proxy certificates the returned CA
// issues.
func NewTestApp(t *testing.T, cfg *config.Config) (*App, *testCA) {
	t.Helper()
	if cfg == nil {
		cfg = NewTestConfig()
	}

	proxyCA := newTestCA(t, "proxy-ca")
	app, err := newApp(cfg, nil, nil, newTestAuthCfg(proxyCA, "front-proxy-client"))
	require.NoError(t, err)

	return app, proxyCA
}

// asFrontProxy makes r look as if the kube-apiserver forwarded it for user,
// with a front-proxy certificate proxyCA issued.
func asFrontProxy(t *testing.T, r *http.Request, proxyCA *testCA, user string) {
	t.Helper()
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{proxyCA.issue(t, "front-proxy-client")}}
	r.Header.Set("X-Remote-User", user)
}

// Cookie helper method
func GetCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

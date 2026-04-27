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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

// POST without a CSRF token is blocked at the dynamic-route gate before
// reaching the proxy.
func TestClusterAPIProxyPOSTRequiresCSRFToken(t *testing.T) {
	app := newTestApp(nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/cluster-api-proxy/some/path", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	app.ServeHTTP(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// POST in token mode without a session token is rejected by
// k8sAuthenticationMiddleware on the protected-route group.
func TestClusterAPIProxyPOSTRequiresAuthInTokenMode(t *testing.T) {
	cfg := newTestConfig()
	cfg.AuthMode = config.AuthModeToken
	app := newTestApp(cfg)
	client := testutils.NewWebTestClient(t, app)
	defer client.Teardown()

	// Prime the CSRF token so the POST reaches the auth check.
	client.Get("/api/auth/session")

	req := client.NewRequest("POST", "/cluster-api-proxy/some/path", nil)
	resp := client.Do(req)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

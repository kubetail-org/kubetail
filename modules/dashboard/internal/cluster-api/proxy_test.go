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

package clusterapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInClusterProxy_XForwardedAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		userToken  string
		wantHeader string
	}{
		{
			name:       "forwards user token as X-Forwarded-Authorization",
			userToken:  "user-token-123",
			wantHeader: "Bearer user-token-123",
		},
		{
			name:       "no X-Forwarded-Authorization without user token",
			userToken:  "",
			wantHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedHeader string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHeader = r.Header.Get("X-Forwarded-Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			proxy, err := newInClusterProxy(backend.URL, "/prefix", http.DefaultTransport)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/prefix/somepath", nil)
			if tt.userToken != "" {
				ctx := context.WithValue(req.Context(), k8shelpers.K8STokenCtxKey, tt.userToken)
				req = req.WithContext(ctx)
			}

			proxy.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.wantHeader, capturedHeader)
		})
	}
}

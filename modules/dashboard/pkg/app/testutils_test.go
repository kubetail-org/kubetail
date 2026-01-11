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
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	authv1 "k8s.io/api/authentication/v1"

	"github.com/kubetail-org/kubetail/modules/shared/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// Create new base config for testing
func newTestConfig() *config.Config {
	cfg := config.Config{}
	cfg.Dashboard.BasePath = "/"
	cfg.Dashboard.Environment = config.EnvironmentCluster
	cfg.Dashboard.Logging.AccessLog.Enabled = false
	cfg.Dashboard.Session.Secret = "TESTSESSIONSECRET"
	cfg.Dashboard.Session.Cookie.Name = "session"
	cfg.Dashboard.CSRF.Enabled = false
	return &cfg
}

// Create new app for testing
func newTestApp(cfg *config.Config) *App {
	if cfg == nil {
		cfg = newTestConfig()
	}

	app, err := NewApp(cfg)
	if err != nil {
		panic(err)
	}

	return app
}

// Cookie helper method
func getCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// Represents mock for queryHelpers
type mockQueryHelpers struct {
	mock.Mock
}

// HasAccess
func (m *mockQueryHelpers) HasAccess(ctx context.Context, token string) (*authv1.TokenReview, error) {
	ret := m.Called(ctx, token)

	var r0 *authv1.TokenReview
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*authv1.TokenReview)
	}

	return r0, ret.Error(1)
}

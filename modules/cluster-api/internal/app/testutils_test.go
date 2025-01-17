// Copyright 2024-2025 Andres Morey
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

	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/shared/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// Create new base config for testing
func NewTestConfig() *config.Config {
	cfg := config.Config{}
	cfg.ClusterAPI.BasePath = "/"
	cfg.ClusterAPI.Logging.AccessLog.Enabled = false
	cfg.ClusterAPI.CSRF.Enabled = false
	cfg.ClusterAPI.CSRF.Secret = "TESTCSRFSECRET"
	return &cfg
}

// Create new app for testing
func NewTestApp(cfg *config.Config) *App {
	if cfg == nil {
		cfg = NewTestConfig()
	}

	app, err := NewApp(cfg)
	if err != nil {
		panic(err)
	}

	return app
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

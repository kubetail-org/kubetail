// Copyright 2024 Andres Morey
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

package ginapp

import (
	"net/http"

	"github.com/gorilla/csrf"
)

type Config struct {
	// Auth mode
	AuthMode AuthMode

	// Kube config
	KubeConfig string

	// namespace filter
	Namespace string

	// access log options
	AccessLog struct {
		Enabled          bool
		HideHealthChecks bool
	}

	// session options
	Session struct {
		Secret string

		// cookie options
		Cookie struct {
			Name     string
			Path     string
			Domain   string
			MaxAge   int
			Secure   bool
			HttpOnly bool
			SameSite http.SameSite
		}
	}

	// csrf protection options
	CSRF struct {
		Enabled   bool
		Secret    string
		FieldName string

		// cookie options
		Cookie struct {
			Name     string
			Path     string
			Domain   string
			MaxAge   int
			Secure   bool
			HttpOnly bool
			SameSite csrf.SameSiteMode
		}
	}
}

func DefaultConfig() Config {
	cfg := Config{}

	cfg.AuthMode = AuthModeToken
	cfg.AccessLog.Enabled = true
	cfg.AccessLog.HideHealthChecks = false

	cfg.Session.Secret = ""
	cfg.Session.Cookie.Name = "session"
	cfg.Session.Cookie.Path = "/"
	cfg.Session.Cookie.Domain = ""
	cfg.Session.Cookie.MaxAge = 36400 * 30
	cfg.Session.Cookie.Secure = false
	cfg.Session.Cookie.HttpOnly = true
	cfg.Session.Cookie.SameSite = http.SameSiteLaxMode

	cfg.CSRF.Enabled = true
	cfg.CSRF.Secret = ""
	cfg.CSRF.FieldName = "csrf_token"
	cfg.CSRF.Cookie.Name = "csrf"
	cfg.CSRF.Cookie.Path = "/"
	cfg.CSRF.Cookie.Domain = ""
	cfg.CSRF.Cookie.MaxAge = 60 * 60 * 12 // 12 hours
	cfg.CSRF.Cookie.Secure = false
	cfg.CSRF.Cookie.HttpOnly = true
	cfg.CSRF.Cookie.SameSite = csrf.SameSiteStrictMode

	return cfg
}

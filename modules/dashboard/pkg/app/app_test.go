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
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/requestid"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/stretchr/testify/assert"
)

func TestRequestID(t *testing.T) {
	app := newTestApp(nil)

	// add route for testing
	app.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, requestid.Get(c))
	})

	// request 1
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w1, r1)
	id1 := w1.Body.String()

	// request 2
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w2, r2)
	id2 := w2.Body.String()

	// check result
	assert.NotEqual(t, "", id1)
	assert.NotEqual(t, "", id2)
	assert.NotEqual(t, id1, id2)
}

func TestGzip(t *testing.T) {
	app := newTestApp(nil)

	// add route for testing
	app.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// request without compression
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(w1, r1)
	assert.Equal(t, w1.Body.String(), "ok")

	// request with compression
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.Header["Accept-Encoding"] = []string{"gzip"}
	app.ServeHTTP(w2, r2)

	gzreader, err := gzip.NewReader(w2.Body)
	assert.Equal(t, nil, err)
	uncompressed, err := io.ReadAll(gzreader)
	assert.Equal(t, nil, err)
	assert.Equal(t, "ok", string(uncompressed))
}

func TestSessionCookieOptions(t *testing.T) {
	cfg1 := newTestConfig()
	cfg1.Dashboard.Session.Cookie.Path = "/xxx"

	cfg2 := newTestConfig()
	cfg2.Dashboard.Session.Cookie.Domain = "x.example.com"

	cfg3 := newTestConfig()
	cfg3.Dashboard.Session.Cookie.MaxAge = 1

	cfg4 := newTestConfig()
	cfg4.Dashboard.Session.Cookie.Secure = false

	cfg5 := newTestConfig()
	cfg5.Dashboard.Session.Cookie.Secure = true

	cfg6 := newTestConfig()
	cfg6.Dashboard.Session.Cookie.HttpOnly = false

	cfg7 := newTestConfig()
	cfg7.Dashboard.Session.Cookie.HttpOnly = true

	cfg8 := newTestConfig()
	cfg8.Dashboard.Session.Cookie.SameSite = http.SameSiteNoneMode

	tests := []struct {
		name   string
		setCfg *config.Config
	}{
		{"Path", cfg1},
		{"Domain", cfg2},
		{"MaxAge", cfg3},
		{"Secure:false", cfg4},
		{"Secure:true", cfg5},
		{"HttpOnly:false", cfg6},
		{"HttpOnly:true", cfg7},
		{"SameSite", cfg8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(tt.setCfg)

			// add route for testing
			app.dynamicRoutes.GET("/test", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("k", "v")
				err := session.Save()
				assert.Nil(t, err)

				c.String(http.StatusOK, "ok")
			})

			// request
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/test", nil)
			app.ServeHTTP(w, r)

			// check session cookie
			cookie := getCookie(w.Result().Cookies(), "session")
			assert.NotNil(t, cookie)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.Path, cookie.Path)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.Domain, cookie.Domain)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.MaxAge, cookie.MaxAge)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.Secure, cookie.Secure)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.HttpOnly, cookie.HttpOnly)
			assert.Equal(t, tt.setCfg.Dashboard.Session.Cookie.SameSite, cookie.SameSite)
		})
	}
}

func TestHealthz(t *testing.T) {
	app := newTestApp(nil)

	// make request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/healthz", nil)
	app.ServeHTTP(w, r)

	// check response
	result := w.Result()
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "{\"status\":\"ok\"}", w.Body.String())
}

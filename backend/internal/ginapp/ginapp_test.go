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
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/requestid"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/csrf"
	"github.com/kubetail-org/kubetail/graph"
	"github.com/stretchr/testify/assert"
)

func TestRequestID(t *testing.T) {
	app := NewTestApp(nil)

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
	app := NewTestApp(nil)

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
	cfg1 := NewTestConfig()
	cfg1.Session.Cookie.Path = "/xxx"

	cfg2 := NewTestConfig()
	cfg2.Session.Cookie.Domain = "x.example.com"

	cfg3 := NewTestConfig()
	cfg3.Session.Cookie.MaxAge = 1

	cfg4 := NewTestConfig()
	cfg4.Session.Cookie.Secure = false

	cfg5 := NewTestConfig()
	cfg5.Session.Cookie.Secure = true

	cfg6 := NewTestConfig()
	cfg6.Session.Cookie.HttpOnly = false

	cfg7 := NewTestConfig()
	cfg7.Session.Cookie.HttpOnly = true

	cfg8 := NewTestConfig()
	cfg8.Session.Cookie.SameSite = http.SameSiteNoneMode

	tests := []struct {
		name   string
		setCfg *Config
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
			app := NewTestApp(tt.setCfg)

			// add route for testing
			app.dynamicroutes.GET("/test", func(c *gin.Context) {
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
			cookie := GetCookie(w.Result().Cookies(), "session")
			assert.NotNil(t, cookie)
			assert.Equal(t, tt.setCfg.Session.Cookie.Path, cookie.Path)
			assert.Equal(t, tt.setCfg.Session.Cookie.Domain, cookie.Domain)
			assert.Equal(t, tt.setCfg.Session.Cookie.MaxAge, cookie.MaxAge)
			assert.Equal(t, tt.setCfg.Session.Cookie.Secure, cookie.Secure)
			assert.Equal(t, tt.setCfg.Session.Cookie.HttpOnly, cookie.HttpOnly)
			assert.Equal(t, tt.setCfg.Session.Cookie.SameSite, cookie.SameSite)
		})
	}
}

func TestCsrfCookieOptions(t *testing.T) {
	cfg1 := NewTestConfig()
	cfg1.CSRF.Cookie.Path = "/xxx"

	cfg2 := NewTestConfig()
	cfg2.CSRF.Cookie.Domain = "x.example.com"

	cfg3 := NewTestConfig()
	cfg3.CSRF.Cookie.MaxAge = 1

	cfg4 := NewTestConfig()
	cfg4.CSRF.Cookie.Secure = false

	cfg5 := NewTestConfig()
	cfg5.CSRF.Cookie.Secure = true

	cfg6 := NewTestConfig()
	cfg6.CSRF.Cookie.HttpOnly = false

	cfg7 := NewTestConfig()
	cfg7.CSRF.Cookie.HttpOnly = true

	cfg8 := NewTestConfig()
	cfg8.CSRF.Cookie.SameSite = csrf.SameSiteNoneMode

	tests := []struct {
		name   string
		setCfg *Config
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
			tt.setCfg.CSRF.Enabled = true
			tt.setCfg.CSRF.Cookie.Name = "customname"
			app := NewTestApp(tt.setCfg)

			// add route for testing
			app.dynamicroutes.GET("/test", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			// request
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/test", nil)
			app.ServeHTTP(w, r)

			// check session cookie
			cookie := GetCookie(w.Result().Cookies(), tt.setCfg.CSRF.Cookie.Name)
			assert.NotNil(t, cookie)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.Path, cookie.Path)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.Domain, cookie.Domain)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.MaxAge, cookie.MaxAge)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.Secure, cookie.Secure)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.HttpOnly, cookie.HttpOnly)
			assert.Equal(t, tt.setCfg.CSRF.Cookie.SameSite, csrf.SameSiteMode(cookie.SameSite))
		})
	}
}

func TestAuthMiddlewareChain(t *testing.T) {
	tests := []struct {
		name         string
		setAuthMode  AuthMode
		wantHasToken bool
	}{
		{"cluster", AuthModeCluster, false},
		{"local", AuthModeLocal, false},
		{"token", AuthModeToken, true},
	}

	for _, tt := range tests {
		cfg := NewTestConfig()
		cfg.AuthMode = tt.setAuthMode
		app := NewTestApp(cfg)

		// add route for testing
		app.dynamicroutes.GET("/test", func(c *gin.Context) {
			// check gin context
			token1, exists := c.Get(k8sTokenCtxKey)
			assert.Equal(t, tt.wantHasToken, exists)
			if tt.wantHasToken {
				assert.Equal(t, "xxx", token1)
			}

			// check go context
			tokenIF := c.Request.Context().Value(graph.K8STokenCtxKey)
			if tt.wantHasToken {
				assert.NotNil(t, tokenIF)
				token2, ok := tokenIF.(string)
				assert.True(t, ok)
				assert.Equal(t, "xxx", token2)
			} else {
				assert.Nil(t, tokenIF)
			}
		})

		// request
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/test", nil)
		r.Header.Set("Authorization", "Bearer xxx")
		app.ServeHTTP(w, r)
	}
}

func TestHealthz(t *testing.T) {
	app := NewTestApp(nil)

	// make request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/healthz", nil)
	app.ServeHTTP(w, r)

	// check response
	result := w.Result()
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "{\"status\":\"ok\"}", w.Body.String())
}

func TestGraphQLPlayground(t *testing.T) {
	app := NewTestApp(nil)

	// check url
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/graphiql", nil)
	app.ServeHTTP(w, r)

	// check result
	res := w.Result()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestWraponce(t *testing.T) {
	app := NewTestApp(nil)

	var checkVal string

	// request 1
	checkVal = ""
	app.wraponce = func(c *gin.Context) {
		checkVal = "request1"
		c.Next()
	}
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, "request1", checkVal)

	// request 2 (without wrapper)
	checkVal = ""
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, "", checkVal)

	// request 3 (with new wrapper)
	app.wraponce = func(c *gin.Context) {
		checkVal = "request3"
		c.Next()
	}
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, "request3", checkVal)
}

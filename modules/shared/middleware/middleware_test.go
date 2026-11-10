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

package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoggedRouter returns a router whose access log writes JSON lines to buf.
func newLoggedRouter(buf *bytes.Buffer, hideHealthChecks bool, handler gin.HandlerFunc) *gin.Engine {
	logger := zerolog.New(buf)
	r := gin.New()
	r.Use(requestid.New())
	r.Use(loggingMiddleware(&logger, hideHealthChecks))
	r.Any("/*path", handler)
	return r
}

// logLines decodes each JSON line written to buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		lines = append(lines, m)
	}
	return lines
}

func TestLoggingMiddleware_RecordsAccessFields(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf, false, func(c *gin.Context) {
		c.Header("Content-Length", "2")
		c.String(http.StatusCreated, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/things?limit=5&q=x", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Content-Length", "2")
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Referer", "http://example.com/")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", "graphql-transport-ws")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	lines := logLines(t, &buf)
	require.Len(t, lines, 1)
	rec := lines[0]

	assert.Equal(t, "info", rec["level"])
	assert.Equal(t, "Access", rec["event_type"])
	assert.Equal(t, w.Header().Get("X-Request-ID"), rec["request_id"])
	assert.NotEmpty(t, rec["request_id"])
	assert.NotEmpty(t, rec["request_ts"])
	assert.Contains(t, rec, "duration_ms")
	assert.Equal(t, "10.0.0.1:1234", rec["remote_addr"])
	assert.Equal(t, "POST", rec["method"])
	assert.Equal(t, "HTTP/1.1", rec["proto"])
	assert.Equal(t, "http", rec["scheme"])
	assert.Equal(t, "example.com", rec["host"])
	assert.Equal(t, "/api/things", rec["path"])
	assert.Equal(t, "limit=5&q=x", rec["raw_query"])
	assert.Equal(t, "2", rec["content_length"])
	assert.Equal(t, "test-agent", rec["user_agent"])
	assert.Equal(t, "http://example.com/", rec["referer"])
	assert.Equal(t, "1.2.3.4", rec["x_forwarded_for"])
	assert.Equal(t, "proxy.example.com", rec["x_forwarded_host"])
	assert.Equal(t, "https", rec["x_forwarded_proto"])
	assert.Equal(t, "websocket", rec["upgrade"])
	assert.Equal(t, "graphql-transport-ws", rec["sec_websocket_protocol"])
	assert.Equal(t, float64(http.StatusCreated), rec["status_code"])
	assert.Equal(t, "2", rec["resp_content_length"])
}

func TestLoggingMiddleware_PutsRequestLoggerOnContext(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf, false, func(c *gin.Context) {
		zerolog.Ctx(c.Request.Context()).Info().Msg("inside handler")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/x", nil))

	lines := logLines(t, &buf)
	require.Len(t, lines, 2)
	requestID := w.Header().Get("X-Request-ID")
	require.NotEmpty(t, requestID)

	assert.Equal(t, "inside handler", lines[0]["message"])
	assert.Equal(t, requestID, lines[0]["request_id"])
	assert.Equal(t, "Access", lines[1]["event_type"])
	assert.Equal(t, requestID, lines[1]["request_id"])
}

func TestLoggingMiddleware_HideHealthChecks(t *testing.T) {
	tests := []struct {
		name             string
		hideHealthChecks bool
		path             string
		wantLogged       bool
	}{
		{"hidden health check", true, "/healthz", false},
		{"hidden nested health check", true, "/cluster-api-proxy/healthz", false},
		{"shown health check", false, "/healthz", true},
		{"other path with hiding on", true, "/api/x", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handlerRan := false
			r := newLoggedRouter(&buf, tt.hideHealthChecks, func(c *gin.Context) {
				handlerRan = true
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assert.True(t, handlerRan)
			assert.Equal(t, http.StatusOK, w.Code)
			if tt.wantLogged {
				assert.Len(t, logLines(t, &buf), 1)
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestLoggingMiddleware_NeverLogsAuthorizationCookiesOrBody(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf, false, func(c *gin.Context) {
		zerolog.Ctx(c.Request.Context()).Info().Msg("inside handler")
		http.SetCookie(c.Writer, &http.Cookie{Name: "session", Value: "secret-set-cookie"})
		c.String(http.StatusOK, "secret-response-body")
	})

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"token":"secret-request-body"}`))
	req.Header.Set("Authorization", "Bearer secret-bearer-token")
	req.Header.Set("Cookie", "session=secret-cookie")
	req.Header.Set("X-CSRF-Token", "secret-csrf-token")
	r.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	require.NotEmpty(t, out)
	for _, secret := range []string{
		"secret-bearer-token",
		"secret-cookie",
		"secret-set-cookie",
		"secret-csrf-token",
		"secret-request-body",
		"secret-response-body",
	} {
		assert.NotContains(t, out, secret)
	}
	for _, rec := range logLines(t, &buf) {
		for key := range rec {
			assert.NotContains(t, strings.ToLower(key), "auth")
			assert.NotContains(t, strings.ToLower(key), "cookie")
		}
	}
}

func TestLoggingMiddleware_PassesRequestThrough(t *testing.T) {
	// The health check path keeps the global logger silent.
	r := gin.New()
	r.Use(LoggingMiddleware(true))
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestCacheControlMiddleware(t *testing.T) {
	r := gin.New()
	r.Use(CacheControlMiddleware)
	r.GET("/assets/app.js", func(c *gin.Context) { c.String(http.StatusOK, "js") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "js", w.Body.String())
	assert.Equal(t, "public, max-age=31536000, immutable", w.Header().Get("Cache-Control"))
}

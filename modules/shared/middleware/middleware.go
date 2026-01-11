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
	"strings"
	"time"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Log HTTP requests
func LoggingMiddleware(hideHealthChecks bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if hideHealthChecks && strings.HasSuffix(c.Request.URL.Path, "/healthz") {
			c.Next()
			return
		}

		t0 := time.Now().UTC() // for access log request time

		// create contextual sub-logger
		requestId := requestid.Get(c)
		logger := log.With().Str("request_id", requestId).Logger()
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context()))

		// execute request
		c.Next()

		// record `Access` event using contextual logger
		m := logger.Info()
		m.Str("event_type", "Access")
		m.Time("request_ts", t0)
		m.Str("remote_addr", c.Request.RemoteAddr)
		m.Str("method", c.Request.Method)
		m.Str("proto", c.Request.Proto)
		m.Str("scheme", c.Request.URL.Scheme)
		m.Str("host", c.Request.Host)
		m.Str("path", c.Request.URL.Path)
		m.Str("raw_query", c.Request.URL.RawQuery)
		m.Str("content_length", c.Request.Header.Get("Content-Length"))
		m.Str("user_agent", c.Request.Header.Get("User-Agent"))
		m.Str("referer", c.Request.Header.Get("Referer"))
		m.Str("x_forwarded_for", c.Request.Header.Get("X-Forwarded-For"))
		m.Str("x_forwarded_host", c.Request.Header.Get("X-Forwarded-Host"))
		m.Str("x_forwarded_proto", c.Request.Header.Get("X-Forwarded-Proto"))
		m.Str("upgrade", c.Request.Header.Get("Upgrade"))
		m.Str("sec_websocket_protocol", c.Request.Header.Get("Sec-WebSocket-Protocol"))
		m.Int("status_code", c.Writer.Status())
		m.Dur("duration_ms", time.Since(t0))
		m.Str("resp_content_length", c.Writer.Header().Get("Content-Length"))
		m.Send()
	}
}

// Add far-future expires cache headers
func CacheControlMiddleware(c *gin.Context) {
	c.Writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	c.Next()
}

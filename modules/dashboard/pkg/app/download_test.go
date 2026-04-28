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
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

// fakeStreamer emits a pre-populated slice of LogRecords through the Records
// channel. Ignores options; use only for handler-level assertions.
type fakeStreamer struct {
	records []logs.LogRecord
	ch      chan logs.LogRecord
}

func (f *fakeStreamer) Start(ctx context.Context) error {
	go func() {
		defer close(f.ch)
		for _, r := range f.records {
			select {
			case <-ctx.Done():
				return
			case f.ch <- r:
			}
		}
	}()
	return nil
}

func (f *fakeStreamer) Records() <-chan logs.LogRecord { return f.ch }
func (f *fakeStreamer) Err() error                     { return nil }
func (f *fakeStreamer) Close()                         {}

// newTestDownloadHandlers builds a downloadHandlers wired to a fake stream
// returning the given records. The embedded *App is nil — the handler does
// not use it directly, only via h.newLogStream.
func newTestDownloadHandlers(records []logs.LogRecord) *downloadHandlers {
	return &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			return &fakeStreamer{records: records, ch: make(chan logs.LogRecord, len(records))}, nil
		},
	}
}

// mountDownloadHandler returns a minimal gin engine with only the download
// route mounted — no CSRF, auth, or session middleware. Those are covered
// separately in app_test.go.
func mountDownloadHandler(h *downloadHandlers) *gin.Engine {
	r := gin.New()
	r.POST("/api/v1/download", h.DownloadPOST)
	return r
}

func baseDownloadForm() url.Values {
	return url.Values{
		"sources":         {"default:pod/my-pod"},
		"mode":            {"HEAD"},
		"outputFormat":    {"TSV"},
		"messageFormat":   {"TEXT"},
		"includeMetadata": {"true"},
		"columns":         {"timestamp", "message"},
	}
}

func postDownload(engine *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/download", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(w, r)
	return w
}

func TestDownloadForwardsAllowedNamespaces(t *testing.T) {
	var capturedOptCount int
	h := &downloadHandlers{
		allowedNamespaces: []string{"allowed-ns"},
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			capturedOptCount = len(opts)
			return &fakeStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}
	engine := mountDownloadHandler(h)

	w := postDownload(engine, baseDownloadForm())
	assert.Equal(t, http.StatusOK, w.Code)

	// Compare against the same form built without allowedNamespaces — the
	// difference must be exactly one option (WithAllowedNamespaces).
	var baselineOptCount int
	baseline := &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			baselineOptCount = len(opts)
			return &fakeStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}
	w2 := postDownload(mountDownloadHandler(baseline), baseDownloadForm())
	assert.Equal(t, http.StatusOK, w2.Code)

	assert.Equal(t, baselineOptCount+1, capturedOptCount)
}

func TestDownloadFormBindingValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(url.Values)
		wantStatus int
	}{
		{
			name:       "valid form reaches stub",
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty sources",
			mutate:     func(f url.Values) { f.Del("sources") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid mode",
			mutate:     func(f url.Values) { f.Set("mode", "XYZ") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "csv requires metadata",
			mutate: func(f url.Values) {
				f.Set("outputFormat", "CSV")
				f.Set("includeMetadata", "false")
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "text allows metadata false",
			mutate: func(f url.Values) {
				f.Set("outputFormat", "TEXT")
				f.Set("includeMetadata", "false")
				f.Del("columns")
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "non-numeric limit (gin binding error)",
			mutate:     func(f url.Values) { f.Set("limit", "abc") },
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := mountDownloadHandler(newTestDownloadHandlers(nil))

			form := baseDownloadForm()
			if tt.mutate != nil {
				tt.mutate(form)
			}

			w := postDownload(engine, form)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestDownloadResponseHeaders(t *testing.T) {
	engine := mountDownloadHandler(newTestDownloadHandlers(nil))
	w := postDownload(engine, baseDownloadForm())

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/tab-separated-values; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.True(t, strings.HasPrefix(cd, `attachment; filename="logs-`), "Content-Disposition: %q", cd)
	assert.True(t, strings.HasSuffix(cd, `.tsv"`), "Content-Disposition: %q", cd)
}

// Behavioral check: the download endpoint accepts the CSRF token via a
// `csrfToken` form field when no X-CSRF-Token header is present (HTML form
// submission via a hidden iframe can't set headers).
//
// Uses token auth mode so the request passes the CSRF gate and is rejected
// later by k8sAuthenticationMiddleware (401) — we assert specifically that
// the CSRF gate did not return 403.
func TestDownloadPOSTAcceptsCSRFTokenInFormField(t *testing.T) {
	cfg := newTestConfig()
	cfg.AuthMode = config.AuthModeToken
	app := newTestApp(cfg)

	srv := httptest.NewServer(app)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Prime the CSRF token + session cookie.
	primeReq, _ := http.NewRequest("GET", srv.URL+"/api/auth/session", nil)
	primeResp, err := client.Do(primeReq)
	if err != nil {
		t.Fatal(err)
	}
	primeResp.Body.Close()
	csrfTok := primeResp.Header.Get("X-CSRF-Token")
	assert.NotEmpty(t, csrfTok)

	// POST with csrfToken in the form, no X-CSRF-Token header.
	form := baseDownloadForm()
	form.Set("csrfToken", csrfTok)
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// CSRF gate must allow the request through. Token-mode auth then
	// rejects it with 401 because there's no bearer/session k8s token.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"expected request to pass CSRF gate (form-field token) and be rejected later by auth")
}

// Same setup as the form-field test, but without the csrfToken field —
// expect a 403 from the CSRF gate (the form fallback doesn't trip without
// the field).
func TestDownloadPOSTRejectsMissingCSRFTokenFormField(t *testing.T) {
	cfg := newTestConfig()
	cfg.AuthMode = config.AuthModeToken
	app := newTestApp(cfg)

	srv := httptest.NewServer(app)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Prime so the session has a token; we just don't send it.
	primeReq, _ := http.NewRequest("GET", srv.URL+"/api/auth/session", nil)
	primeResp, _ := client.Do(primeReq)
	primeResp.Body.Close()

	form := baseDownloadForm()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// Behavioral check: the download endpoint is mounted under protectedRoutes,
// so requests without a CSRF token are blocked at the dynamic-route gate.
func TestDownloadPOSTRequiresCSRFToken(t *testing.T) {
	app := newTestApp(nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/download", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	app.ServeHTTP(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Behavioral check: in token mode, a request without a session token is
// rejected by k8sAuthenticationMiddleware on the protected-route group.
func TestDownloadPOSTRequiresAuthInTokenMode(t *testing.T) {
	cfg := newTestConfig()
	cfg.AuthMode = config.AuthModeToken
	app := newTestApp(cfg)
	client := testutils.NewWebTestClient(t, app)
	defer client.Teardown()

	// Prime the CSRF token so the POST reaches the auth check.
	client.Get("/api/auth/session")

	req := client.NewRequest("POST", "/api/v1/download", nil)
	resp := client.Do(req)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

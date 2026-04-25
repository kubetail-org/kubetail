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
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// fakeStreamer emits a pre-populated slice of LogRecords through the Records
// channel. Ignores options; use only for output-path assertions.
type fakeStreamer struct {
	records []logs.LogRecord
	ch      chan logs.LogRecord
	err     error
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
func (f *fakeStreamer) Err() error                     { return f.err }
func (f *fakeStreamer) Close()                         {}

// newTestDownloadHandlers builds a downloadHandlers wired to a fake stream
// returning the given records. The embedded *App is nil — the handler does
// not use it directly, only via h.newLogStream.
func newTestDownloadHandlers(records []logs.LogRecord) *downloadHandlers {
	return &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logStreamer, error) {
			return &fakeStreamer{records: records, ch: make(chan logs.LogRecord, len(records))}, nil
		},
	}
}

// mountDownloadHandler returns a minimal gin engine with only the download
// route mounted — no CSRF, auth, or session middleware. Those are covered
// separately in app_test.go.
func mountDownloadHandler(h *downloadHandlers) *gin.Engine {
	r := gin.New()
	r.POST("/api/logs/download", h.DownloadPOST)
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
	r := httptest.NewRequest("POST", "/api/logs/download", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(w, r)
	return w
}

func TestBuildStreamOptionsAllowedNamespaces(t *testing.T) {
	req := &downloadRequest{raw: downloadForm{Mode: downloadModeHead}}

	without := buildStreamOptions(req, "", nil)
	withEmpty := buildStreamOptions(req, "", []string{})
	withNs := buildStreamOptions(req, "", []string{"ns1", "ns2"})

	// nil/empty allowedNamespaces produce the same option count.
	assert.Equal(t, len(without), len(withEmpty))

	// non-empty allowedNamespaces appends exactly one extra option.
	assert.Equal(t, len(without)+1, len(withNs))
}

func TestDownloadForwardsAllowedNamespaces(t *testing.T) {
	var capturedOptCount int
	h := &downloadHandlers{
		allowedNamespaces: []string{"allowed-ns"},
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logStreamer, error) {
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
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logStreamer, error) {
			baselineOptCount = len(opts)
			return &fakeStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}
	w2 := postDownload(mountDownloadHandler(baseline), baseDownloadForm())
	assert.Equal(t, http.StatusOK, w2.Code)

	assert.Equal(t, baselineOptCount+1, capturedOptCount)
}

func TestDownloadFormValidation(t *testing.T) {
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
			name:       "invalid outputFormat",
			mutate:     func(f url.Values) { f.Set("outputFormat", "XML") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid messageFormat",
			mutate:     func(f url.Values) { f.Set("messageFormat", "XYZ") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid since",
			mutate:     func(f url.Values) { f.Set("since", "not-a-time") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid until",
			mutate:     func(f url.Values) { f.Set("until", "not-a-time") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown column",
			mutate:     func(f url.Values) { f.Add("columns", "bogus") },
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
			name:       "tsv requires metadata",
			mutate:     func(f url.Values) { f.Set("includeMetadata", "false") },
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
			name:       "duration since",
			mutate:     func(f url.Values) { f.Set("since", "PT1M") },
			wantStatus: http.StatusOK,
		},
		{
			name:       "rfc3339 since",
			mutate:     func(f url.Values) { f.Set("since", "2026-04-01T00:00:00Z") },
			wantStatus: http.StatusOK,
		},
		{
			name:       "negative limit",
			mutate:     func(f url.Values) { f.Set("limit", "-5") },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "non-numeric limit",
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

func TestDownloadTSVOutput(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "hello",
			Source:    logs.LogSource{Namespace: "default", PodName: "p1", ContainerName: "c1"},
		},
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 1, 0, time.UTC),
			Message:   "world",
			Source:    logs.LogSource{Namespace: "default", PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Del("columns")
	form["columns"] = []string{"timestamp", "pod", "container", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/tab-separated-values; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.True(t, strings.HasPrefix(cd, `attachment; filename="logs-`), "Content-Disposition: %q", cd)
	assert.True(t, strings.HasSuffix(cd, `.tsv"`), "Content-Disposition: %q", cd)

	wantBody := "timestamp\tpod\tcontainer\tmessage\n" +
		"2026-04-01T00:00:00Z\tp1\tc1\thello\n" +
		"2026-04-01T00:00:01Z\tp1\tc1\tworld\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadCSVOutput(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   `a,b "quoted" c`,
			Source:    logs.LogSource{PodName: "p1", ContainerName: "c1"},
		},
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 1, 0, time.UTC),
			Message:   "multi\nline",
			Source:    logs.LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("outputFormat", "CSV")
	form.Del("columns")
	form["columns"] = []string{"timestamp", "pod", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/csv; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.True(t, strings.HasSuffix(cd, `.csv"`), "Content-Disposition: %q", cd)

	// encoding/csv with UseCRLF normalizes LF inside quoted fields to CRLF.
	wantBody := "timestamp,pod,message\r\n" +
		`2026-04-01T00:00:00Z,p1,"a,b ""quoted"" c"` + "\r\n" +
		`2026-04-01T00:00:01Z,p1,"multi` + "\r\n" + `line"` + "\r\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadTextOutput_StripAnsi(t *testing.T) {
	records := []logs.LogRecord{
		{Message: "\x1b[31mred\x1b[0m normal"},
		{Message: "plain line"},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("outputFormat", "TEXT")
	form.Set("messageFormat", "TEXT")
	form.Set("includeMetadata", "false")
	form.Del("columns")

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.True(t, strings.HasSuffix(cd, `.txt"`), "Content-Disposition: %q", cd)
	assert.Equal(t, "red normal\nplain line\n", w.Body.String())
}

func TestDownloadTextOutput_KeepAnsi(t *testing.T) {
	records := []logs.LogRecord{
		{Message: "\x1b[31mred\x1b[0m normal"},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("outputFormat", "TEXT")
	form.Set("messageFormat", "ANSI")
	form.Set("includeMetadata", "false")
	form.Del("columns")

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "\x1b[31mred\x1b[0m normal\n", w.Body.String())
}

func TestDownloadTSVOutput_StripAnsi(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    logs.LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("messageFormat", "TEXT")
	form.Del("columns")
	form["columns"] = []string{"timestamp", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	wantBody := "timestamp\tmessage\n" +
		"2026-04-01T00:00:00Z\tred normal\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadTSVOutput_KeepAnsi(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    logs.LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("messageFormat", "ANSI")
	form.Del("columns")
	form["columns"] = []string{"timestamp", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	wantBody := "timestamp\tmessage\n" +
		"2026-04-01T00:00:00Z\t\x1b[31mred\x1b[0m normal\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadCSVOutput_StripAnsi(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    logs.LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Set("outputFormat", "CSV")
	form.Set("messageFormat", "TEXT")
	form.Del("columns")
	form["columns"] = []string{"timestamp", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	wantBody := "timestamp,message\r\n" +
		"2026-04-01T00:00:00Z,red normal\r\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadColumnOrderAndSubset(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "hello",
			Source: logs.LogSource{
				PodName:       "p1",
				ContainerName: "c1",
				Metadata:      logs.LogSourceMetadata{Region: "us-east-1", Node: "n1"},
			},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Del("columns")
	// Deliberately out-of-order + subset (skip container, pod, zone, os, arch).
	form["columns"] = []string{"message", "region", "node", "timestamp"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	wantBody := "message\tregion\tnode\ttimestamp\n" +
		"hello\tus-east-1\tn1\t2026-04-01T00:00:00Z\n"
	assert.Equal(t, wantBody, w.Body.String())
}

func TestDownloadTSVEscapesTabsAndNewlines(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "a\tb\nc",
			Source:    logs.LogSource{PodName: "p\t1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records))

	form := baseDownloadForm()
	form.Del("columns")
	form["columns"] = []string{"timestamp", "pod", "container", "message"}

	w := postDownload(engine, form)

	assert.Equal(t, http.StatusOK, w.Code)
	// tabs and newlines in fields are replaced with spaces so rows/columns stay aligned
	wantBody := "timestamp\tpod\tcontainer\tmessage\n" +
		"2026-04-01T00:00:00Z\tp 1\tc1\ta b c\n"
	assert.Equal(t, wantBody, w.Body.String())
}

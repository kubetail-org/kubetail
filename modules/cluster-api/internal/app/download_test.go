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

// fakeDownloadStreamer emits canned LogRecords through the Records channel.
type fakeDownloadStreamer struct {
	records []logs.LogRecord
	ch      chan logs.LogRecord
}

func (f *fakeDownloadStreamer) Start(ctx context.Context) error {
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

func (f *fakeDownloadStreamer) Records() <-chan logs.LogRecord { return f.ch }
func (f *fakeDownloadStreamer) Err() error                     { return nil }
func (f *fakeDownloadStreamer) Close()                         {}

func newTestDownloadHandlers(records []logs.LogRecord, captured *[]logs.Option) *downloadHandlers {
	return &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			if captured != nil {
				*captured = opts
			}
			return &fakeDownloadStreamer{records: records, ch: make(chan logs.LogRecord, len(records))}, nil
		},
	}
}

func mountDownloadHandler(h *downloadHandlers) *gin.Engine {
	r := gin.New()
	r.Use(authenticationMiddleware)
	r.Use(requireTokenMiddleware)
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

func postDownload(engine *gin.Engine, form url.Values, withBearer bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/download", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withBearer {
		r.Header.Set("Authorization", "Bearer test-token")
	}
	engine.ServeHTTP(w, r)
	return w
}

func TestDownloadRequiresBearerToken(t *testing.T) {
	engine := mountDownloadHandler(newTestDownloadHandlers(nil, nil))
	w := postDownload(engine, baseDownloadForm(), false)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDownloadHappyPathTSV(t *testing.T) {
	records := []logs.LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "hello",
			Source:    logs.LogSource{Namespace: "default", PodName: "p1", ContainerName: "c1"},
		},
	}
	engine := mountDownloadHandler(newTestDownloadHandlers(records, nil))

	form := baseDownloadForm()
	form.Del("columns")
	form["columns"] = []string{"timestamp", "pod", "message"}

	w := postDownload(engine, form, true)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/tab-separated-values; charset=utf-8", w.Header().Get("Content-Type"))
	cd := w.Header().Get("Content-Disposition")
	assert.True(t, strings.HasPrefix(cd, `attachment; filename="logs-`), "Content-Disposition: %q", cd)
	assert.True(t, strings.HasSuffix(cd, `.tsv"`), "Content-Disposition: %q", cd)

	want := "timestamp\tpod\tmessage\n" +
		"2026-04-01T00:00:00Z\tp1\thello\n"
	assert.Equal(t, want, w.Body.String())
}

func TestDownloadInvalidFormFails(t *testing.T) {
	engine := mountDownloadHandler(newTestDownloadHandlers(nil, nil))
	form := baseDownloadForm()
	form.Set("mode", "INVALID")
	w := postDownload(engine, form, true)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// The cluster-api server runs against its in-cluster config, so it can't
// honor a per-request kubeContext (InClusterConnectionManager rejects any
// non-empty kubeContext with a 500). The handler must drop that form field
// before building stream options.
func TestDownloadIgnoresKubeContextFromForm(t *testing.T) {
	var withCount, withoutCount int
	with := &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			withCount = len(opts)
			return &fakeDownloadStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}
	without := &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			withoutCount = len(opts)
			return &fakeDownloadStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}

	formWith := baseDownloadForm()
	formWith.Set("kubeContext", "some-ctx")

	w1 := postDownload(mountDownloadHandler(with), formWith, true)
	assert.Equal(t, http.StatusOK, w1.Code)
	w2 := postDownload(mountDownloadHandler(without), baseDownloadForm(), true)
	assert.Equal(t, http.StatusOK, w2.Code)

	assert.Equal(t, withoutCount, withCount, "kubeContext form field must not add a WithKubeContext option")
}

func TestDownloadAppliesAllowedNamespaces(t *testing.T) {
	var withCount, withoutCount int
	with := &downloadHandlers{
		allowedNamespaces: []string{"ns1"},
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			withCount = len(opts)
			return &fakeDownloadStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}
	without := &downloadHandlers{
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			withoutCount = len(opts)
			return &fakeDownloadStreamer{ch: make(chan logs.LogRecord)}, nil
		},
	}

	w1 := postDownload(mountDownloadHandler(with), baseDownloadForm(), true)
	assert.Equal(t, http.StatusOK, w1.Code)
	w2 := postDownload(mountDownloadHandler(without), baseDownloadForm(), true)
	assert.Equal(t, http.StatusOK, w2.Code)

	// allowedNamespaces adds exactly one option (WithAllowedNamespaces).
	assert.Equal(t, withoutCount+1, withCount)
}

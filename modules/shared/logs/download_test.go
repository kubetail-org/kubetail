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

package logs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeDownloadStreamer emits a pre-populated slice of LogRecords through the
// Records channel. Ignores options; use only for output-path assertions.
type fakeDownloadStreamer struct {
	records []LogRecord
	ch      chan LogRecord
	err     error
}

func newFakeStreamer(records []LogRecord) *fakeDownloadStreamer {
	return &fakeDownloadStreamer{records: records, ch: make(chan LogRecord, len(records))}
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

func (f *fakeDownloadStreamer) Records() <-chan LogRecord { return f.ch }
func (f *fakeDownloadStreamer) Err() error                { return f.err }
func (f *fakeDownloadStreamer) Close()                    {}

func intPtr(i int) *int { return &i }

func baseDownloadRequest() *DownloadRequest {
	return &DownloadRequest{
		Raw: DownloadForm{
			Sources:         []string{"default:pod/my-pod"},
			Mode:            DownloadModeHead,
			OutputFormat:    DownloadOutputTSV,
			MessageFormat:   DownloadMsgText,
			IncludeMetadata: true,
			Columns:         []string{"timestamp", "message"},
		},
	}
}

// runStream wires a fake streamer into WriteDownloadStream and returns the
// resulting body so format-level tests can compare bytes directly.
func runStream(t *testing.T, req *DownloadRequest, records []LogRecord) string {
	t.Helper()
	stream := newFakeStreamer(records)
	if err := stream.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteDownloadStream(context.Background(), &buf, req, stream); err != nil {
		t.Fatalf("WriteDownloadStream: %v", err)
	}
	return buf.String()
}

func TestDownloadFormValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*DownloadForm)
		wantField string
	}{
		{
			name:      "valid form",
			wantField: "",
		},
		{
			name:      "empty sources",
			mutate:    func(f *DownloadForm) { f.Sources = nil },
			wantField: "sources",
		},
		{
			name:      "invalid mode",
			mutate:    func(f *DownloadForm) { f.Mode = "XYZ" },
			wantField: "mode",
		},
		{
			name:      "invalid outputFormat",
			mutate:    func(f *DownloadForm) { f.OutputFormat = "XML" },
			wantField: "outputFormat",
		},
		{
			name:      "invalid messageFormat",
			mutate:    func(f *DownloadForm) { f.MessageFormat = "XYZ" },
			wantField: "messageFormat",
		},
		{
			name:      "invalid since",
			mutate:    func(f *DownloadForm) { f.Since = "not-a-time" },
			wantField: "since",
		},
		{
			name:      "invalid until",
			mutate:    func(f *DownloadForm) { f.Until = "not-a-time" },
			wantField: "until",
		},
		{
			name:      "unknown column",
			mutate:    func(f *DownloadForm) { f.Columns = append(f.Columns, "bogus") },
			wantField: "columns",
		},
		{
			name: "csv requires metadata",
			mutate: func(f *DownloadForm) {
				f.OutputFormat = DownloadOutputCSV
				f.IncludeMetadata = false
			},
			wantField: "includeMetadata",
		},
		{
			name:      "tsv requires metadata",
			mutate:    func(f *DownloadForm) { f.IncludeMetadata = false },
			wantField: "includeMetadata",
		},
		{
			name: "text allows metadata false",
			mutate: func(f *DownloadForm) {
				f.OutputFormat = DownloadOutputText
				f.IncludeMetadata = false
				f.Columns = nil
			},
			wantField: "",
		},
		{
			name:      "duration since",
			mutate:    func(f *DownloadForm) { f.Since = "PT1M" },
			wantField: "",
		},
		{
			name:      "rfc3339 since",
			mutate:    func(f *DownloadForm) { f.Since = "2026-04-01T00:00:00Z" },
			wantField: "",
		},
		{
			name:      "negative limit",
			mutate:    func(f *DownloadForm) { f.Limit = intPtr(-5) },
			wantField: "limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := baseDownloadRequest().Raw
			if tt.mutate != nil {
				tt.mutate(&f)
			}
			req, verr := f.Validate()
			if tt.wantField == "" {
				assert.Nil(t, verr)
				assert.NotNil(t, req)
			} else {
				assert.NotNil(t, verr)
				if verr != nil {
					assert.Equal(t, tt.wantField, verr.Field)
				}
				assert.Nil(t, req)
			}
		})
	}
}

func TestBuildDownloadStreamOptionsAllowedNamespaces(t *testing.T) {
	req := &DownloadRequest{Raw: DownloadForm{Mode: DownloadModeHead}}

	without := BuildDownloadStreamOptions(req, "", nil)
	withEmpty := BuildDownloadStreamOptions(req, "", []string{})
	withNs := BuildDownloadStreamOptions(req, "", []string{"ns1", "ns2"})

	assert.Equal(t, len(without), len(withEmpty))
	assert.Equal(t, len(without)+1, len(withNs))
}

func TestBuildDownloadStreamOptionsBearerToken(t *testing.T) {
	req := &DownloadRequest{Raw: DownloadForm{Mode: DownloadModeHead}}

	without := BuildDownloadStreamOptions(req, "", nil)
	with := BuildDownloadStreamOptions(req, "tok", nil)

	assert.Equal(t, len(without)+1, len(with))
}

func TestDownloadContentTypeAndExt(t *testing.T) {
	cases := []struct {
		format       string
		wantType     string
		wantExt      string
		wantFilename string
	}{
		{DownloadOutputTSV, "text/tab-separated-values; charset=utf-8", "tsv", "logs-2026-04-01_00-00-00.tsv"},
		{DownloadOutputCSV, "text/csv; charset=utf-8", "csv", "logs-2026-04-01_00-00-00.csv"},
		{DownloadOutputText, "text/plain; charset=utf-8", "txt", "logs-2026-04-01_00-00-00.txt"},
	}
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range cases {
		t.Run(tt.format, func(t *testing.T) {
			assert.Equal(t, tt.wantType, DownloadContentType(tt.format))
			assert.Equal(t, tt.wantExt, DownloadExt(tt.format))
			assert.Equal(t, tt.wantFilename, DownloadFilename(tt.format, now))
		})
	}
}

func TestWriteDownloadStream_TSV(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.Columns = []string{"timestamp", "pod", "container", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "hello",
			Source:    LogSource{Namespace: "default", PodName: "p1", ContainerName: "c1"},
		},
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 1, 0, time.UTC),
			Message:   "world",
			Source:    LogSource{Namespace: "default", PodName: "p1", ContainerName: "c1"},
		},
	}

	body := runStream(t, req, records)
	want := "timestamp\tpod\tcontainer\tmessage\n" +
		"2026-04-01T00:00:00Z\tp1\tc1\thello\n" +
		"2026-04-01T00:00:01Z\tp1\tc1\tworld\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_CSV(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.OutputFormat = DownloadOutputCSV
	req.Raw.Columns = []string{"timestamp", "pod", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   `a,b "quoted" c`,
			Source:    LogSource{PodName: "p1", ContainerName: "c1"},
		},
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 1, 0, time.UTC),
			Message:   "multi\nline",
			Source:    LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}

	body := runStream(t, req, records)
	want := "timestamp,pod,message\r\n" +
		`2026-04-01T00:00:00Z,p1,"a,b ""quoted"" c"` + "\r\n" +
		`2026-04-01T00:00:01Z,p1,"multi` + "\r\n" + `line"` + "\r\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_TextStripAnsi(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.OutputFormat = DownloadOutputText
	req.Raw.IncludeMetadata = false
	req.Raw.Columns = nil

	records := []LogRecord{
		{Message: "\x1b[31mred\x1b[0m normal"},
		{Message: "plain line"},
	}

	body := runStream(t, req, records)
	assert.Equal(t, "red normal\nplain line\n", body)
}

func TestWriteDownloadStream_TextKeepAnsi(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.OutputFormat = DownloadOutputText
	req.Raw.MessageFormat = DownloadMsgAnsi
	req.Raw.IncludeMetadata = false
	req.Raw.Columns = nil

	records := []LogRecord{
		{Message: "\x1b[31mred\x1b[0m normal"},
	}
	body := runStream(t, req, records)
	assert.Equal(t, "\x1b[31mred\x1b[0m normal\n", body)
}

func TestWriteDownloadStream_TSVStripAnsi(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.Columns = []string{"timestamp", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	body := runStream(t, req, records)
	want := "timestamp\tmessage\n" +
		"2026-04-01T00:00:00Z\tred normal\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_TSVKeepAnsi(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.MessageFormat = DownloadMsgAnsi
	req.Raw.Columns = []string{"timestamp", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	body := runStream(t, req, records)
	want := "timestamp\tmessage\n" +
		"2026-04-01T00:00:00Z\t\x1b[31mred\x1b[0m normal\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_CSVStripAnsi(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.OutputFormat = DownloadOutputCSV
	req.Raw.Columns = []string{"timestamp", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "\x1b[31mred\x1b[0m normal",
			Source:    LogSource{PodName: "p1", ContainerName: "c1"},
		},
	}
	body := runStream(t, req, records)
	want := "timestamp,message\r\n" +
		"2026-04-01T00:00:00Z,red normal\r\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_ColumnOrderAndSubset(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.Columns = []string{"message", "region", "node", "timestamp"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "hello",
			Source: LogSource{
				PodName:       "p1",
				ContainerName: "c1",
				Metadata:      LogSourceMetadata{Region: "us-east-1", Node: "n1"},
			},
		},
	}
	body := runStream(t, req, records)
	want := "message\tregion\tnode\ttimestamp\n" +
		"hello\tus-east-1\tn1\t2026-04-01T00:00:00Z\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_TSVEscapesTabsAndNewlines(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.Columns = []string{"timestamp", "pod", "container", "message"}

	records := []LogRecord{
		{
			Timestamp: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Message:   "a\tb\nc",
			Source:    LogSource{PodName: "p\t1", ContainerName: "c1"},
		},
	}
	body := runStream(t, req, records)
	want := "timestamp\tpod\tcontainer\tmessage\n" +
		"2026-04-01T00:00:00Z\tp 1\tc1\ta b c\n"
	assert.Equal(t, want, body)
}

func TestWriteDownloadStream_PropagatesStreamErr(t *testing.T) {
	req := baseDownloadRequest()
	req.Raw.OutputFormat = DownloadOutputText
	req.Raw.IncludeMetadata = false
	req.Raw.Columns = nil

	wantErr := errors.New("agent disconnected mid-stream")
	stream := &fakeDownloadStreamer{
		records: []LogRecord{{Message: "partial"}},
		ch:      make(chan LogRecord, 1),
		err:     wantErr,
	}
	if err := stream.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var buf bytes.Buffer
	err := WriteDownloadStream(context.Background(), &buf, req, stream)
	assert.ErrorIs(t, err, wantErr)
	// Records emitted before the error should still have been written.
	assert.Equal(t, "partial\n", buf.String())
}

func TestStripAnsi(t *testing.T) {
	assert.Equal(t, "red", StripAnsi("\x1b[31mred\x1b[0m"))
	assert.Equal(t, "plain", StripAnsi("plain"))
	// OSC hyperlink (ESC ] ... ST)
	assert.Equal(t, "click", StripAnsi("\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\"))
	assert.True(t, strings.Contains(StripAnsi("hello \x1b[1mworld\x1b[0m"), "hello world"))
}

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
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Matches the ANSI escape sequences that show up in practice in container
// logs: CSI (cursor/erase + SGR colors) and OSC (incl. hyperlinks). Uses
// ESC-introduced forms only — 8-bit C1 introducers aren't valid standalone
// UTF-8 and don't appear in real-world container output.
var ansiRe = regexp.MustCompile(
	`\x1b[[\]()#;?]*` +
		`(?:` +
		`(?:(?:(?:;[-a-zA-Z\d/#&.:=?%@~_]+)*|[a-zA-Z\d]+(?:;[-a-zA-Z\d/#&.:=?%@~_]*)*)?(?:\x07|\x1b\\))` +
		`|` +
		`(?:(?:\d{1,4}(?:;\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~])` +
		`)`,
)

// StripAnsi removes ANSI escape sequences from s.
func StripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// Supported download form enum values.
const (
	DownloadModeHead = "HEAD"
	DownloadModeTail = "TAIL"

	DownloadOutputTSV  = "TSV"
	DownloadOutputCSV  = "CSV"
	DownloadOutputText = "TEXT"

	DownloadMsgText = "TEXT"
	DownloadMsgAnsi = "ANSI"
)

// allowedDownloadColumns lists backend column names accepted in the form's
// `columns[]` field. Mirrors dashboard-ui/src/pages/console/shared.ts.
var allowedDownloadColumns = map[string]struct{}{
	"timestamp": {},
	"pod":       {},
	"container": {},
	"region":    {},
	"zone":      {},
	"os":        {},
	"arch":      {},
	"node":      {},
	"message":   {},
}

// DownloadStreamer is the subset of *Stream used by download handlers. It
// lets tests inject fakes without standing up a real cluster.
type DownloadStreamer interface {
	Start(ctx context.Context) error
	Records() <-chan LogRecord
	Err() error
	Close()
}

// NewDownloadStreamFn opens a download stream against the given sources.
type NewDownloadStreamFn func(ctx context.Context, sources []string, opts ...Option) (DownloadStreamer, error)

// DownloadForm is the raw form as submitted by the dashboard's download
// dialog. Bound by gin's form binder via the struct tags.
type DownloadForm struct {
	KubeContext     string   `form:"kubeContext"`
	Sources         []string `form:"sources"`
	Grep            string   `form:"grep"`
	Regions         []string `form:"sourceFilter.region"`
	Zones           []string `form:"sourceFilter.zone"`
	OSes            []string `form:"sourceFilter.os"`
	Arches          []string `form:"sourceFilter.arch"`
	Nodes           []string `form:"sourceFilter.node"`
	Containers      []string `form:"sourceFilter.container"`
	Mode            string   `form:"mode"`
	Limit           *int     `form:"limit"`
	Since           string   `form:"since"`
	Until           string   `form:"until"`
	OutputFormat    string   `form:"outputFormat"`
	MessageFormat   string   `form:"messageFormat"`
	IncludeMetadata bool     `form:"includeMetadata"`
	Columns         []string `form:"columns"`
}

// DownloadRequest is a parsed and validated download form ready for use.
type DownloadRequest struct {
	Raw   DownloadForm
	Since time.Time
	Until time.Time
}

// DownloadValidationError reports a specific form field that failed validation.
type DownloadValidationError struct {
	Field   string
	Message string
}

func (e *DownloadValidationError) Error() string { return e.Message }

// Validate checks the form and returns a parsed DownloadRequest, or a
// DownloadValidationError naming the offending field.
func (f DownloadForm) Validate() (*DownloadRequest, *DownloadValidationError) {
	verr := func(field, msg string) (*DownloadRequest, *DownloadValidationError) {
		return nil, &DownloadValidationError{Field: field, Message: msg}
	}

	if len(f.Sources) == 0 {
		return verr("sources", "at least one source is required")
	}

	switch f.Mode {
	case DownloadModeHead, DownloadModeTail:
	default:
		return verr("mode", fmt.Sprintf("must be one of %s, %s", DownloadModeHead, DownloadModeTail))
	}

	switch f.OutputFormat {
	case DownloadOutputTSV, DownloadOutputCSV, DownloadOutputText:
	default:
		return verr("outputFormat", fmt.Sprintf("must be one of %s, %s, %s", DownloadOutputTSV, DownloadOutputCSV, DownloadOutputText))
	}

	switch f.MessageFormat {
	case DownloadMsgText, DownloadMsgAnsi:
	default:
		return verr("messageFormat", fmt.Sprintf("must be one of %s, %s", DownloadMsgText, DownloadMsgAnsi))
	}

	// Tabular formats require the metadata flag — otherwise the request is
	// ambiguous (rows with no columns).
	if (f.OutputFormat == DownloadOutputCSV || f.OutputFormat == DownloadOutputTSV) && !f.IncludeMetadata {
		return verr("includeMetadata", "must be true for CSV/TSV")
	}

	for _, c := range f.Columns {
		if _, ok := allowedDownloadColumns[c]; !ok {
			return verr("columns", fmt.Sprintf("unknown column: %s", c))
		}
	}

	if f.Limit != nil && *f.Limit < 0 {
		return verr("limit", "must be non-negative")
	}

	since, err := ParseTimeArg(f.Since)
	if err != nil {
		return verr("since", err.Error())
	}
	until, err := ParseTimeArg(f.Until)
	if err != nil {
		return verr("until", err.Error())
	}

	return &DownloadRequest{Raw: f, Since: since, Until: until}, nil
}

// BuildDownloadStreamOptions turns a parsed request into the []Option slice
// to pass to NewStream. allowedNamespaces, when non-empty, restricts the
// stream to those namespaces.
func BuildDownloadStreamOptions(req *DownloadRequest, bearerToken string, allowedNamespaces []string) []Option {
	// Up to ~14 conditional appends below; pre-size to avoid reallocs.
	opts := make([]Option, 0, 14)
	if req.Raw.KubeContext != "" {
		opts = append(opts, WithKubeContext(req.Raw.KubeContext))
	}
	if bearerToken != "" {
		opts = append(opts, WithBearerToken(bearerToken))
	}
	if len(allowedNamespaces) > 0 {
		opts = append(opts, WithAllowedNamespaces(allowedNamespaces))
	}
	if !req.Since.IsZero() {
		opts = append(opts, WithSince(req.Since))
	}
	if !req.Until.IsZero() {
		opts = append(opts, WithUntil(req.Until))
	}
	if req.Raw.Grep != "" {
		opts = append(opts, WithGrep(req.Raw.Grep))
	}
	if len(req.Raw.Regions) > 0 {
		opts = append(opts, WithRegions(req.Raw.Regions))
	}
	if len(req.Raw.Zones) > 0 {
		opts = append(opts, WithZones(req.Raw.Zones))
	}
	if len(req.Raw.OSes) > 0 {
		opts = append(opts, WithOSes(req.Raw.OSes))
	}
	if len(req.Raw.Arches) > 0 {
		opts = append(opts, WithArches(req.Raw.Arches))
	}
	if len(req.Raw.Nodes) > 0 {
		opts = append(opts, WithNodes(req.Raw.Nodes))
	}
	if len(req.Raw.Containers) > 0 {
		opts = append(opts, WithContainers(req.Raw.Containers))
	}

	switch req.Raw.Mode {
	case DownloadModeHead:
		if req.Raw.Limit != nil && *req.Raw.Limit > 0 {
			opts = append(opts, WithHead(int64(*req.Raw.Limit)))
		} else {
			opts = append(opts, WithAll())
		}
	case DownloadModeTail:
		if req.Raw.Limit != nil && *req.Raw.Limit > 0 {
			opts = append(opts, WithTail(int64(*req.Raw.Limit)))
		} else {
			opts = append(opts, WithAll())
		}
	}

	return opts
}

// DownloadContentType returns the response Content-Type for an output format.
func DownloadContentType(outputFormat string) string {
	switch outputFormat {
	case DownloadOutputCSV:
		return "text/csv; charset=utf-8"
	case DownloadOutputText:
		return "text/plain; charset=utf-8"
	default:
		return "text/tab-separated-values; charset=utf-8"
	}
}

// DownloadExt returns the filename extension for an output format.
func DownloadExt(outputFormat string) string {
	switch outputFormat {
	case DownloadOutputCSV:
		return "csv"
	case DownloadOutputText:
		return "txt"
	default:
		return "tsv"
	}
}

// DownloadFilename returns a default filename for a download response.
func DownloadFilename(outputFormat string, now time.Time) string {
	return fmt.Sprintf("logs-%s.%s",
		now.UTC().Format("2006-01-02_15-04-05"),
		DownloadExt(outputFormat),
	)
}

// downloadFieldValue returns the string representation of a column for a
// record. When stripAnsiCodes is true the "message" column has ANSI escape
// sequences removed so plain-text downloads don't leak terminal control codes.
func downloadFieldValue(r LogRecord, column string, stripAnsiCodes bool) string {
	switch column {
	case "timestamp":
		return r.Timestamp.UTC().Format(time.RFC3339Nano)
	case "pod":
		return r.Source.PodName
	case "container":
		return r.Source.ContainerName
	case "region":
		return r.Source.Metadata.Region
	case "zone":
		return r.Source.Metadata.Zone
	case "os":
		return r.Source.Metadata.OS
	case "arch":
		return r.Source.Metadata.Arch
	case "node":
		return r.Source.Metadata.Node
	case "message":
		if stripAnsiCodes {
			return StripAnsi(r.Message)
		}
		return r.Message
	}
	return ""
}

// DownloadRecordWriter writes a stream of log records in a specific output format.
type DownloadRecordWriter interface {
	WriteHeader() error
	WriteRecord(LogRecord) error
}

// tsvWriter emits a tab-separated header + data rows. Tabs and newlines in
// field values are replaced with spaces so row/column boundaries stay intact.
type tsvWriter struct {
	w         io.Writer
	columns   []string
	stripAnsi bool
}

func (tw *tsvWriter) WriteHeader() error {
	_, err := io.WriteString(tw.w, strings.Join(tw.columns, "\t")+"\n")
	return err
}

func (tw *tsvWriter) WriteRecord(r LogRecord) error {
	vals := make([]string, len(tw.columns))
	for i, col := range tw.columns {
		vals[i] = tsvEscape(downloadFieldValue(r, col, tw.stripAnsi))
	}
	_, err := io.WriteString(tw.w, strings.Join(vals, "\t")+"\n")
	return err
}

func tsvEscape(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}

// csvWriter emits RFC 4180 CSV via encoding/csv. It flushes after each record
// so the response streams rather than buffering.
type csvWriter struct {
	cw        *csv.Writer
	columns   []string
	stripAnsi bool
}

func (cw *csvWriter) WriteHeader() error {
	if err := cw.cw.Write(cw.columns); err != nil {
		return err
	}
	cw.cw.Flush()
	return cw.cw.Error()
}

func (cw *csvWriter) WriteRecord(r LogRecord) error {
	vals := make([]string, len(cw.columns))
	for i, col := range cw.columns {
		vals[i] = downloadFieldValue(r, col, cw.stripAnsi)
	}
	if err := cw.cw.Write(vals); err != nil {
		return err
	}
	cw.cw.Flush()
	return cw.cw.Error()
}

// textWriter emits one message per line. Message format TEXT strips ANSI
// escape sequences; ANSI leaves them intact.
type textWriter struct {
	w         io.Writer
	stripAnsi bool
}

func (tw *textWriter) WriteHeader() error { return nil }

func (tw *textWriter) WriteRecord(r LogRecord) error {
	msg := r.Message
	if tw.stripAnsi {
		msg = StripAnsi(msg)
	}
	_, err := io.WriteString(tw.w, msg+"\n")
	return err
}

// NewDownloadRecordWriter picks an encoder for the given request.
func NewDownloadRecordWriter(w io.Writer, req *DownloadRequest) DownloadRecordWriter {
	stripAnsiCodes := req.Raw.MessageFormat == DownloadMsgText
	switch req.Raw.OutputFormat {
	case DownloadOutputCSV:
		cw := csv.NewWriter(w)
		cw.UseCRLF = true
		return &csvWriter{cw: cw, columns: req.Raw.Columns, stripAnsi: stripAnsiCodes}
	case DownloadOutputText:
		return &textWriter{w: w, stripAnsi: stripAnsiCodes}
	default:
		return &tsvWriter{w: w, columns: req.Raw.Columns, stripAnsi: stripAnsiCodes}
	}
}

// WriteDownloadStream writes the column header (if any) and each stream record
// to w. If w implements an interface with `Flush()`, it flushes after the
// header and after each record so the response streams to the client.
//
// After the records channel closes the stream's terminal error is returned so
// callers can distinguish a truncated download (e.g. an upstream agent
// disconnect mid-stream) from a clean finish. The HTTP status has already been
// sent by then, so handlers can't change the response code, but they should
// log the failure rather than treat it as success.
func WriteDownloadStream(ctx context.Context, w io.Writer, req *DownloadRequest, stream DownloadStreamer) error {
	rw := NewDownloadRecordWriter(w, req)
	if err := rw.WriteHeader(); err != nil {
		return err
	}
	flusher, _ := w.(interface{ Flush() })
	if flusher != nil {
		flusher.Flush()
	}

	for rec := range stream.Records() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rw.WriteRecord(rec); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return stream.Err()
}

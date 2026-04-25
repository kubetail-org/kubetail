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
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
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

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// Supported form enum values.
const (
	downloadModeHead = "HEAD"
	downloadModeTail = "TAIL"

	downloadOutputTSV  = "TSV"
	downloadOutputCSV  = "CSV"
	downloadOutputText = "TEXT"

	downloadMsgText = "TEXT"
	downloadMsgAnsi = "ANSI"
)

// allowedColumns lists backend column names accepted in `columns[]`.
// Mirrors dashboard-ui/src/pages/console/shared.ts.
var allowedColumns = map[string]struct{}{
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

// logStreamer is the subset of logs.Stream used by the download handler;
// defined here so tests can inject fakes without standing up a real cluster.
type logStreamer interface {
	Start(ctx context.Context) error
	Records() <-chan logs.LogRecord
	Err() error
	Close()
}

type newLogStreamFn func(ctx context.Context, sources []string, opts ...logs.Option) (logStreamer, error)

// Raw form as submitted by the dashboard's download dialog.
type downloadForm struct {
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

// Parsed and validated form ready for use by the handler.
type downloadRequest struct {
	raw   downloadForm
	since time.Time
	until time.Time
}

// validate returns (*downloadRequest, field, message) where a non-empty field
// signals a validation error.
func (f downloadForm) validate() (*downloadRequest, string, string) {
	if len(f.Sources) == 0 {
		return nil, "sources", "at least one source is required"
	}

	switch f.Mode {
	case downloadModeHead, downloadModeTail:
	default:
		return nil, "mode", fmt.Sprintf("must be one of %s, %s", downloadModeHead, downloadModeTail)
	}

	switch f.OutputFormat {
	case downloadOutputTSV, downloadOutputCSV, downloadOutputText:
	default:
		return nil, "outputFormat", fmt.Sprintf("must be one of %s, %s, %s", downloadOutputTSV, downloadOutputCSV, downloadOutputText)
	}

	switch f.MessageFormat {
	case downloadMsgText, downloadMsgAnsi:
	default:
		return nil, "messageFormat", fmt.Sprintf("must be one of %s, %s", downloadMsgText, downloadMsgAnsi)
	}

	// Tabular formats require the metadata flag — otherwise the request is
	// ambiguous (rows with no columns).
	if (f.OutputFormat == downloadOutputCSV || f.OutputFormat == downloadOutputTSV) && !f.IncludeMetadata {
		return nil, "includeMetadata", "must be true for CSV/TSV"
	}

	for _, c := range f.Columns {
		if _, ok := allowedColumns[c]; !ok {
			return nil, "columns", fmt.Sprintf("unknown column: %s", c)
		}
	}

	if f.Limit != nil && *f.Limit < 0 {
		return nil, "limit", "must be non-negative"
	}

	since, err := logs.ParseTimeArg(f.Since)
	if err != nil {
		return nil, "since", err.Error()
	}
	until, err := logs.ParseTimeArg(f.Until)
	if err != nil {
		return nil, "until", err.Error()
	}

	return &downloadRequest{raw: f, since: since, until: until}, "", ""
}

// buildStreamOptions turns a parsed request into the []logs.Option slice.
// allowedNamespaces, when non-empty, restricts the stream to those namespaces.
func buildStreamOptions(req *downloadRequest, bearerToken string, allowedNamespaces []string) []logs.Option {
	opts := []logs.Option{}
	if req.raw.KubeContext != "" {
		opts = append(opts, logs.WithKubeContext(req.raw.KubeContext))
	}
	if bearerToken != "" {
		opts = append(opts, logs.WithBearerToken(bearerToken))
	}
	if len(allowedNamespaces) > 0 {
		opts = append(opts, logs.WithAllowedNamespaces(allowedNamespaces))
	}
	if !req.since.IsZero() {
		opts = append(opts, logs.WithSince(req.since))
	}
	if !req.until.IsZero() {
		opts = append(opts, logs.WithUntil(req.until))
	}
	if req.raw.Grep != "" {
		opts = append(opts, logs.WithGrep(req.raw.Grep))
	}
	if len(req.raw.Regions) > 0 {
		opts = append(opts, logs.WithRegions(req.raw.Regions))
	}
	if len(req.raw.Zones) > 0 {
		opts = append(opts, logs.WithZones(req.raw.Zones))
	}
	if len(req.raw.OSes) > 0 {
		opts = append(opts, logs.WithOSes(req.raw.OSes))
	}
	if len(req.raw.Arches) > 0 {
		opts = append(opts, logs.WithArches(req.raw.Arches))
	}
	if len(req.raw.Nodes) > 0 {
		opts = append(opts, logs.WithNodes(req.raw.Nodes))
	}
	if len(req.raw.Containers) > 0 {
		opts = append(opts, logs.WithContainers(req.raw.Containers))
	}

	switch req.raw.Mode {
	case downloadModeHead:
		if req.raw.Limit != nil && *req.raw.Limit > 0 {
			opts = append(opts, logs.WithHead(int64(*req.raw.Limit)))
		} else {
			opts = append(opts, logs.WithAll())
		}
	case downloadModeTail:
		if req.raw.Limit != nil && *req.raw.Limit > 0 {
			opts = append(opts, logs.WithTail(int64(*req.raw.Limit)))
		} else {
			opts = append(opts, logs.WithAll())
		}
	}

	return opts
}

// contentTypeFor returns the response Content-Type for an output format.
func contentTypeFor(outputFormat string) string {
	switch outputFormat {
	case downloadOutputCSV:
		return "text/csv; charset=utf-8"
	case downloadOutputText:
		return "text/plain; charset=utf-8"
	default:
		return "text/tab-separated-values; charset=utf-8"
	}
}

// extFor returns the filename extension for an output format.
func extFor(outputFormat string) string {
	switch outputFormat {
	case downloadOutputCSV:
		return "csv"
	case downloadOutputText:
		return "txt"
	default:
		return "tsv"
	}
}

// fieldValue returns the string representation of a column for a record.
// When stripAnsiCodes is true the "message" column has ANSI escape sequences
// removed so plain-text downloads don't leak terminal control codes.
func fieldValue(r logs.LogRecord, column string, stripAnsiCodes bool) string {
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
			return stripAnsi(r.Message)
		}
		return r.Message
	}
	return ""
}

// recordWriter writes a stream of log records in a specific output format.
type recordWriter interface {
	writeHeader() error
	writeRecord(logs.LogRecord) error
}

// tsvWriter emits a tab-separated header + data rows. Tabs and newlines in
// field values are replaced with spaces so row/column boundaries stay intact.
type tsvWriter struct {
	w         io.Writer
	columns   []string
	stripAnsi bool
}

func (tw *tsvWriter) writeHeader() error {
	_, err := io.WriteString(tw.w, strings.Join(tw.columns, "\t")+"\n")
	return err
}

func (tw *tsvWriter) writeRecord(r logs.LogRecord) error {
	vals := make([]string, len(tw.columns))
	for i, col := range tw.columns {
		vals[i] = tsvEscape(fieldValue(r, col, tw.stripAnsi))
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

func (cw *csvWriter) writeHeader() error {
	if err := cw.cw.Write(cw.columns); err != nil {
		return err
	}
	cw.cw.Flush()
	return cw.cw.Error()
}

func (cw *csvWriter) writeRecord(r logs.LogRecord) error {
	vals := make([]string, len(cw.columns))
	for i, col := range cw.columns {
		vals[i] = fieldValue(r, col, cw.stripAnsi)
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

func (tw *textWriter) writeHeader() error { return nil }

func (tw *textWriter) writeRecord(r logs.LogRecord) error {
	msg := r.Message
	if tw.stripAnsi {
		msg = stripAnsi(msg)
	}
	_, err := io.WriteString(tw.w, msg+"\n")
	return err
}

// newRecordWriter picks an encoder for the given request.
func newRecordWriter(w io.Writer, req *downloadRequest) recordWriter {
	stripAnsiCodes := req.raw.MessageFormat == downloadMsgText
	switch req.raw.OutputFormat {
	case downloadOutputCSV:
		cw := csv.NewWriter(w)
		cw.UseCRLF = true
		return &csvWriter{cw: cw, columns: req.raw.Columns, stripAnsi: stripAnsiCodes}
	case downloadOutputText:
		return &textWriter{w: w, stripAnsi: stripAnsiCodes}
	default:
		return &tsvWriter{w: w, columns: req.raw.Columns, stripAnsi: stripAnsiCodes}
	}
}

// Represents download handlers
type downloadHandlers struct {
	*App
	newLogStream      newLogStreamFn
	allowedNamespaces []string
}

// newDownloadHandlers wires the production log stream factory against the
// app's connection manager and threads allowedNamespaces through from config.
func newDownloadHandlers(app *App) *downloadHandlers {
	return &downloadHandlers{
		App: app,
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logStreamer, error) {
			return logs.NewStream(ctx, app.cm, sources, opts...)
		},
		allowedNamespaces: app.config.AllowedNamespaces,
	}
}

// Log download endpoint
func (h *downloadHandlers) DownloadPOST(c *gin.Context) {
	var form downloadForm
	if err := c.ShouldBind(&form); err != nil {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
			"error": err.Error(),
		})
		return
	}

	req, field, msg := form.validate()
	if field != "" {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
			"field":   field,
			"message": msg,
		})
		return
	}

	opts := buildStreamOptions(req, c.GetString(k8sTokenGinKey), h.allowedNamespaces)

	ctx := c.Request.Context()
	stream, err := h.newLogStream(ctx, req.raw.Sources, opts...)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer stream.Close()

	if err := stream.Start(ctx); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("logs-%s.%s",
		time.Now().UTC().Format("2006-01-02_15-04-05"),
		extFor(req.raw.OutputFormat),
	)
	c.Header("Content-Type", contentTypeFor(req.raw.OutputFormat))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Status(http.StatusOK)

	writer := newRecordWriter(c.Writer, req)
	if err := writer.writeHeader(); err != nil {
		return
	}
	c.Writer.Flush()

	for rec := range stream.Records() {
		if ctx.Err() != nil {
			return
		}
		if err := writer.writeRecord(rec); err != nil {
			return
		}
		c.Writer.Flush()
	}
}

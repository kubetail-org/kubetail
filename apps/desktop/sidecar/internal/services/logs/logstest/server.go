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

// Package logstest is the fake log server the logs service's tests read from, and
// the lines they feed it.
package logstest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing/iotest"
	"time"
)

// Server is the API server's log endpoint over containers' logs held in memory,
// honouring the parameters the fetcher sends. Hold Mu to read or write a field.
type Server struct {
	Mu sync.Mutex
	// Logs is each container's lines, "<ts> <message>", keyed "<ns>/<pod>/<container>",
	// with "/previous" for the instance before. A follow streams the lines added
	// after its read, so a line reaches every follow still open, and one opened
	// later reads it as part of the log: a follow the client has abandoned, whose
	// handler has not noticed yet, cannot take a line from the follow that
	// replaced it.
	Logs map[string][]string
	// Refuse answers a container's requests with a status and a message, and
	// RefuseReads its requests that follow nothing. Refuse wins over RefuseReads.
	Refuse, RefuseReads map[string]Denial
	// Grew is closed and replaced when a container's log grows or its follows
	// end, by key. Ended is the containers whose follows end, and any opened after.
	Grew  map[string]chan struct{}
	Ended map[string]bool
	// Queries is every request's query, in order, and Keys each one's container.
	Queries []url.Values
	Keys    []string
	// Opened and Closed count each container's follows that have opened and ended,
	// by key.
	Opened, Closed map[string]int
	// OnRequest, when set, runs before each request is served, with its index, and
	// OnOpen once each follow has opened, with its key. Set either before any request.
	OnRequest func(n int)
	OnOpen    func(key string)
	// Part is the most bytes of a message one line of the runtime's file holds: a
	// longer one is split, and tailLines counts each part. Zero splits none; it
	// starts at 16 KiB, the runtime's default.
	Part int
	// Broken, when set, is how many lines the next read that follows nothing serves
	// before its body fails.
	Broken *int
	// Refused builds the error Get answers a refused request with, from the
	// refusal's status and message. Get needs it set to answer a refusal.
	Refused func(code int, message string) error
}

// ErrBroken is a body that failed under its read, as a dropped connection does.
var ErrBroken = errors.New("connection reset")

// Denial is a refusal the server answers with: an HTTP status and a message.
type Denial struct {
	Code    int
	Message string
}

// New is an empty server whose Part is the runtime's 16 KiB.
func New() *Server {
	return &Server{Part: 16 << 10, Logs: map[string][]string{}, Refuse: map[string]Denial{}, RefuseReads: map[string]Denial{}, Grew: map[string]chan struct{}{}, Ended: map[string]bool{}, Opened: map[string]int{}, Closed: map[string]int{}}
}

// End ends key's follows, and any opened after.
func (s *Server) End(key string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Ended[key] = true
	s.Wake(key)
}

// Resume lets key's follows run again after End.
func (s *Server) Resume(key string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	delete(s.Ended, key)
}

// Wake tells key's follows its log changed. Call it holding s.Mu.
func (s *Server) Wake(key string) {
	if grew := s.Grew[key]; grew != nil {
		close(grew)
	}
	s.Grew[key] = make(chan struct{})
}

// Next is a copy of key's lines from the nth, whether its follows have ended,
// and a channel closed when either changes. A log emptied under a follow has
// fewer lines than the follow has sent.
func (s *Server) Next(key string, n int) ([]string, bool, <-chan struct{}) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.Grew[key] == nil {
		s.Grew[key] = make(chan struct{})
	}
	lines := s.Logs[key]
	return append([]string(nil), lines[min(n, len(lines)):]...), s.Ended[key], s.Grew[key]
}

// Add appends lines to key's log, as a running container writes them.
func (s *Server) Add(key string, lines ...string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Logs[key] = append(s.Logs[key], lines...)
	s.Wake(key)
}

// Rotate drops key's lines before d past 10:00, as a rotation drops the oldest.
func (s *Server) Rotate(key string, d time.Duration) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	var kept []string
	for _, line := range s.Logs[key] {
		ts, _ := time.Parse(time.RFC3339Nano, strings.SplitN(line, " ", 2)[0])
		if !ts.Before(at(d)) {
			kept = append(kept, line)
		}
	}
	s.Logs[key] = kept
}

// BreakNext fails the body of the next read that follows nothing after n lines.
func (s *Server) BreakNext(n int) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Broken = &n
}

// Clear empties key's log.
func (s *Server) Clear(key string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	delete(s.Logs, key)
}

// SetRefusal answers key's requests with r from now on, nil for none.
func (s *Server) SetRefusal(key string, r *Denial) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if r == nil {
		delete(s.Refuse, key)
		return
	}
	s.Refuse[key] = *r
}

// FollowsOpened is how many of key's follows have opened.
func (s *Server) FollowsOpened(key string) int {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Opened[key]
}

// FollowsClosed is how many of key's follows have ended.
func (s *Server) FollowsClosed(key string) int {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Closed[key]
}

// Requests is every query so far.
func (s *Server) Requests() []url.Values {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return append([]url.Values(nil), s.Queries...)
}

// RequestsOf is every query for key's container, in order. A follow's containers
// read concurrently, so a test about one container reads its requests alone.
func (s *Server) RequestsOf(key string) []url.Values {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	var out []url.Values
	for i, k := range s.Keys {
		if k == key {
			out = append(out, s.Queries[i])
		}
	}
	return out
}

// keyOf is the container a log path and query name, "" for a path that is no log's.
func keyOf(path string, q url.Values) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 7 || parts[0] != "api" || parts[6] != "log" {
		return ""
	}
	key := parts[3] + "/" + parts[5] + "/" + q.Get("container")
	if q.Get("previous") == "true" {
		key += "/previous"
	}
	return key
}

// Get reads s in memory, as a claim's get reads the API server, so a read's
// bytes arrive as the server wrote them. It never follows.
func (s *Server) Get(_ context.Context, path string, q url.Values) (io.ReadCloser, error) {
	text, _, broken, refused := s.serve(keyOf(path, q), q)
	if refused != nil {
		return nil, s.Refused(refused.Code, refused.Message)
	}
	if broken {
		return io.NopCloser(io.MultiReader(strings.NewReader(text), iotest.ErrReader(ErrBroken))), nil
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

// ServeHTTP serves the log endpoint over HTTP, following while follow=true until
// End or the client leaves; any other path is a 404.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := keyOf(r.URL.Path, r.URL.Query())
	if key == "" {
		http.NotFound(w, r)
		return
	}
	text, n, broken, refused := s.serve(key, r.URL.Query())
	if refused != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(refused.Code)
		_, _ = fmt.Fprintf(w, `{"kind":"Status","message":%q}`, refused.Message)
		return
	}
	_, _ = w.Write([]byte(text))
	if broken {
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
	if r.URL.Query().Get("follow") != "true" {
		return
	}
	w.(http.Flusher).Flush()
	s.Mu.Lock()
	s.Opened[key]++
	s.Mu.Unlock()
	if s.OnOpen != nil {
		s.OnOpen(key)
	}
	defer func() {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		s.Closed[key]++
	}()
	for {
		lines, ended, grew := s.Next(key, n)
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n"))
			w.(http.Flusher).Flush()
		}
		n += len(lines)
		if ended {
			return
		}
		select {
		case <-grew:
		case <-r.Context().Done():
			return
		}
	}
}

// serve is key's log as the request's parameters cut it, how many lines the log
// held, and whether its body breaks after it, or the refusal set for key, after the
// request is counted and the hook has run. As the kubelet's: sinceTime is whole
// seconds and filters from the file's first line, and tailLines counts a long line's
// parts.
func (s *Server) serve(key string, q url.Values) (string, int, bool, *Denial) {
	s.Mu.Lock()
	n := len(s.Queries)
	s.Queries = append(s.Queries, q)
	s.Keys = append(s.Keys, key)
	hook := s.OnRequest
	s.Mu.Unlock()
	if hook != nil {
		hook(n)
	}
	s.Mu.Lock()
	lines := append([]string(nil), s.Logs[key]...)
	held := len(lines)
	refused, isRefused := s.Refuse[key]
	if read, ok := s.RefuseReads[key]; ok && !isRefused && q.Get("follow") != "true" {
		refused, isRefused = read, true
	}
	broken := -1
	if s.Broken != nil && !isRefused && q.Get("follow") != "true" {
		broken, s.Broken = *s.Broken, nil
	}
	part := s.Part
	s.Mu.Unlock()
	if isRefused {
		return "", 0, false, &refused
	}
	if since := q.Get("sinceTime"); since != "" {
		at, _ := time.Parse(time.RFC3339, since)
		at = at.Truncate(time.Second)
		var kept []string
		for _, line := range lines {
			ts, _ := time.Parse(time.RFC3339Nano, strings.SplitN(line, " ", 2)[0])
			if !ts.Before(at) {
				kept = append(kept, line)
			}
		}
		lines = kept
	}
	if tail := q.Get("tailLines"); tail != "" {
		n, _ := strconv.Atoi(tail)
		start := len(lines)
		for start > 0 {
			parts := 1
			if part > 0 {
				parts = max(1, (len(strings.SplitN(lines[start-1], " ", 2)[1])+part-1)/part)
			}
			if n < parts {
				break
			}
			n -= parts
			start--
		}
		lines = lines[start:]
	}
	if broken >= 0 {
		lines = lines[:min(broken, len(lines))]
	}
	text := ""
	if len(lines) > 0 {
		text = strings.Join(lines, "\n") + "\n"
	}
	if limit := q.Get("limitBytes"); limit != "" {
		n, _ := strconv.Atoi(limit)
		text = text[:min(n, len(text))]
	}
	return text, held, broken >= 0, nil
}

// Stamped is a log line at d past 10:00 saying msg.
func Stamped(d time.Duration, msg string) string {
	return at(d).Format(time.RFC3339Nano) + " " + msg
}

// LineEvery is n lines a second apart from 10:00, saying their index.
func LineEvery(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = Stamped(time.Duration(i)*time.Second, strconv.Itoa(i))
	}
	return out
}

// Tails is the tailLines of each request after the first, which finds the first
// line, "" for the read of the first moment.
func Tails(s *Server) []string {
	var out []string
	for _, q := range s.Requests()[1:] {
		out = append(out, q.Get("tailLines"))
	}
	return out
}

// WideLines is n lines a second apart from 10:00, each its index padded to size.
func WideLines(n, size int) []string {
	out := make([]string, n)
	for i := range out {
		msg := strconv.Itoa(i)
		out[i] = Stamped(time.Duration(i)*time.Second, msg+strings.Repeat("x", size-len(msg)))
	}
	return out
}

// LoudLine is the loud tests' message. They run under smallCaps, where a second
// holds 1 KiB: a loud second is a dozen lines of LoudLine, which the body carries
// in 126 bytes.
var LoudLine = strings.Repeat("x", 100)

// Pauser pauses the first body a getter it wraps answers, not counting a first line's
// read or a follow's, after its first N lines until Gate closes or the read's context
// ends.
type Pauser struct {
	N      int
	Gate   <-chan struct{}
	paused atomic.Bool
}

// On is get, paused as p says.
func (p *Pauser) On(get func(context.Context, string, url.Values) (io.ReadCloser, error)) func(context.Context, string, url.Values) (io.ReadCloser, error) {
	return func(ctx context.Context, path string, q url.Values) (io.ReadCloser, error) {
		body, err := get(ctx, path, q)
		if err != nil || q.Has("limitBytes") || q.Get("follow") == "true" || p.paused.Swap(true) {
			return body, err
		}
		data, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, err
		}
		cut := 0
		for range p.N {
			if i := strings.IndexByte(string(data[cut:]), '\n'); i >= 0 {
				cut += i + 1
			}
		}
		rest := pausedReader{ctx: ctx, gate: p.Gate, r: strings.NewReader(string(data[cut:]))}
		return io.NopCloser(io.MultiReader(strings.NewReader(string(data[:cut])), &rest)), nil
	}
}

// pausedReader is r once gate closes; a read its context ended fails.
type pausedReader struct {
	ctx  context.Context
	gate <-chan struct{}
	r    io.Reader
}

func (p *pausedReader) Read(b []byte) (int, error) {
	select {
	case <-p.gate:
		return p.r.Read(b)
	case <-p.ctx.Done():
		return 0, p.ctx.Err()
	}
}

// at is a moment on 2026-10-08 at 10:00, offset by d.
func at(d time.Duration) time.Time {
	return time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC).Add(d)
}

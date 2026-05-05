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

package clusterapi

import (
	"context"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

type clientIface interface {
	LogRecordsFetch(ctx context.Context, v LogRecordsFetchVars) (*LogRecordsQueryResponse, error)
	LogRecordsFollow(ctx context.Context, v LogRecordsFollowVars) (<-chan logs.LogRecord, <-chan error)
}

type StreamConfig struct {
	KubeContext string
	Sources     []string
	Mode        string // "HEAD" or "TAIL"
	Since       string
	Until       string
	Grep        string
	Limit       int
	Follow      bool

	// Paginate, when true and Mode is HEAD, makes the bootstrap loop walk
	// every page of NextCursor until the server stops returning one.
	Paginate bool

	// Source filters mapped 1:1 to the cluster-api LogSourceFilter input.
	Regions []string
	Zones   []string
	OSes    []string
	Arches  []string
	Nodes   []string
}

// Stream consumes the cluster-api GraphQL endpoint and exposes a logs.Stream
// to the CLI's printLogs.
type Stream struct {
	client clientIface
	cfg    StreamConfig

	out chan logs.LogRecord

	mu  sync.Mutex
	err error

	cancel context.CancelFunc
	doneCh chan struct{}
}

func NewStream(client *Client, cfg StreamConfig) *Stream {
	return newStreamForTest(client, cfg)
}

func newStreamForTest(client clientIface, cfg StreamConfig) *Stream {
	return &Stream{
		client: client,
		cfg:    cfg,
		out:    make(chan logs.LogRecord, 64),
		doneCh: make(chan struct{}),
	}
}

// Start performs the bootstrap LogRecordsFetch synchronously (when Mode is
// set) so callers see connectivity errors before any records reach the
// consumer, then kicks off a goroutine that emits the seeded response and
// continues with pagination and/or the follow subscription. Follow-only
// flows (Mode == "") issue no synchronous round-trip — callers that need to
// probe API availability must do so before calling Start (see clusterapi
// Client.Ping); doing it here would either duplicate the caller's probe or
// re-introduce an unbounded round-trip on rootCtx.
func (s *Stream) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var first *LogRecordsQueryResponse
	if s.cfg.Mode != "" {
		resp, err := s.client.LogRecordsFetch(ctx, s.fetchVars(""))
		if err != nil {
			cancel()
			close(s.out)
			close(s.doneCh)
			return err
		}
		first = resp
	}

	go s.run(ctx, first)
	return nil
}

func (s *Stream) fetchVars(cursor string) LogRecordsFetchVars {
	return LogRecordsFetchVars{
		KubeContext: s.cfg.KubeContext,
		Sources:     s.cfg.Sources,
		Mode:        s.cfg.Mode,
		Since:       s.cfg.Since,
		Until:       s.cfg.Until,
		Grep:        s.cfg.Grep,
		Limit:       s.cfg.Limit,
		Cursor:      cursor,
		Regions:     s.cfg.Regions,
		Zones:       s.cfg.Zones,
		OSes:        s.cfg.OSes,
		Arches:      s.cfg.Arches,
		Nodes:       s.cfg.Nodes,
	}
}

// Sources returns nil; the Kubetail API streams already-merged records and
// printLogs falls back to header lengths for column widths when empty.
func (s *Stream) Sources() []logs.LogSource { return nil }

func (s *Stream) Records() <-chan logs.LogRecord { return s.out }

func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Stream) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.doneCh
}

func (s *Stream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *Stream) run(ctx context.Context, first *LogRecordsQueryResponse) {
	defer close(s.out)
	defer close(s.doneCh)

	// Bootstrap fetch: HEAD paginates via NextCursor; TAIL is a single page.
	// An empty Mode skips the bootstrap entirely (used by `-f` with no
	// explicit --tail, where the user wants only new records). The first
	// response was fetched synchronously by Start.
	var lastTimestamp string
	resp := first
	for resp != nil {
		for _, r := range resp.Records {
			select {
			case s.out <- r:
			case <-ctx.Done():
				return
			}
			lastTimestamp = r.Timestamp.Format(time.RFC3339Nano)
		}
		if !s.cfg.Paginate || resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		next, err := s.client.LogRecordsFetch(ctx, s.fetchVars(*resp.NextCursor))
		if err != nil {
			s.setErr(err)
			return
		}
		resp = next
	}

	if !s.cfg.Follow {
		return
	}

	// The cluster-api's logRecordsFollow subscription replays full history
	// when no since/after is set. Anchor at the last bootstrap record (so we
	// don't double-emit) or at "now" (so we don't replay history when the
	// bootstrap was skipped or returned no records).
	followAfter := lastTimestamp
	if followAfter == "" {
		followAfter = time.Now().Format(time.RFC3339Nano)
	}
	records, errs := s.client.LogRecordsFollow(ctx, LogRecordsFollowVars{
		KubeContext: s.cfg.KubeContext,
		Sources:     s.cfg.Sources,
		Since:       s.cfg.Since,
		After:       followAfter,
		Grep:        s.cfg.Grep,
		Regions:     s.cfg.Regions,
		Zones:       s.cfg.Zones,
		OSes:        s.cfg.OSes,
		Arches:      s.cfg.Arches,
		Nodes:       s.cfg.Nodes,
	})
	for records != nil || errs != nil {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-records:
			if !ok {
				records = nil
				continue
			}
			select {
			case s.out <- rec:
			case <-ctx.Done():
				return
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				s.setErr(err)
			}
		}
	}
}

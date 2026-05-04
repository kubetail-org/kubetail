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

func (s *Stream) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.run(ctx)
	return nil
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

func (s *Stream) run(ctx context.Context) {
	defer close(s.out)
	defer close(s.doneCh)

	// Bootstrap fetch: HEAD paginates via NextCursor; TAIL is a single page.
	// An empty Mode skips the bootstrap entirely (used by `-f` with no
	// explicit --tail, where the user wants only new records).
	cursor := ""
	var lastTimestamp string
	for s.cfg.Mode != "" {
		resp, err := s.client.LogRecordsFetch(ctx, LogRecordsFetchVars{
			KubeContext: s.cfg.KubeContext,
			Sources:     s.cfg.Sources,
			Mode:        s.cfg.Mode,
			Since:       s.cfg.Since,
			Until:       s.cfg.Until,
			Grep:        s.cfg.Grep,
			Limit:       s.cfg.Limit,
			Cursor:      cursor,
		})
		if err != nil {
			s.setErr(err)
			return
		}
		for _, r := range resp.Records {
			select {
			case s.out <- r:
			case <-ctx.Done():
				return
			}
			lastTimestamp = r.Timestamp.Format(time.RFC3339Nano)
		}
		if s.cfg.Mode != "HEAD" || resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
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

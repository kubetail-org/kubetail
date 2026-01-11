// Copyright 2024-2026 The Kubetail Authors
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
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	set "github.com/deckarep/golang-set/v2"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

const DEFAULT_MAX_CHUNK_SIZE = 16 * 1024 // 16 KB

// LogRecord represents a log record
type LogRecord struct {
	Timestamp time.Time
	Message   string
	Source    LogSource
	err       error // for use internally
}

// streamMode enum type
type streamMode int

const (
	streamModeUnknown streamMode = iota
	streamModeHead
	streamModeTail
	streamModeAll
)

// Stream represents a stream of log records
type Stream struct {
	sinceTime time.Time
	untilTime time.Time

	//reverse bool
	follow    bool
	grep      string
	grepRegex *regexp.Regexp

	rootCtx       context.Context
	rootCtxCancel context.CancelFunc

	mode    streamMode
	maxNum  int64
	sources set.Set[LogSource]

	maxChunkSize int

	kubeContext string
	sw          SourceWatcher
	logFetcher  LogFetcher

	isStarted bool
	futureWG  sync.WaitGroup
	pastCh    chan LogRecord
	futureCh  chan LogRecord
	outCh     chan LogRecord
	err       error
	mu        sync.Mutex

	closePastChOnce   sync.Once
	closeFutureChOnce sync.Once
	closeOutChOnce    sync.Once
	setErrorOnce      sync.Once
}

// Initialize new stream
func NewStream(ctx context.Context, cm k8shelpers.ConnectionManager, sourcePaths []string, opts ...Option) (*Stream, error) {
	rootCtx, rootCtxCancel := context.WithCancel(ctx)

	// Init stream instance
	stream := &Stream{
		rootCtx:       rootCtx,
		rootCtxCancel: rootCtxCancel,
		sources:       set.NewSet[LogSource](),
		maxChunkSize:  DEFAULT_MAX_CHUNK_SIZE,
		pastCh:        make(chan LogRecord),
		futureCh:      make(chan LogRecord),
		outCh:         make(chan LogRecord),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(stream); err != nil {
			return nil, err
		}
	}

	// Validate options
	if stream.follow && stream.mode == streamModeHead {
		return nil, fmt.Errorf("head and follow not allowed")
	}

	if stream.follow && !stream.untilTime.IsZero() && stream.untilTime.Before(time.Now()) {
		stream.follow = false
	}

	// Init source watcher
	sw, err := NewSourceWatcher(cm, sourcePaths, opts...)
	if err != nil {
		return nil, err
	}
	stream.sw = sw

	// Init log fetcher if not already set
	if stream.logFetcher == nil {
		clientset, err := cm.GetOrCreateClientset(stream.kubeContext)
		if err != nil {
			return nil, err
		}
		stream.logFetcher = NewKubeLogFetcher(clientset)
	}

	return stream, nil
}

// Start log fetchers and other background processes
// TODO: make this idempodent
func (s *Stream) Start(ctx context.Context) error {
	// Add source watcher event handlers
	s.sw.Subscribe(SourceWatcherEventAdded, s.handleSourceAdd)
	s.sw.Subscribe(SourceWatcherEventDeleted, s.handleSourceDelete)

	// Start source watcher
	if err := s.sw.Start(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize log streams
	s.sources = s.sw.Set()

	// Start past fetchers
	switch s.mode {
	case streamModeHead, streamModeAll:
		if err := s.startHead_UNSAFE(); err != nil {
			return err
		}
	default:
		if err := s.startTail_UNSAFE(); err != nil {
			return err
		}
	}

	// Start follow fetchers
	if s.follow {
		if err := s.startFollow_UNSAFE(); err != nil {
			return err
		}

		// Close future channel after writers have finished
		go func() {
			<-s.rootCtx.Done()
			s.futureWG.Wait()
			s.closeFutureCh()
		}()
	}

	// Run forwarder in background
	go s.runForwarder()

	// Update isStarted flag
	s.isStarted = true

	return nil
}

// Sources returns the stream's sources
func (s *Stream) Sources() []LogSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sources.ToSlice()
}

// Records returns the stream's output channel
func (s *Stream) Records() <-chan LogRecord {
	return s.outCh
}

// Err returns any error that occurred during stream processing
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops internal data fetchers and closes the output channel
func (s *Stream) Close() {
	// Remove source watcher event handlers
	s.sw.Unsubscribe(SourceWatcherEventAdded, s.handleSourceAdd)
	s.sw.Unsubscribe(SourceWatcherEventDeleted, s.handleSourceDelete)

	// Stop background processes
	s.rootCtxCancel()

	// Close output channel
	s.closeOutCh()
}

// Handle source ADDED event
func (s *Stream) handleSourceAdd(source LogSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check isStarted flag
	if !s.isStarted {
		return
	}

	// Exit if already exists
	if s.sources.ContainsOne(source) {
		return
	}

	// Stream from beginning and keep following
	opts := FetcherOptions{
		Grep:         s.grep,
		GrepRegex:    s.grepRegex,
		FollowFrom:   FollowFromDefault,
		MaxChunkSize: s.maxChunkSize,
	}

	stream, err := s.logFetcher.StreamForward(s.rootCtx, source, opts)
	if err != nil {
		s.setError_UNSAFE(err)
		return
	}

	// Forward records in goroutine
	s.futureWG.Add(1)
	go func() {
		defer s.futureWG.Done()

		for {
			select {
			case <-s.rootCtx.Done():
				return
			case record, ok := <-stream:
				if !ok {
					return
				}

				// Check for errors
				if record.err != nil {
					s.setError_SAFE(record.err)
					s.rootCtxCancel() // Kill the entire stream
					return
				}

				// Send record
				select {
				case <-s.rootCtx.Done():
					return
				case s.futureCh <- record:
					// Sent successfully
				}
			}
		}
	}()

	// Add to sources
	s.sources.Add(source)
}

// Handle source DELETED event
func (s *Stream) handleSourceDelete(source LogSource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check isStarted flag
	if !s.isStarted {
		return
	}

	// Remove from sources
	s.sources.Remove(source)
}

// Start fetching log records in `head` mode
func (s *Stream) startHead_UNSAFE() error {
	ctx, cancel := context.WithCancel(s.rootCtx)

	opts := FetcherOptions{
		StartTime:    s.sinceTime,
		StopTime:     s.untilTime,
		Grep:         s.grep,
		GrepRegex:    s.grepRegex,
		MaxChunkSize: s.maxChunkSize,
	}

	streams := make([]<-chan LogRecord, s.sources.Cardinality())
	for i, source := range s.sources.ToSlice() {
		stream, err := s.logFetcher.StreamForward(ctx, source, opts)
		if err != nil {
			cancel()
			return err
		}
		streams[i] = stream
	}

	// Process in goroutine
	go func() {
		defer s.closePastCh()
		defer cancel()

		N := int(s.maxNum)
		var count int

		for record := range mergeLogStreams(ctx, false, streams...) {
			// Handle errors
			if record.err != nil {
				s.setError_SAFE(record.err)
				return
			}

			// Write out
			select {
			case <-ctx.Done():
				return
			case s.pastCh <- record:
			}

			count += 1
			// Exit loop if we have enough records
			if s.mode != streamModeAll && count >= N {
				break
			}
		}
	}()

	return nil
}

// Start fetching log records in `tail` mode
func (s *Stream) startTail_UNSAFE() error {
	ctx, cancel := context.WithCancel(s.rootCtx)

	// Set batch size
	batchSize := s.maxNum
	if s.sinceTime.IsZero() {
		batchSize = 300
	}

	opts := FetcherOptions{
		StartTime:     s.sinceTime,
		StopTime:      s.untilTime,
		Grep:          s.grep,
		GrepRegex:     s.grepRegex,
		BatchSizeHint: batchSize,
		MaxChunkSize:  s.maxChunkSize,
	}

	streams := make([]<-chan LogRecord, s.sources.Cardinality())
	for i, source := range s.sources.ToSlice() {
		stream, err := s.logFetcher.StreamBackward(ctx, source, opts)
		if err != nil {
			cancel()
			return err
		}
		streams[i] = stream
	}

	// Process in goroutine
	go func() {
		defer s.closePastCh()
		defer cancel()

		N := int(s.maxNum)
		var count int
		tailRecords := []LogRecord{}

		for record := range mergeLogStreams(ctx, true, streams...) {
			// Handle errors
			if record.err != nil {
				s.setError_SAFE(record.err)
				return
			}

			tailRecords = append(tailRecords, record)
			count += 1

			if count >= N {
				break
			}
		}

		// Send the tail records in reverse order
		for i := len(tailRecords) - 1; i >= 0; i-- {
			select {
			case <-ctx.Done():
				return
			case s.pastCh <- tailRecords[i]:
			}
		}
	}()

	return nil
}

// Start following log records
func (s *Stream) startFollow_UNSAFE() error {
	ctx, cancel := context.WithCancel(s.rootCtx)

	var wg sync.WaitGroup

	opts := FetcherOptions{
		StopTime:     s.untilTime,
		Grep:         s.grep,
		GrepRegex:    s.grepRegex,
		FollowFrom:   FollowFromEnd,
		MaxChunkSize: s.maxChunkSize,
	}

	for _, source := range s.sources.ToSlice() {
		stream, err := s.logFetcher.StreamForward(ctx, source, opts)
		if err != nil {
			cancel() // stop all fetchers
			return err
		}

		// Forward records in goroutine
		wg.Add(1)
		s.futureWG.Add(1)
		go func(stream <-chan LogRecord) {
			defer s.futureWG.Done()
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case record, ok := <-stream:
					if !ok {
						return
					}

					// Check for errors
					if record.err != nil {
						s.setError_SAFE(record.err)
						s.rootCtxCancel()
						return
					}

					// Send record
					select {
					case <-ctx.Done():
						return
					case s.futureCh <- record:
						// Sent successfully
					}
				}
			}
		}(stream)
	}

	// Cancel context after goroutines exit
	go func() {
		wg.Wait()
		cancel()
	}()

	return nil
}

func (s *Stream) runForwarder() {
	defer s.closeOutCh()

	// Close output channel
	var buffer []LogRecord
	lastTSMap := make(map[LogSource]time.Time)

	// Forwarder
LOOP:
	for {
		select {
		case <-s.rootCtx.Done():
			return // exit
		case r, ok := <-s.pastCh:
			if !ok {
				break LOOP
			}

			// Save reference to last record
			lastTSMap[r.Source] = r.Timestamp

			// Write out
			select {
			case <-s.rootCtx.Done():
				return // exit
			case s.outCh <- r:
				// Sent successfully
			}
		case r, ok := <-s.futureCh:
			if !ok {
				return // exit
			}
			buffer = append(buffer, r)
		}
	}

	// Drain any pending future events that were queued before the past stream
	// finished. This ensures they are included in the sorted buffer below.
	drainCtx, cancelDrainCtx := context.WithTimeout(s.rootCtx, 50*time.Millisecond)
	defer cancelDrainCtx()

DrainLoop:
	for {
		select {
		case <-s.rootCtx.Done():
			return // exit
		case <-drainCtx.Done():
			break DrainLoop
		case r, ok := <-s.futureCh:
			if !ok {
				break DrainLoop
			}
			buffer = append(buffer, r)
		default:
			break DrainLoop
		}
	}

	// Exit if not following
	if !s.follow {
		return
	}

	// Step 2: sort the buffered events
	sort.Slice(buffer, func(i, j int) bool {
		return buffer[i].Timestamp.Before(buffer[j].Timestamp)
	})

	// Step 3: output sorted future events from the buffer
	for _, r := range buffer {
		// Skip if already sent
		lastTS, exists := lastTSMap[r.Source]
		if exists {
			if r.Timestamp.Before(lastTS) {
				continue
			}
			if r.Timestamp.Equal(lastTS) {
				delete(lastTSMap, r.Source)
				continue
			}
		}

		// Write out
		select {
		case <-s.rootCtx.Done():
			return // exit
		case s.outCh <- r:
			// Sent successfully
		}
	}
	buffer = nil // clear buffer

	// Step 4: any new future events now go directly to out
	// if futureEvents got closed earlier, the loop will just exit
	for r := range s.futureCh {
		// Write out
		select {
		case <-s.rootCtx.Done():
			return // exit
		case s.outCh <- r:
			// Sent successfully
		}
	}
}

// Set error and close channels if needed
func (s *Stream) setError_SAFE(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setError_UNSAFE(err)
}

// Set error and close channels if needed
func (s *Stream) setError_UNSAFE(err error) {
	s.setErrorOnce.Do(func() {
		s.err = err
	})
}

// Close past channel safely
func (s *Stream) closePastCh() {
	s.closePastChOnce.Do(func() {
		close(s.pastCh)
	})
}

// Close future channel safely
func (s *Stream) closeFutureCh() {
	s.closeFutureChOnce.Do(func() {
		close(s.futureCh)
	})
}

// Close output channel safely
func (s *Stream) closeOutCh() {
	s.closeOutChOnce.Do(func() {
		close(s.outCh)
	})
}

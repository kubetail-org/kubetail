// Copyright 2024-2025 Andres Morey
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
	"strings"
	"sync"
	"time"

	set "github.com/deckarep/golang-set/v2"
	zlog "github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// LogRecord represents a log record
type LogRecord struct {
	Source    LogSource
	Timestamp time.Time
	Message   string
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

	reverse bool
	follow  bool

	grep    *regexp.Regexp
	regions []string
	zones   []string
	oses    []string
	arches  []string
	nodes   []string

	rootCtx       context.Context
	rootCtxCancel context.CancelFunc

	mode             streamMode
	maxNum           int64
	sources          set.Set[LogSource]
	defaultNamespace string

	sw          SourceWatcher
	logProvider logProvider
	isStarted   bool
	futureWG    sync.WaitGroup
	pastCh      chan LogRecord
	futureCh    chan LogRecord
	outCh       chan LogRecord
	mu          sync.Mutex

	closePastChOnce   sync.Once
	closeFutureChOnce sync.Once
	closeOutChOnce    sync.Once
}

// Initialize new stream
func NewStream(ctx context.Context, clientset kubernetes.Interface, sourcePaths []string, opts ...StreamOption) (*Stream, error) {
	rootCtx, rootCtxCancel := context.WithCancel(ctx)

	// Init stream instance
	stream := &Stream{
		rootCtx:          rootCtx,
		rootCtxCancel:    rootCtxCancel,
		defaultNamespace: "default",
		sources:          set.NewSet[LogSource](),
		logProvider:      newK8sLogProvider(clientset),
		pastCh:           make(chan LogRecord),
		futureCh:         make(chan LogRecord),
		outCh:            make(chan LogRecord),
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
	cfg := &sourceWatcherConfig{
		DefaultNamespace: stream.defaultNamespace,
		Regions:          stream.regions,
		Zones:            stream.zones,
		Oses:             stream.oses,
		Arches:           stream.arches,
		Nodes:            stream.nodes,
	}

	sw, err := NewSourceWatcher(clientset, sourcePaths, cfg)
	if err != nil {
		return nil, err
	}
	stream.sw = sw

	return stream, nil
}

// Start log fetchers and other background processes
// TODO: make this idempodent
func (s *Stream) Start(ctx context.Context) error {
	// Add source watcher event handlers
	s.sw.Subscribe(watchEventAdded, s.handleSourceAdd)
	s.sw.Subscribe(watchEventDeleted, s.handleSourceDelete)

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

// Close stops internal data fetchers and closes the output channel
func (s *Stream) Close(ctx context.Context) error {
	// Remove source watcher event handlers
	s.sw.Unsubscribe(watchEventAdded, s.handleSourceAdd)
	s.sw.Unsubscribe(watchEventDeleted, s.handleSourceDelete)

	// Stop background processes
	s.rootCtxCancel()

	// Shutdown source watcher
	return s.sw.Shutdown(ctx)
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

	// Start following
	opts := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: ptr.To[int64](0),
	}

	stream, err := s.logProvider.GetLogs(s.rootCtx, source, opts)
	if err != nil {
		// Log error
		zlog.Error().Err(err).Send()
		return
	}

	// Forward records in goroutine
	s.futureWG.Add(1)
	go func() {
		defer s.futureWG.Done()

		for record := range stream {
			// Exit loop if record is after `untilTime`
			if !s.untilTime.IsZero() && record.Timestamp.After(s.untilTime) {
				break
			}

			// Skip records that don't match grep pattern
			if s.grep != nil && !s.grep.MatchString(record.Message) {
				continue
			}

			// Send record to future channel
			select {
			case <-s.rootCtx.Done():
				return
			case s.futureCh <- record:
				// Sent successfully
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

	streams := make([]<-chan LogRecord, s.sources.Cardinality())
	for i, source := range s.sources.ToSlice() {
		opts := &corev1.PodLogOptions{}
		if !s.sinceTime.IsZero() {
			opts.SinceTime = &metav1.Time{Time: s.sinceTime}
		}

		if stream, err := s.logProvider.GetLogs(ctx, source, opts); err != nil {
			cancel()
			return err
		} else {
			streams[i] = stream
		}
	}

	// Process in goroutine
	go func() {
		defer cancel()
		defer s.closePastCh()

		N := int(s.maxNum)
		var count int

		for record := range mergeLogStreams(ctx, false, streams...) {
			// Skip if records is before `sinceTime`
			if !s.sinceTime.IsZero() && record.Timestamp.Before(s.sinceTime) {
				continue
			}

			// Exit loop if record is after `untilTime`
			if !s.untilTime.IsZero() && record.Timestamp.After(s.untilTime) {
				break
			}

			// Skip records that don't match grep pattern
			if s.grep != nil && !s.grep.MatchString(record.Message) {
				continue
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

	streams := make([]<-chan LogRecord, s.sources.Cardinality())
	for i, source := range s.sources.ToSlice() {
		if stream, err := s.logProvider.GetLogsReverse(ctx, source, batchSize, s.sinceTime); err != nil {
			cancel()
			return err
		} else {
			streams[i] = stream
		}
	}

	// Process in goroutine
	go func() {
		defer cancel()
		defer s.closePastCh()

		N := int(s.maxNum)
		var count int
		tailRecords := []LogRecord{}

		for record := range mergeLogStreams(ctx, true, streams...) {
			// Skip records that are after `untilTime`
			if !s.untilTime.IsZero() && record.Timestamp.After(s.untilTime) {
				continue
			}

			// Exit loop if records are before `sinceTime`
			if !s.sinceTime.IsZero() && record.Timestamp.Before(s.sinceTime) {
				break
			}

			// Skip records that don't match grep pattern
			if s.grep != nil && !s.grep.MatchString(record.Message) {
				continue
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

	for _, source := range s.sources.ToSlice() {
		opts := &corev1.PodLogOptions{
			Follow:    true,
			TailLines: ptr.To[int64](0),
		}

		stream, err := s.logProvider.GetLogs(ctx, source, opts)
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

			for record := range stream {
				// Exit loop if record is after `untilTime`
				if !s.untilTime.IsZero() && record.Timestamp.After(s.untilTime) {
					break
				}

				// Skip records that don't match grep pattern
				if s.grep != nil && !s.grep.MatchString(record.Message) {
					continue
				}

				// Send record to future channel
				select {
				case <-ctx.Done():
					return
				case s.futureCh <- record:
					// Sent successfully
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

// Close past channel safely
func (s *Stream) closePastCh() {
	s.closePastChOnce.Do(func() {
		close(s.pastCh)
	})
}

// Close future channel safely
func (s *Stream) closeFutureCh() {
	s.closeFutureChOnce.Do(func() {
		if s.futureCh != nil {
			close(s.futureCh)
		}
		s.futureCh = nil
	})
}

// Close output channel safely
func (s *Stream) closeOutCh() {
	s.closeOutChOnce.Do(func() {
		close(s.outCh)
	})
}

// Option defines signature for functional options
type StreamOption func(s *Stream) error

// WithDefaultNamespace sets the default namespace for use with the source path parser
func WithDefaultNamespace(namespace string) StreamOption {
	return func(s *Stream) error {
		s.defaultNamespace = namespace
		return nil
	}
}

// WithKubeContext sets the kube context of the stream
func WithKubeContext(kubeContext string) StreamOption {
	return func(s *Stream) error {
		//s.kubeContext = kubeContext
		return nil
	}
}

// WithSince sets the since time for the stream
func WithSince(ts time.Time) StreamOption {
	return func(s *Stream) error {
		s.sinceTime = ts
		return nil
	}
}

// WithUntil sets the until time for the stream
func WithUntil(ts time.Time) StreamOption {
	return func(s *Stream) error {
		s.untilTime = ts
		return nil
	}
}

// WithReverse sets whether to return logs in reverse order
func WithReverse(reverse bool) StreamOption {
	return func(s *Stream) error {
		s.reverse = reverse
		return nil
	}
}

// WithFollow sets whether to follow/tail the log stream
func WithFollow(follow bool) StreamOption {
	return func(s *Stream) error {
		s.follow = follow
		return nil
	}
}

// WithHead sets the number of lines to return from the beginning
func WithHead(n int64) StreamOption {
	return func(s *Stream) error {
		if n < 0 {
			return fmt.Errorf("head must be >= 0")
		}
		s.mode = streamModeHead
		s.maxNum = n
		return nil
	}
}

// WithTail sets the number of lines to return from the end
func WithTail(n int64) StreamOption {
	return func(s *Stream) error {
		if n < 0 {
			return fmt.Errorf("tail must be >= 0")
		}
		s.mode = streamModeTail
		s.maxNum = n
		return nil
	}
}

// WithAll sets whether to return all logs
func WithAll() StreamOption {
	return func(s *Stream) error {
		s.mode = streamModeAll
		return nil
	}
}

// WithGrep sets the grep filter for the stream
func WithGrep(pattern string) StreamOption {
	return func(s *Stream) error {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			s.grep = nil
			return nil
		}

		// Replace spaces with ANSI-tolerant pattern
		pattern = strings.ReplaceAll(pattern, " ", `(?:(?:\x1B\[[0-9;]*[mK])?)*\s(?:(?:\x1B\[[0-9;]*[mK])?)*`)

		// Compile the regex pattern
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid grep pattern: %w", err)
		}

		s.grep = regex
		return nil
	}
}

// WithRegion sets the region filters for the stream
func WithRegions(regions []string) StreamOption {
	return func(s *Stream) error {
		s.regions = regions
		return nil
	}
}

// WithZones sets the zone filters for the stream
func WithZones(zones []string) StreamOption {
	return func(s *Stream) error {
		s.zones = zones
		return nil
	}
}

// WithOS sets the operating system filters for the stream
func WithOSes(oses []string) StreamOption {
	return func(s *Stream) error {
		s.oses = oses
		return nil
	}
}

// WithArch sets the architecture filters for the stream
func WithArches(arches []string) StreamOption {
	return func(s *Stream) error {
		s.arches = arches
		return nil
	}
}

// WithNode sets the node filters for the stream
func WithNodes(nodes []string) StreamOption {
	return func(s *Stream) error {
		s.nodes = nodes
		return nil
	}
}

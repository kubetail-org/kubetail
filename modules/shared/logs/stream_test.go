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
	"errors"
	"slices"
	"testing"
	"time"

	set "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
)

// filterRecords filters a slice of LogRecords by start and stop times
func filterRecords(records []LogRecord, sinceTime time.Time, untilTime time.Time) []LogRecord {
	filteredRecords := []LogRecord{}
	for _, r := range records {
		if !sinceTime.IsZero() && r.Timestamp.Before(sinceTime) {
			continue
		}
		if !untilTime.IsZero() && r.Timestamp.After(untilTime) {
			continue
		}
		filteredRecords = append(filteredRecords, r)
	}
	return filteredRecords
}

// newForwardChannel creates a channel of LogRecords filtered by start and stop times
func newForwardChannel(records []LogRecord, sinceTime time.Time, untilTime time.Time) <-chan LogRecord {
	filteredRecords := filterRecords(records, sinceTime, untilTime)

	ch := make(chan LogRecord, len(filteredRecords))
	for _, log := range filteredRecords {
		ch <- log
	}

	close(ch)
	return ch
}

// newBackwardChannel creates a channel of LogRecords filtered by start and stop times
func newBackwardChannel(records []LogRecord, sinceTime time.Time, untilTime time.Time) <-chan LogRecord {
	filteredRecords := filterRecords(records, sinceTime, untilTime)
	slices.Reverse(filteredRecords)

	ch := make(chan LogRecord, len(filteredRecords))
	for _, log := range filteredRecords {
		ch <- log
	}

	close(ch)
	return ch
}

// mockLogFetcher implements LogFetcher for testing
type mockLogFetcher struct {
	mock.Mock
}

func (m *mockLogFetcher) StreamForward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	ret := m.Called(ctx, source, opts)

	var r0 <-chan LogRecord
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(<-chan LogRecord)
	}

	return r0, ret.Error(1)
}

func (m *mockLogFetcher) StreamBackward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	ret := m.Called(ctx, source, opts)

	var r0 <-chan LogRecord
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(<-chan LogRecord)
	}

	return r0, ret.Error(1)
}

// mockSourceWatcher implements sourceWatcher for testing
type mockSourceWatcher struct {
	mock.Mock
}

func (m *mockSourceWatcher) Start(ctx context.Context) error {
	ret := m.Called(ctx)
	return ret.Error(0)
}

func (m *mockSourceWatcher) Set() set.Set[LogSource] {
	ret := m.Called()

	var r0 set.Set[LogSource]
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(set.Set[LogSource])
	}

	return r0
}

func (m *mockSourceWatcher) Subscribe(event SourceWatcherEvent, fn any) {
	m.Called(event, fn)
}

func (m *mockSourceWatcher) Unsubscribe(event SourceWatcherEvent, fn any) {
	m.Called(event, fn)
}

func (m *mockSourceWatcher) Close() {
	m.Called()
}

func TestStreamHead(t *testing.T) {
	s1 := LogSource{Namespace: "ns1", PodName: "pod1", ContainerName: "container1"}
	s2 := LogSource{Namespace: "ns2", PodName: "pod2", ContainerName: "container2"}

	t1 := time.Date(2025, 3, 13, 11, 46, 1, 123456789, time.UTC)
	t2 := time.Date(2025, 3, 13, 11, 46, 2, 123456789, time.UTC)
	t3 := time.Date(2025, 3, 13, 11, 46, 3, 123456789, time.UTC)
	t4 := time.Date(2025, 3, 13, 11, 46, 4, 123456789, time.UTC)
	t5 := time.Date(2025, 3, 13, 11, 46, 5, 123456789, time.UTC)
	t6 := time.Date(2025, 3, 13, 11, 46, 6, 123456789, time.UTC)

	logs1 := []LogRecord{
		{Source: s1, Timestamp: t1, Message: "s1-a"},
		{Source: s1, Timestamp: t3, Message: "s1-b"},
		{Source: s1, Timestamp: t5, Message: "s1-c"},
	}

	logs2 := []LogRecord{
		{Source: s2, Timestamp: t2, Message: "s2-a"},
		{Source: s2, Timestamp: t4, Message: "s2-b"},
		{Source: s2, Timestamp: t6, Message: "s2-c"},
	}

	tests := []struct {
		name         string
		setMode      streamMode
		setSinceTime time.Time
		setUntilTime time.Time
		setMaxNum    int64
		wantLines    []string
	}{
		{
			"all mode",
			streamModeAll,
			time.Time{},
			time.Time{},
			0,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"head mode with maxNum less than number of records",
			streamModeHead,
			time.Time{},
			time.Time{},
			3,
			[]string{"s1-a", "s2-a", "s1-b"},
		},
		{
			"head mode with maxNum same as number of records",
			streamModeHead,
			time.Time{},
			time.Time{},
			6,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"head mode with maxNum same as number of records",
			streamModeHead,
			time.Time{},
			time.Time{},
			9,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"head mode with sinceTime",
			streamModeHead,
			t3,
			time.Time{},
			2,
			[]string{"s1-b", "s2-b"},
		},
		{
			"head mode with untilTime",
			streamModeHead,
			time.Time{},
			t4,
			6,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b"},
		},
		{
			"head mode with sinceTime and untilTime",
			streamModeHead,
			t2,
			t5,
			6,
			[]string{"s2-a", "s1-b", "s2-b", "s1-c"},
		},
		{
			"head mode with sinceTime, untilTime and maxNum",
			streamModeHead,
			t2,
			t5,
			3,
			[]string{"s2-a", "s1-b", "s2-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Init mock logFetcher
			m := mockLogFetcher{}

			var capturedCtx1 context.Context
			var capturedCtx2 context.Context

			ch1 := newForwardChannel(logs1, tt.setSinceTime, tt.setUntilTime)
			ch2 := newForwardChannel(logs2, tt.setSinceTime, tt.setUntilTime)

			m.On("StreamForward", mock.Anything, s1, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx1 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch1), nil)

			m.On("StreamForward", mock.Anything, s2, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx2 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch2), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Init connection manager
			cm := &k8shelpersmock.MockConnectionManager{}
			cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
			cm.On("GetDefaultNamespace", mock.Anything).Return("default")

			// Create stream
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			opts := []Option{
				WithSince(tt.setSinceTime),
				WithUntil(tt.setUntilTime),
			}

			if tt.setMode == streamModeAll {
				opts = append(opts, WithAll())
			} else {
				opts = append(opts, WithHead(tt.setMaxNum))
			}

			stream, err := NewStream(ctx, cm, []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close()

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logFetcher = &m

			// Start background processes
			err = stream.Start(context.Background())
			require.NoError(t, err)

			// Get log records
			messages := []string{}
			for r := range stream.Records() {
				messages = append(messages, r.Message)
			}
			assert.Equal(t, tt.wantLines, messages)

			// Check sinceTime option
			fetcherOpts := FetcherOptions{
				StartTime:       tt.setSinceTime,
				StopTime:        tt.setUntilTime,
				TruncateAtBytes: DEFAULT_TRUNCATE_AT_BYTES,
			}

			m.AssertCalled(t, "StreamForward", mock.Anything, s1, fetcherOpts)
			m.AssertCalled(t, "StreamForward", mock.Anything, s2, fetcherOpts)

			// Check that context was canceled
			assert.Equal(t, context.Canceled, capturedCtx1.Err())
			assert.Equal(t, context.Canceled, capturedCtx2.Err())
		})
	}
}

func TestStreamTail(t *testing.T) {
	s1 := LogSource{Namespace: "ns1", PodName: "pod1", ContainerName: "container1"}
	s2 := LogSource{Namespace: "ns2", PodName: "pod2", ContainerName: "container2"}

	t1 := time.Date(2025, 3, 13, 11, 46, 1, 123456789, time.UTC)
	t2 := time.Date(2025, 3, 13, 11, 46, 2, 123456789, time.UTC)
	t3 := time.Date(2025, 3, 13, 11, 46, 3, 123456789, time.UTC)
	t4 := time.Date(2025, 3, 13, 11, 46, 4, 123456789, time.UTC)
	t5 := time.Date(2025, 3, 13, 11, 46, 5, 123456789, time.UTC)
	t6 := time.Date(2025, 3, 13, 11, 46, 6, 123456789, time.UTC)

	logs1 := []LogRecord{
		{Source: s1, Timestamp: t1, Message: "s1-a"},
		{Source: s1, Timestamp: t3, Message: "s1-b"},
		{Source: s1, Timestamp: t5, Message: "s1-c"},
	}

	logs2 := []LogRecord{
		{Source: s2, Timestamp: t2, Message: "s2-a"},
		{Source: s2, Timestamp: t4, Message: "s2-b"},
		{Source: s2, Timestamp: t6, Message: "s2-c"},
	}

	tests := []struct {
		name         string
		setSinceTime time.Time
		setUntilTime time.Time
		setMaxNum    int64
		wantLines    []string
	}{
		{
			"with maxNum less than number of records",
			time.Time{},
			time.Time{},
			3,
			[]string{"s2-b", "s1-c", "s2-c"},
		},
		{
			"with maxNum same as number of records",
			time.Time{},
			time.Time{},
			6,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"with maxNum greater than number of records",
			time.Time{},
			time.Time{},
			9,
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"with sinceTime",
			t3,
			time.Time{},
			2,
			[]string{"s1-c", "s2-c"},
		},
		{
			"with untilTime",
			time.Time{},
			t4,
			2,
			[]string{"s1-b", "s2-b"},
		},
		{
			"with sinceTime and untilTime",
			t2,
			t5,
			6,
			[]string{"s2-a", "s1-b", "s2-b", "s1-c"},
		},
		{
			"with sinceTime, untilTime and maxNum",
			t2,
			t5,
			3,
			[]string{"s1-b", "s2-b", "s1-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Init mock logFetcher
			m := mockLogFetcher{}

			var capturedCtx1 context.Context
			var capturedCtx2 context.Context

			ch1 := newBackwardChannel(logs1, tt.setSinceTime, tt.setUntilTime)
			ch2 := newBackwardChannel(logs2, tt.setSinceTime, tt.setUntilTime)

			m.On("StreamBackward", mock.Anything, s1, mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx1 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch1), nil)
			m.On("StreamBackward", mock.Anything, s2, mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx2 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch2), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Init connection manager
			cm := &k8shelpersmock.MockConnectionManager{}
			cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
			cm.On("GetDefaultNamespace", mock.Anything).Return("default")

			// Create stream
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			opts := []Option{
				WithSince(tt.setSinceTime),
				WithUntil(tt.setUntilTime),
				WithTail(tt.setMaxNum),
			}

			stream, err := NewStream(ctx, cm, []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close()

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logFetcher = &m

			// Start background processes
			err = stream.Start(context.Background())
			require.NoError(t, err)

			// Get log records
			messages := []string{}
			for r := range stream.Records() {
				messages = append(messages, r.Message)
			}
			assert.Equal(t, tt.wantLines, messages)

			// Check that context was canceled
			assert.Equal(t, context.Canceled, capturedCtx1.Err())
			assert.Equal(t, context.Canceled, capturedCtx2.Err())
		})
	}
}

func TestStreamAllWithFollow(t *testing.T) {
	s1 := LogSource{Namespace: "ns1", PodName: "pod1", ContainerName: "container1"}
	s2 := LogSource{Namespace: "ns2", PodName: "pod2", ContainerName: "container2"}

	t1 := time.Date(2025, 3, 13, 11, 46, 1, 123456789, time.UTC)
	t2 := time.Date(2025, 3, 13, 11, 46, 2, 123456789, time.UTC)
	t3 := time.Date(2025, 3, 13, 11, 46, 3, 123456789, time.UTC)
	t4 := time.Date(2025, 3, 13, 11, 46, 4, 123456789, time.UTC)
	t5 := time.Date(2025, 3, 13, 11, 46, 5, 123456789, time.UTC)
	t6 := time.Date(2025, 3, 13, 11, 46, 6, 123456789, time.UTC)

	logs1 := []LogRecord{
		{Source: s1, Timestamp: t1, Message: "s1-a"},
		{Source: s1, Timestamp: t3, Message: "s1-b"},
		{Source: s1, Timestamp: t5, Message: "s1-c"},
	}

	logs2 := []LogRecord{
		{Source: s2, Timestamp: t2, Message: "s2-a"},
		{Source: s2, Timestamp: t4, Message: "s2-b"},
		{Source: s2, Timestamp: t6, Message: "s2-c"},
	}

	tests := []struct {
		name                 string
		setPastStreamBefore1 []LogRecord
		setPastStreamBefore2 []LogRecord
		setPastStreamAfter1  []LogRecord
		setPastStreamAfter2  []LogRecord
		setFutureStream      []LogRecord
		wantLines            []string
	}{
		{
			"no follow data, past data arrives before",
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]string{"s1-a", "s2-a"},
		},
		{
			"no follow data, past data arrives after",
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{},
			[]string{"s1-a", "s2-a"},
		},
		{
			"no follow data, past data arrives before and after",
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{logs1[1]},
			[]LogRecord{logs2[1]},
			[]LogRecord{},
			[]string{"s1-a", "s2-a", "s1-b", "s2-b"},
		},
		{
			"with follow data, past data arrives before",
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[1]},
			[]string{"s1-a", "s2-a", "s1-b"},
		},
		{
			"with follow data, past data arrives after",
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{logs1[1], logs2[1]},
			[]string{"s1-a", "s2-a", "s1-b", "s2-b"},
		},
		{
			"with follow data, past data arrives before and after",
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{logs1[1]},
			[]LogRecord{logs2[1]},
			[]LogRecord{logs1[2], logs2[2]},
			[]string{"s1-a", "s2-a", "s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"with follow data, no past data",
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0], logs1[1]},
			[]string{"s1-a", "s1-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			numPast1 := len(tt.setPastStreamBefore1) + len(tt.setPastStreamAfter1)
			numPast2 := len(tt.setPastStreamBefore2) + len(tt.setPastStreamAfter2)

			// Init old data channels
			ch1Old := make(chan LogRecord, numPast1)
			ch2Old := make(chan LogRecord, numPast2)

			// Send past data to channels
			for _, r := range tt.setPastStreamBefore1 {
				ch1Old <- r
			}
			for _, r := range tt.setPastStreamBefore2 {
				ch2Old <- r
			}

			// Close channels if no more data
			if len(tt.setPastStreamAfter1) == 0 {
				close(ch1Old)
			}
			if len(tt.setPastStreamAfter2) == 0 {
				close(ch2Old)
			}

			// Init new data channels
			ch1New := make(chan LogRecord)
			ch2New := make(chan LogRecord)
			defer close(ch1New)
			defer close(ch2New)

			// Init mock logProvider
			optsOld := FetcherOptions{TruncateAtBytes: DEFAULT_TRUNCATE_AT_BYTES}
			optsNew := FetcherOptions{
				FollowFrom:      FollowFromEnd,
				TruncateAtBytes: DEFAULT_TRUNCATE_AT_BYTES,
			}

			m := mockLogFetcher{}
			m.On("StreamForward", mock.Anything, s1, optsOld).
				Return((<-chan LogRecord)(ch1Old), nil)
			m.On("StreamForward", mock.Anything, s1, optsNew).
				Return((<-chan LogRecord)(ch1New), nil)
			m.On("StreamForward", mock.Anything, s2, optsOld).
				Return((<-chan LogRecord)(ch2Old), nil)
			m.On("StreamForward", mock.Anything, s2, optsNew).
				Return((<-chan LogRecord)(ch2New), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Init connection manager
			cm := &k8shelpersmock.MockConnectionManager{}
			cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
			cm.On("GetDefaultNamespace", mock.Anything).Return("default")

			// Create stream
			opts := []Option{
				WithAll(),
				WithFollow(true),
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			stream, err := NewStream(ctx, cm, []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close()

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logFetcher = &m

			// Start background processes
			err = stream.Start(context.Background())
			require.NoError(t, err)

			// Get log records in goroutine
			messages := []string{}

			doneCh := make(chan struct{})
			go func() {
				defer close(doneCh)

				for r := range stream.Records() {
					messages = append(messages, r.Message)

					// Exit after expected number of messages arrives
					if len(messages) == len(tt.wantLines) {
						break
					}
				}
			}()

			// Send future data
			for _, r := range tt.setFutureStream {
				switch r.Source {
				case s1:
					ch1New <- r
				case s2:
					ch2New <- r
				}
			}

			// Send past data
			for _, r := range tt.setPastStreamAfter1 {
				ch1Old <- r
			}
			for _, r := range tt.setPastStreamAfter2 {
				ch2Old <- r
			}

			// Close channels if still open
			if len(tt.setPastStreamAfter1) > 0 {
				close(ch1Old)
			}
			if len(tt.setPastStreamAfter2) > 0 {
				close(ch2Old)
			}

			// Wait for all messages to arrive
			<-doneCh

			// Check result
			assert.Equal(t, tt.wantLines, messages)
		})
	}
}

func TestStreamTailWithFollow(t *testing.T) {
	s1 := LogSource{Namespace: "ns1", PodName: "pod1", ContainerName: "container1"}
	s2 := LogSource{Namespace: "ns2", PodName: "pod2", ContainerName: "container2"}

	t1 := time.Date(2025, 3, 13, 11, 46, 1, 123456789, time.UTC)
	t2 := time.Date(2025, 3, 13, 11, 46, 2, 123456789, time.UTC)
	t3 := time.Date(2025, 3, 13, 11, 46, 3, 123456789, time.UTC)
	t4 := time.Date(2025, 3, 13, 11, 46, 4, 123456789, time.UTC)
	t5 := time.Date(2025, 3, 13, 11, 46, 5, 123456789, time.UTC)
	t6 := time.Date(2025, 3, 13, 11, 46, 6, 123456789, time.UTC)

	logs1 := []LogRecord{
		{Source: s1, Timestamp: t1, Message: "s1-a"},
		{Source: s1, Timestamp: t3, Message: "s1-b"},
		{Source: s1, Timestamp: t5, Message: "s1-c"},
	}

	logs2 := []LogRecord{
		{Source: s2, Timestamp: t2, Message: "s2-a"},
		{Source: s2, Timestamp: t4, Message: "s2-b"},
		{Source: s2, Timestamp: t6, Message: "s2-c"},
	}

	tests := []struct {
		name                 string
		setTailNum           int64
		setPastStreamBefore1 []LogRecord
		setPastStreamBefore2 []LogRecord
		setPastStreamAfter1  []LogRecord
		setPastStreamAfter2  []LogRecord
		setFutureStream      []LogRecord
		wantLines            []string
	}{
		{
			"no follow data, past data arrives before",
			2,
			[]LogRecord{logs1[0], logs1[1]},
			[]LogRecord{logs2[0], logs2[1]},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]string{"s1-b", "s2-b"},
		},
		{
			"no follow data, past data arrives after",
			2,
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0], logs1[1]},
			[]LogRecord{logs2[0], logs2[1]},
			[]LogRecord{},
			[]string{"s1-b", "s2-b"},
		},
		{
			"no follow data, past data arrives before and after",
			2,
			[]LogRecord{logs1[1]},
			[]LogRecord{logs2[1]},
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{},
			[]string{"s1-b", "s2-b"},
		},
		{
			"with follow data, past data arrives before",
			2,
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[1]},
			[]string{"s1-a", "s2-a", "s1-b"},
		},
		{
			"with follow data, past data arrives after",
			2,
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{logs1[1], logs2[1]},
			[]string{"s1-a", "s2-a", "s1-b", "s2-b"},
		},
		{
			"with follow data, past data arrives before and after",
			2,
			[]LogRecord{logs1[1]},
			[]LogRecord{logs2[1]},
			[]LogRecord{logs1[0]},
			[]LogRecord{logs2[0]},
			[]LogRecord{logs1[2], logs2[2]},
			[]string{"s1-b", "s2-b", "s1-c", "s2-c"},
		},
		{
			"with follow data, no past data",
			2,
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{},
			[]LogRecord{logs1[0], logs1[1]},
			[]string{"s1-a", "s1-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			numPast1 := len(tt.setPastStreamBefore1) + len(tt.setPastStreamAfter1)
			numPast2 := len(tt.setPastStreamBefore2) + len(tt.setPastStreamAfter2)

			// Init old data channels
			ch1Old := make(chan LogRecord, numPast1)
			ch2Old := make(chan LogRecord, numPast2)

			// Send past data to channels
			for i := len(tt.setPastStreamBefore1) - 1; i >= 0; i-- {
				ch1Old <- tt.setPastStreamBefore1[i]
			}
			for i := len(tt.setPastStreamBefore2) - 1; i >= 0; i-- {
				ch2Old <- tt.setPastStreamBefore2[i]
			}

			// Close channels if no more data
			if len(tt.setPastStreamAfter1) == 0 {
				close(ch1Old)
			}
			if len(tt.setPastStreamAfter2) == 0 {
				close(ch2Old)
			}

			// Init new data channels
			ch1New := make(chan LogRecord)
			ch2New := make(chan LogRecord)
			defer close(ch1New)
			defer close(ch2New)

			// Init mock logProvider
			optsNew := FetcherOptions{
				FollowFrom:      FollowFromEnd,
				TruncateAtBytes: DEFAULT_TRUNCATE_AT_BYTES,
			}

			m := mockLogFetcher{}
			m.On("StreamBackward", mock.Anything, s1, mock.Anything, mock.Anything).
				Return((<-chan LogRecord)(ch1Old), nil)
			m.On("StreamForward", mock.Anything, s1, optsNew).
				Return((<-chan LogRecord)(ch1New), nil)
			m.On("StreamBackward", mock.Anything, s2, mock.Anything, mock.Anything).
				Return((<-chan LogRecord)(ch2Old), nil)
			m.On("StreamForward", mock.Anything, s2, optsNew).
				Return((<-chan LogRecord)(ch2New), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Init connection manager
			cm := &k8shelpersmock.MockConnectionManager{}
			cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
			cm.On("GetDefaultNamespace", mock.Anything).Return("default")

			// Create stream
			opts := []Option{
				WithTail(tt.setTailNum),
				WithFollow(true),
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			stream, err := NewStream(ctx, cm, []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close()

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logFetcher = &m

			// Start background processes
			err = stream.Start(context.Background())
			require.NoError(t, err)

			// Get log records in goroutine
			messages := []string{}

			doneCh := make(chan struct{})
			go func() {
				defer close(doneCh)

				for r := range stream.Records() {
					messages = append(messages, r.Message)

					// Exit after expected number of messages arrives
					if len(messages) == len(tt.wantLines) {
						break
					}
				}
			}()

			// Send future data
			for _, r := range tt.setFutureStream {
				switch r.Source {
				case s1:
					ch1New <- r
				case s2:
					ch2New <- r
				}
			}

			// Send past data
			for i := len(tt.setPastStreamAfter1) - 1; i >= 0; i-- {
				ch1Old <- tt.setPastStreamAfter1[i]
			}
			for i := len(tt.setPastStreamAfter2) - 1; i >= 0; i-- {
				ch2Old <- tt.setPastStreamAfter2[i]
			}

			// Close channels if still open
			if len(tt.setPastStreamAfter1) > 0 {
				close(ch1Old)
			}
			if len(tt.setPastStreamAfter2) > 0 {
				close(ch2Old)
			}

			// Wait for all messages to arrive
			<-doneCh

			// Check result
			assert.Equal(t, tt.wantLines, messages)
		})
	}
}

func TestStreamErrorHandling(t *testing.T) {
	t.Run("Test error handling", func(t *testing.T) {
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
		cm.On("GetDefaultNamespace", mock.Anything).Return("default")

		stream, err := NewStream(context.Background(), cm, []string{})
		require.NoError(t, err)
		stream.Close()

		// Initially there should be no error
		require.Nil(t, stream.Err())
	})

	t.Run("Test error from stream init", func(t *testing.T) {
		// Create a mock log provider that returns an error
		expectedError := errors.New("test error from log provider")
		m := &mockLogFetcher{}

		// Setup the mock to return an error when GetLogs is called
		source := LogSource{Namespace: "test-ns", PodName: "test-pod", ContainerName: "test-container"}
		m.On("StreamForward", mock.Anything, source, mock.Anything).Return(nil, expectedError)

		// Create a mock source watcher
		sw := &mockSourceWatcher{}
		sw.On("Start", mock.Anything).Return(nil)
		sw.On("Set").Return(set.NewSet[LogSource]())
		sw.On("Subscribe", mock.Anything, mock.Anything).Return()
		sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()

		// Create a connection manager mock
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
		cm.On("GetDefaultNamespace", mock.Anything).Return("default")

		// Create the stream
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream, err := NewStream(ctx, cm, []string{}, WithHead(10))
		require.NoError(t, err)
		defer stream.Close()

		// Override the log provider and source watcher
		stream.logFetcher = m
		stream.sw = sw

		// Start the stream
		err = stream.Start(context.Background())
		require.NoError(t, err)

		// Add source
		stream.handleSourceAdd(source)

		// Wait a bit for error to be processed
		<-stream.Records()

		// Check error
		assert.NotNil(t, stream.Err())
		assert.Contains(t, stream.Err().Error(), expectedError.Error())
	})

	t.Run("Test error from stream record - head mode", func(t *testing.T) {
		// Create a mock log provider that returns an error
		expectedError := errors.New("test error from stream")

		// Create mock stream
		mockStream := make(chan LogRecord, 1)
		mockStream <- LogRecord{err: expectedError}
		defer close(mockStream)

		// Convert to receive-only channel to match the expected return type
		var readOnlyStream <-chan LogRecord = mockStream

		// Setup the mock to return an error when GetLogs is called
		source := LogSource{Namespace: "test-ns", PodName: "test-pod", ContainerName: "test-container"}
		m := &mockLogFetcher{}
		m.On("StreamForward", mock.Anything, source, mock.Anything).Return(readOnlyStream, nil)

		// Create a mock source watcher
		sw := &mockSourceWatcher{}
		sw.On("Start", mock.Anything).Return(nil)
		sw.On("Set").Return(set.NewSet(source))
		sw.On("Subscribe", mock.Anything, mock.Anything).Return()
		sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()

		// Create a connection manager mock
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
		cm.On("GetDefaultNamespace", mock.Anything).Return("default")

		// Create the stream
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream, err := NewStream(ctx, cm, []string{}, WithHead(10))
		require.NoError(t, err)
		defer stream.Close()

		// Override the log provider and source watcher
		stream.logFetcher = m
		stream.sw = sw

		// Start the stream
		err = stream.Start(context.Background())
		require.NoError(t, err)

		// Wait for error to be processed
		<-stream.Records()

		// Check error
		assert.NotNil(t, stream.Err())
		assert.Contains(t, stream.Err().Error(), expectedError.Error())
	})

	t.Run("Test error from stream record - tail mode", func(t *testing.T) {
		// Create a mock log provider that returns an error
		expectedError := errors.New("test error from stream")

		// Create mock stream
		mockStream := make(chan LogRecord, 1)
		mockStream <- LogRecord{err: expectedError}
		defer close(mockStream)

		// Convert to receive-only channel to match the expected return type
		var readOnlyStream <-chan LogRecord = mockStream

		// Setup the mock to return an error when GetLogs is called
		source := LogSource{Namespace: "test-ns", PodName: "test-pod", ContainerName: "test-container"}
		m := &mockLogFetcher{}
		m.On("StreamBackward", mock.Anything, source, mock.Anything).Return(readOnlyStream, nil)

		// Create a mock source watcher
		sw := &mockSourceWatcher{}
		sw.On("Start", mock.Anything).Return(nil)
		sw.On("Set").Return(set.NewSet(source))
		sw.On("Subscribe", mock.Anything, mock.Anything).Return()
		sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()

		// Create a connection manager mock
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
		cm.On("GetDefaultNamespace", mock.Anything).Return("default")

		// Create the stream
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream, err := NewStream(ctx, cm, []string{}, WithTail(10))
		require.NoError(t, err)
		defer stream.Close()

		// Override the log provider and source watcher
		stream.logFetcher = m
		stream.sw = sw

		// Start the stream
		err = stream.Start(context.Background())
		require.NoError(t, err)

		// Wait for error to be processed
		<-stream.Records()

		// Check error
		assert.NotNil(t, stream.Err())
		assert.Contains(t, stream.Err().Error(), expectedError.Error())
	})

	t.Run("Test error from stream record - follow mode", func(t *testing.T) {
		// Create a mock log provider that returns an error
		expectedError := errors.New("test error from stream")

		// Create mock stream
		mockStream := make(chan LogRecord, 1)
		mockStream <- LogRecord{err: expectedError}
		defer close(mockStream)

		// Convert to receive-only channel to match the expected return type
		var readOnlyStream <-chan LogRecord = mockStream

		// Setup the mock to return an error when GetLogs is called
		source := LogSource{Namespace: "test-ns", PodName: "test-pod", ContainerName: "test-container"}
		m := &mockLogFetcher{}
		m.On("StreamForward", mock.Anything, source, mock.Anything).Return(readOnlyStream, nil)

		// Create a mock source watcher
		sw := &mockSourceWatcher{}
		sw.On("Start", mock.Anything).Return(nil)
		sw.On("Set").Return(set.NewSet[LogSource]())
		sw.On("Subscribe", mock.Anything, mock.Anything).Return()
		sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()

		// Create a connection manager mock
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
		cm.On("GetDefaultNamespace", mock.Anything).Return("default")

		// Create the stream
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream, err := NewStream(ctx, cm, []string{}, WithFollow(true))
		require.NoError(t, err)
		defer stream.Close()

		// Override the log provider and source watcher
		stream.logFetcher = m
		stream.sw = sw

		// Start the stream
		err = stream.Start(context.Background())
		require.NoError(t, err)

		// Add source
		stream.handleSourceAdd(source)

		// Wait for error to be processed
		<-stream.Records()

		// Check error
		assert.NotNil(t, stream.Err())
		assert.Contains(t, stream.Err().Error(), expectedError.Error())
	})
}

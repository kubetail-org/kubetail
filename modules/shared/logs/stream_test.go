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
	"testing"
	"time"

	set "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// mockLogProvider implements logProvider for testing
type mockLogProvider struct {
	mock.Mock
}

func (m *mockLogProvider) GetLogs(ctx context.Context, source LogSource, opts *corev1.PodLogOptions) (<-chan LogRecord, error) {
	ret := m.Called(ctx, source, opts)

	var r0 <-chan LogRecord
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(<-chan LogRecord)
	}

	return r0, ret.Error(1)
}

func (m *mockLogProvider) GetLogsReverse(ctx context.Context, source LogSource, batchSize int64, sinceTime time.Time) (<-chan LogRecord, error) {
	ret := m.Called(ctx, source, batchSize, sinceTime)

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

func (m *mockSourceWatcher) Subscribe(event watchEvent, fn any) {
	m.Called(event, fn)
}

func (m *mockSourceWatcher) Unsubscribe(event watchEvent, fn any) {
	m.Called(event, fn)
}

func (m *mockSourceWatcher) Shutdown(ctx context.Context) error {
	ret := m.Called(ctx)
	return ret.Error(0)
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

	logs := []LogRecord{
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
			// Init mock logProvider
			m := mockLogProvider{}

			var capturedCtx1 context.Context
			var capturedCtx2 context.Context

			ch1 := make(chan LogRecord, len(logs1))
			for _, r := range logs1 {
				ch1 <- r
			}
			close(ch1)

			ch2 := make(chan LogRecord, len(logs))
			for _, r := range logs {
				ch2 <- r
			}
			close(ch2)

			m.On("GetLogs", mock.Anything, s1, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx1 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch1), nil)

			m.On("GetLogs", mock.Anything, s2, mock.Anything).
				Run(func(args mock.Arguments) { capturedCtx2 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch2), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Create stream
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Init stream
			opts := []StreamOption{
				WithSince(tt.setSinceTime),
				WithUntil(tt.setUntilTime),
			}

			if tt.setMode == streamModeAll {
				opts = append(opts, WithAll())
			} else {
				opts = append(opts, WithHead(tt.setMaxNum))
			}

			stream, err := NewStream(ctx, fake.NewClientset(), []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close(context.TODO())

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logProvider = &m

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
			logOpts := &corev1.PodLogOptions{}
			if !tt.setSinceTime.IsZero() {
				logOpts.SinceTime = &metav1.Time{Time: tt.setSinceTime}
			}
			m.AssertCalled(t, "GetLogs", mock.Anything, s1, logOpts)
			m.AssertCalled(t, "GetLogs", mock.Anything, s2, logOpts)

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

	logs := []LogRecord{
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
			// Init mock logProvider
			m := mockLogProvider{}

			var capturedCtx1 context.Context
			var capturedCtx2 context.Context

			ch1 := make(chan LogRecord, len(logs1))
			for i := len(logs1) - 1; i >= 0; i-- {
				ch1 <- logs1[i]
			}
			close(ch1)

			ch2 := make(chan LogRecord, len(logs))
			for i := len(logs) - 1; i >= 0; i-- {
				ch2 <- logs[i]
			}
			close(ch2)

			m.On("GetLogsReverse", mock.Anything, s1, mock.Anything, tt.setSinceTime).
				Run(func(args mock.Arguments) { capturedCtx1 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch1), nil)
			m.On("GetLogsReverse", mock.Anything, s2, mock.Anything, tt.setSinceTime).
				Run(func(args mock.Arguments) { capturedCtx2 = args.Get(0).(context.Context) }).
				Return((<-chan LogRecord)(ch2), nil)

			// Init mock source watcher
			sw := mockSourceWatcher{}

			sw.On("Start", mock.Anything).Return(nil)
			sw.On("Set").Return(set.NewSet(s1, s2))
			sw.On("Subscribe", mock.Anything, mock.Anything).Return()
			sw.On("Unsubscribe", mock.Anything, mock.Anything).Return()
			sw.On("Shutdown", mock.Anything).Return(nil)

			// Create stream
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Init stream
			opts := []StreamOption{
				WithSince(tt.setSinceTime),
				WithUntil(tt.setUntilTime),
				WithTail(tt.setMaxNum),
			}

			stream, err := NewStream(ctx, fake.NewClientset(), []string{}, opts...)
			require.NoError(t, err)
			defer stream.Close(context.TODO())

			// Override source watcher and log provider
			stream.sw = &sw
			stream.logProvider = &m

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

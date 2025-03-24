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

	"github.com/stretchr/testify/assert"
)

func TestMergeLogStreams(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		streams  [][]LogRecord
		expected []LogRecord
	}{
		{
			name: "merge two ordered streams",
			streams: [][]LogRecord{
				{
					{Timestamp: now, Message: "stream1-1"},
					{Timestamp: now.Add(2 * time.Second), Message: "stream1-2"},
				},
				{
					{Timestamp: now.Add(time.Second), Message: "stream2-1"},
					{Timestamp: now.Add(3 * time.Second), Message: "stream2-2"},
				},
			},
			expected: []LogRecord{
				{Timestamp: now, Message: "stream1-1"},
				{Timestamp: now.Add(time.Second), Message: "stream2-1"},
				{Timestamp: now.Add(2 * time.Second), Message: "stream1-2"},
				{Timestamp: now.Add(3 * time.Second), Message: "stream2-2"},
			},
		},
		{
			name: "merge empty streams",
			streams: [][]LogRecord{
				{},
				{},
			},
			expected: []LogRecord{},
		},
		{
			name: "merge single stream",
			streams: [][]LogRecord{
				{
					{Timestamp: now, Message: "msg1"},
					{Timestamp: now.Add(time.Second), Message: "msg2"},
				},
			},
			expected: []LogRecord{
				{Timestamp: now, Message: "msg1"},
				{Timestamp: now.Add(time.Second), Message: "msg2"},
			},
		},
		{
			name: "merge streams with same timestamps",
			streams: [][]LogRecord{
				{
					{Timestamp: now, Message: "stream1"},
				},
				{
					{Timestamp: now, Message: "stream2"},
				},
			},
			expected: []LogRecord{
				{Timestamp: now, Message: "stream1"},
				{Timestamp: now, Message: "stream2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create channels and feed test data
			streams := make([]<-chan LogRecord, len(tt.streams))
			for i, records := range tt.streams {
				ch := make(chan LogRecord, len(records))
				for _, record := range records {
					ch <- record
				}
				close(ch)
				streams[i] = ch
			}

			// Run mergeLogStreams
			ctx := context.Background()
			merged := mergeLogStreams(ctx, false, streams...)

			// Collect results
			var results []LogRecord
			for record := range merged {
				results = append(results, record)
			}

			// Verify results
			assert.Equal(t, len(tt.expected), len(results), "number of records should match")
			for i := range results {
				assert.Equal(t, tt.expected[i].Timestamp, results[i].Timestamp, "timestamps should match at index %d", i)
				assert.Equal(t, tt.expected[i].Message, results[i].Message, "messages should match at index %d", i)
			}
		})
	}
}

func TestMergeLogStreamsReverse(t *testing.T) {
	baseTS := time.Now()

	tests := []struct {
		name     string
		streams  [][]LogRecord
		expected []LogRecord
	}{
		{
			name: "merge two reverse ordered streams",
			streams: [][]LogRecord{
				{
					{Timestamp: baseTS.Add(2 * time.Second), Message: "stream1-2"},
					{Timestamp: baseTS, Message: "stream1-1"},
				},
				{
					{Timestamp: baseTS.Add(3 * time.Second), Message: "stream2-2"},
					{Timestamp: baseTS.Add(time.Second), Message: "stream2-1"},
				},
			},
			expected: []LogRecord{
				{Timestamp: baseTS.Add(3 * time.Second), Message: "stream2-2"},
				{Timestamp: baseTS.Add(2 * time.Second), Message: "stream1-2"},
				{Timestamp: baseTS.Add(time.Second), Message: "stream2-1"},
				{Timestamp: baseTS, Message: "stream1-1"},
			},
		},
		{
			name: "merge empty streams",
			streams: [][]LogRecord{
				{},
				{},
			},
			expected: []LogRecord{},
		},
		{
			name: "merge single stream",
			streams: [][]LogRecord{
				{
					{Timestamp: baseTS.Add(time.Second), Message: "msg2"},
					{Timestamp: baseTS, Message: "msg1"},
				},
			},
			expected: []LogRecord{
				{Timestamp: baseTS.Add(time.Second), Message: "msg2"},
				{Timestamp: baseTS, Message: "msg1"},
			},
		},
		{
			name: "merge streams with same timestamps",
			streams: [][]LogRecord{
				{
					{Timestamp: baseTS, Message: "stream1"},
				},
				{
					{Timestamp: baseTS, Message: "stream2"},
				},
			},
			expected: []LogRecord{
				{Timestamp: baseTS, Message: "stream1"},
				{Timestamp: baseTS, Message: "stream2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create channels and feed test data
			streams := make([]<-chan LogRecord, len(tt.streams))
			for i, records := range tt.streams {
				ch := make(chan LogRecord, len(records))
				for _, record := range records {
					ch <- record
				}
				close(ch)
				streams[i] = ch
			}

			// Run mergeLogStreams in reverse mode
			ctx := context.Background()
			merged := mergeLogStreams(ctx, true, streams...)

			// Collect results
			var results []LogRecord
			for record := range merged {
				results = append(results, record)
			}

			// Verify results
			assert.Equal(t, len(tt.expected), len(results), "number of records should match")
			for i := range results {
				assert.Equal(t, tt.expected[i].Timestamp, results[i].Timestamp, "timestamps should match at index %d", i)
				assert.Equal(t, tt.expected[i].Message, results[i].Message, "messages should match at index %d", i)
			}
		})
	}
}

func TestMergeLogStreamsContextCancellation(t *testing.T) {
	// Create test data
	now := time.Now()
	records := []LogRecord{
		{Timestamp: now, Message: "msg1"},
		{Timestamp: now.Add(time.Second), Message: "msg2"},
		{Timestamp: now.Add(2 * time.Second), Message: "msg3"},
	}

	// Create channel with test data
	ch := make(chan LogRecord, len(records))
	for _, record := range records {
		ch <- record
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Run mergeLogStreams
	merged := mergeLogStreams(ctx, false, ch)

	// Read first record
	record := <-merged

	// Verify first record
	assert.Equal(t, records[0].Timestamp, record.Timestamp)
	assert.Equal(t, records[0].Message, record.Message)

	// Cancel context
	cancel()

	// Verify channel is closed
	_, ok := <-merged
	assert.False(t, ok, "channel should be closed after context cancellation")
}

func TestNewLogRecordFromLogLine(t *testing.T) {
	tests := []struct {
		name        string
		logLine     string
		wantTime    time.Time
		wantMessage string
		wantErr     bool
	}{
		{
			name:        "valid log line",
			logLine:     "2025-03-13T11:36:09.123456789Z Hello world",
			wantTime:    time.Date(2025, 3, 13, 11, 36, 9, 123456789, time.UTC),
			wantMessage: "Hello world",
			wantErr:     false,
		},
		{
			name:        "valid log line with multiple spaces in message",
			logLine:     "2025-03-13T11:36:09.123456789Z   Multiple   spaces   here   ",
			wantTime:    time.Date(2025, 3, 13, 11, 36, 9, 123456789, time.UTC),
			wantMessage: "  Multiple   spaces   here   ",
			wantErr:     false,
		},
		{
			name:    "missing timestamp",
			logLine: "Hello world",
			wantErr: true,
		},
		{
			name:    "invalid timestamp format",
			logLine: "2025-03-13 11:36:09 Hello world",
			wantErr: true,
		},
		{
			name:    "empty string",
			logLine: "",
			wantErr: true,
		},
		{
			name:    "only timestamp",
			logLine: "2025-03-13T11:36:09.123456789Z",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newLogRecordFromLogLine(tt.logLine)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantTime, got.Timestamp, "timestamps should match")
			assert.Equal(t, tt.wantMessage, got.Message, "messages should match")
		})
	}
}

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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/smallnest/ringbuffer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RoundTripperFunc type is an adapter to allow the use of ordinary functions as http.RoundTripper.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f(r).
func (f RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPodLogsReaderSuccess(t *testing.T) {
	baseTS := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	type expectedRecord struct {
		message string
		isFinal bool
	}

	tests := []struct {
		name     string
		body     string
		maxChunk int
		expected []expectedRecord
	}{
		{
			name:     "single complete line",
			body:     "hello world\n",
			maxChunk: 64,
			expected: []expectedRecord{
				{message: "hello world", isFinal: true},
			},
		},
		{
			name:     "chunks long message and keeps timestamp",
			body:     strings.Repeat("a", 12) + "\n",
			maxChunk: 5,
			expected: []expectedRecord{
				{message: strings.Repeat("a", 5), isFinal: false},
				{message: strings.Repeat("a", 5), isFinal: false},
				{message: strings.Repeat("a", 2), isFinal: true},
			},
		},
		{
			name:     "final newline left in pending buffer",
			body:     "hello\n",
			maxChunk: 5,
			expected: []expectedRecord{
				{message: "hello", isFinal: true},
			},
		},
		{
			name:     "marks final chunk when EOF without trailing newline",
			body:     "trailing data without newline",
			maxChunk: 64,
			expected: []expectedRecord{
				{message: "trailing data without newline", isFinal: true},
			},
		},
		{
			name:     "does not split utf8 runes when chunking",
			body:     "abc💡def\n",
			maxChunk: 6,
			expected: []expectedRecord{
				{message: "abc", isFinal: false},
				{message: "💡de", isFinal: false},
				{message: "f", isFinal: true},
			},
		},
		{
			name:     "line exceeds bufio buffer size (4 KiB)",
			body:     strings.Repeat("x", 5*1024),
			maxChunk: 4 * 1024,
			expected: []expectedRecord{
				{message: strings.Repeat("x", 4*1024), isFinal: false},
				{message: strings.Repeat("x", 1*1024), isFinal: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logLine := baseTS.Format(time.RFC3339Nano) + " " + tt.body

			rc := io.NopCloser(strings.NewReader(logLine))
			defer rc.Close()

			next := podLogsReader(rc, tt.maxChunk)

			for i, expected := range tt.expected {
				record, err := next()
				require.NoError(t, err, "record %d", i)
				assert.Equal(t, baseTS, record.Timestamp, "record %d", i)
				assert.Equal(t, expected.message, record.Message, "record %d", i)
				assert.Equal(t, expected.isFinal, record.IsFinal, "record %d", i)
			}

			_, err := next()
			assert.Equal(t, io.EOF, err)
		})
	}
}

func TestPodLogsReaderErrors(t *testing.T) {
	t.Run("eof", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader(""))
		defer rc.Close()

		next := podLogsReader(rc, 64)

		_, err := next()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader("not-a-timestamp message\n"))
		defer rc.Close()

		next := podLogsReader(rc, 64)

		_, err := next()
		var parseErr *time.ParseError
		require.ErrorAs(t, err, &parseErr)
	})

	t.Run("missing log payload", func(t *testing.T) {
		ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
		rc := io.NopCloser(strings.NewReader(ts.Format(time.RFC3339Nano) + " "))
		defer rc.Close()

		next := podLogsReader(rc, 64)

		_, err := next()
		require.ErrorIs(t, err, ErrExpectedData)
	})
}

func TestPodLogsReaderErrBufferFullChunking(t *testing.T) {
	baseTS := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	maxChunk := 2048

	// Use a long line (no early newline) so bufio.ReadSlice hits ErrBufferFull and we still chunk correctly.
	longBody := strings.Repeat("y", 5*1024) // 5120 bytes > default bufio.Reader buffer size

	rc := io.NopCloser(strings.NewReader(baseTS.Format(time.RFC3339Nano) + " " + longBody + "\n"))
	defer rc.Close()

	next := podLogsReader(rc, maxChunk)

	expected := []struct {
		message string
		isFinal bool
	}{
		{message: longBody[:maxChunk], isFinal: false},
		{message: longBody[maxChunk : 2*maxChunk], isFinal: false},
		{message: longBody[2*maxChunk:], isFinal: true},
	}

	for i, exp := range expected {
		record, err := next()
		require.NoError(t, err, "record %d", i)
		assert.Equal(t, baseTS, record.Timestamp, "record %d", i)
		assert.Equal(t, exp.message, record.Message, "record %d", i)
		assert.Equal(t, exp.isFinal, record.IsFinal, "record %d", i)
	}

	_, err := next()
	assert.Equal(t, io.EOF, err)
}

func TestExtractTimestampFromRingSuccess(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC)
	timestamp := ts.Format(time.RFC3339Nano)
	payload := "hello world\n"

	ring := ringbuffer.New(128)
	_, err := ring.Write([]byte(timestamp + " " + payload))
	require.NoError(t, err)

	parsedTS, err := extractTimestampFromRing(ring, make([]byte, len(payload)+36))
	require.NoError(t, err)
	assert.Equal(t, ts, parsedTS)

	remainingLen := ring.Length()
	assert.Equal(t, len(payload), remainingLen)

	remaining := make([]byte, remainingLen)
	n, err := ring.Peek(remaining)
	require.NoError(t, err)
	assert.Equal(t, payload, string(remaining[:n]))
}

func TestExtractTimestampFromRingFailure(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC)
	timestamp := ts.Format(time.RFC3339Nano)

	t.Run("errors when buffer is empty", func(t *testing.T) {
		ring := ringbuffer.New(64)

		parsedTS, err := extractTimestampFromRing(ring, make([]byte, 64))

		assert.Zero(t, parsedTS)
		require.ErrorIs(t, err, ringbuffer.ErrIsEmpty)
		assert.Equal(t, 0, ring.Length())
	})

	t.Run("errors when delimiter is missing", func(t *testing.T) {
		ring := ringbuffer.New(128)
		input := timestamp + "payload-without-space"

		_, err := ring.Write([]byte(input))
		require.NoError(t, err)

		parsedTS, err := extractTimestampFromRing(ring, make([]byte, 64))
		assert.Zero(t, parsedTS)
		require.EqualError(t, err, "delimiter not found")

		remainingLen := ring.Length()
		assert.Equal(t, len(input), remainingLen)

		remaining := make([]byte, remainingLen)
		n, peekErr := ring.Peek(remaining)
		require.NoError(t, peekErr)
		assert.Equal(t, input, string(remaining[:n]))
	})

	t.Run("errors on invalid timestamp", func(t *testing.T) {
		ring := ringbuffer.New(128)
		input := "not-a-timestamp payload"

		_, err := ring.Write([]byte(input))
		require.NoError(t, err)

		parsedTS, err := extractTimestampFromRing(ring, make([]byte, 64))
		assert.Zero(t, parsedTS)

		var parseErr *time.ParseError
		require.ErrorAs(t, err, &parseErr)

		assert.Equal(t, len(input), ring.Length())
	})

	t.Run("errors when peek buffer is too small", func(t *testing.T) {
		ring := ringbuffer.New(128)
		input := timestamp + " message"

		_, err := ring.Write([]byte(input))
		require.NoError(t, err)

		parsedTS, err := extractTimestampFromRing(ring, make([]byte, 10))
		assert.Zero(t, parsedTS)
		require.ErrorIs(t, err, ErrBufCapacity)

		assert.Equal(t, len(input), ring.Length())
	})
}

func TestRuneSafeChunkLenFromRing(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		maxChunkSize int
		peekBufSize  int
		wantChunkLen int
		wantErr      error
	}{
		{
			name:         "empty ring returns zero",
			data:         "",
			maxChunkSize: 5,
			wantChunkLen: 0,
		},
		{
			name:         "returns full length when max chunk not limiting",
			data:         "abc",
			maxChunkSize: 0,
			wantChunkLen: 3,
		},
		{
			name:         "returns full length when max chunk exceeds ring size",
			data:         "abc",
			maxChunkSize: 10,
			wantChunkLen: 3,
		},
		{
			name:         "does not split multibyte rune at boundary",
			data:         "abc\u00e9",
			maxChunkSize: 4,
			wantChunkLen: 3,
		},
		{
			name:         "returns rune length when max chunk smaller than first rune",
			data:         "\U0001f916abc",
			maxChunkSize: 2,
			wantChunkLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := ringbuffer.New(len([]byte(tt.data)) + 8)

			if tt.data != "" {
				_, err := ring.Write([]byte(tt.data))
				require.NoError(t, err)
			}

			buf := make([]byte, tt.maxChunkSize+utf8.UTFMax)

			gotChunkLen, err := runeSafeChunkLenFromRing(ring, tt.maxChunkSize, buf)
			require.NoError(t, err)

			assert.Equal(t, tt.wantChunkLen, gotChunkLen)
			assert.Equal(t, len([]byte(tt.data)), ring.Length())
		})
	}
}

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

func TestParseWorkloadType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected WorkloadType
	}{
		// Test exact matches
		{name: "cronjob", input: "cronjob", expected: WorkloadTypeCronJob},
		{name: "daemonset", input: "daemonset", expected: WorkloadTypeDaemonSet},
		{name: "deployment", input: "deployment", expected: WorkloadTypeDeployment},
		{name: "job", input: "job", expected: WorkloadTypeJob},
		{name: "pod", input: "pod", expected: WorkloadTypePod},
		{name: "replicaset", input: "replicaset", expected: WorkloadTypeReplicaSet},
		{name: "statefulset", input: "statefulset", expected: WorkloadTypeStatefulSet},

		// Test with trailing 's'
		{name: "cronjobs", input: "cronjobs", expected: WorkloadTypeCronJob},
		{name: "daemonsets", input: "daemonsets", expected: WorkloadTypeDaemonSet},
		{name: "deployments", input: "deployments", expected: WorkloadTypeDeployment},
		{name: "jobs", input: "jobs", expected: WorkloadTypeJob},
		{name: "pods", input: "pods", expected: WorkloadTypePod},
		{name: "replicasets", input: "replicasets", expected: WorkloadTypeReplicaSet},
		{name: "statefulsets", input: "statefulsets", expected: WorkloadTypeStatefulSet},

		// Test with mixed case
		{name: "CronJob", input: "CronJob", expected: WorkloadTypeCronJob},
		{name: "DaemonSet", input: "DaemonSet", expected: WorkloadTypeDaemonSet},
		{name: "Deployment", input: "Deployment", expected: WorkloadTypeDeployment},
		{name: "Job", input: "Job", expected: WorkloadTypeJob},
		{name: "Pod", input: "Pod", expected: WorkloadTypePod},
		{name: "ReplicaSet", input: "ReplicaSet", expected: WorkloadTypeReplicaSet},
		{name: "StatefulSet", input: "StatefulSet", expected: WorkloadTypeStatefulSet},

		// Test with mixed case and trailing 's'
		{name: "CronJobs", input: "CronJobs", expected: WorkloadTypeCronJob},
		{name: "DaemonSets", input: "DaemonSets", expected: WorkloadTypeDaemonSet},
		{name: "Deployments", input: "Deployments", expected: WorkloadTypeDeployment},
		{name: "Jobs", input: "Jobs", expected: WorkloadTypeJob},
		{name: "Pods", input: "Pods", expected: WorkloadTypePod},
		{name: "ReplicaSets", input: "ReplicaSets", expected: WorkloadTypeReplicaSet},
		{name: "StatefulSets", input: "StatefulSets", expected: WorkloadTypeStatefulSet},

		// Test with kubectl shortcuts
		{name: "cj", input: "cj", expected: WorkloadTypeCronJob},
		{name: "ds", input: "ds", expected: WorkloadTypeDaemonSet},
		{name: "deploy", input: "deploy", expected: WorkloadTypeDeployment},
		{name: "po", input: "po", expected: WorkloadTypePod},
		{name: "rs", input: "rs", expected: WorkloadTypeReplicaSet},
		{name: "sts", input: "sts", expected: WorkloadTypeStatefulSet},

		// Test unknown workload types
		{name: "unknown", input: "unknown", expected: WorkloadTypeUknown},
		{name: "empty string", input: "", expected: WorkloadTypeUknown},
		{name: "random string", input: "randomstring", expected: WorkloadTypeUknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorkloadType(tt.input)
			assert.Equal(t, tt.expected, result, "parseWorkloadType(%q) should return %v", tt.input, tt.expected)
		})
	}
}

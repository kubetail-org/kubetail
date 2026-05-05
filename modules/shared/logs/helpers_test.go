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

package logs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RoundTripperFunc type is an adapter to allow the use of ordinary functions as http.RoundTripper.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f(r).
func (f RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPodLogsReader(t *testing.T) {
	baseTS := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("single complete line", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader(baseTS.Format(time.RFC3339Nano) + " hello\n"))
		defer rc.Close()

		next := podLogsReader(rc)

		// Get record
		record, err1 := next()
		require.NoError(t, err1)
		require.Equal(t, baseTS, record.Timestamp)
		require.Equal(t, "hello", record.Message)

		// Check end
		_, err2 := next()
		require.Equal(t, io.EOF, err2)
	})

	t.Run("single empty line", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader(baseTS.Format(time.RFC3339Nano) + " \n"))
		defer rc.Close()

		next := podLogsReader(rc)

		// Get record
		record, err1 := next()
		require.NoError(t, err1)
		require.Equal(t, baseTS, record.Timestamp)
		require.Equal(t, "", record.Message)

		// Check end
		_, err2 := next()
		require.Equal(t, io.EOF, err2)
	})

	t.Run("multiple complete lines", func(t *testing.T) {
		ts := baseTS.Format(time.RFC3339Nano)
		rc := io.NopCloser(strings.NewReader(fmt.Sprintf("%s hello\n%s world\n", ts, ts)))
		defer rc.Close()

		next := podLogsReader(rc)

		// First
		r1, err := next()
		require.NoError(t, err)
		require.Equal(t, baseTS, r1.Timestamp)
		require.Equal(t, "hello", r1.Message)

		// Second
		r2, err := next()
		require.NoError(t, err)
		require.Equal(t, baseTS, r2.Timestamp)
		require.Equal(t, "world", r2.Message)
	})

	t.Run("ignores partial lines", func(t *testing.T) {
		ts := baseTS.Format(time.RFC3339Nano)
		rc := io.NopCloser(strings.NewReader(fmt.Sprintf("partial\n%s hello\n", ts)))
		defer rc.Close()

		next := podLogsReader(rc)

		record, err := next()
		require.NoError(t, err)
		require.Equal(t, baseTS, record.Timestamp)
		require.Equal(t, "hello", record.Message)
	})

	t.Run("ignores bad timestamps", func(t *testing.T) {
		ts := baseTS.Format(time.RFC3339Nano)
		rc := io.NopCloser(strings.NewReader(fmt.Sprintf("bad timestamp\n%s hello\n", ts)))
		defer rc.Close()

		next := podLogsReader(rc)

		record, err := next()
		require.NoError(t, err)
		require.Equal(t, baseTS, record.Timestamp)
		require.Equal(t, "hello", record.Message)
	})

	t.Run("line exceeds 4KB buffer size", func(t *testing.T) {
		msg := strings.Repeat("x", 5*1024)

		rc := io.NopCloser(strings.NewReader(fmt.Sprintf("%s %s\n", baseTS.Format(time.RFC3339Nano), msg)))
		defer rc.Close()

		next := podLogsReader(rc)

		// Get record
		record, err1 := next()
		require.NoError(t, err1)
		require.Equal(t, baseTS, record.Timestamp)
		require.Equal(t, msg, record.Message)

		// Check end
		_, err2 := next()
		require.Equal(t, io.EOF, err2)
	})

	t.Run("eof", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader(""))
		defer rc.Close()

		next := podLogsReader(rc)

		_, err := next()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("eof without newline", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader(baseTS.Format(time.RFC3339Nano) + " hello"))
		defer rc.Close()

		next := podLogsReader(rc)

		record, err := next()
		require.Equal(t, io.EOF, err)
		require.True(t, record.Timestamp.IsZero())
		require.Equal(t, "", record.Message)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		rc := io.NopCloser(strings.NewReader("not-a-timestamp message\n"))
		defer rc.Close()

		next := podLogsReader(rc)

		_, err := next()
		require.Equal(t, io.EOF, err)
	})
}

func TestExtractTimestampFromBytes(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC)
	timestamp := ts.Format(time.RFC3339Nano)

	t.Run("valid line with timestamp", func(t *testing.T) {
		payload := "hello world\n"
		input := []byte(timestamp + " " + payload)

		pos, parsedTS, err := extractTimestampFromBytes(input)
		require.NoError(t, err)
		assert.Equal(t, ts, parsedTS)
		assert.Equal(t, len(timestamp), pos)

		// Verify remaining payload starts after the space
		remaining := input[pos+1:]
		assert.Equal(t, payload, string(remaining))
	})

	t.Run("errors when input is empty", func(t *testing.T) {
		input := []byte{}

		pos, parsedTS, err := extractTimestampFromBytes(input)

		assert.Zero(t, parsedTS)
		assert.Equal(t, 0, pos)
		require.ErrorIs(t, err, ErrDelimiterNotFound)
	})

	t.Run("errors when delimiter is missing", func(t *testing.T) {
		input := []byte(timestamp + "payload-without-space")

		pos, parsedTS, err := extractTimestampFromBytes(input)

		assert.Zero(t, parsedTS)
		assert.Equal(t, 0, pos)
		require.ErrorIs(t, err, ErrDelimiterNotFound)
	})

	t.Run("errors on invalid timestamp", func(t *testing.T) {
		input := []byte("not-a-timestamp payload")

		pos, parsedTS, err := extractTimestampFromBytes(input)

		assert.Zero(t, parsedTS)
		assert.Equal(t, 0, pos)

		var parseErr *time.ParseError
		require.ErrorAs(t, err, &parseErr)
	})
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
	close(ch) // Close the channel after populating it

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

	// Drain remaining records and verify channel closes
	// If the fix works, the channel will close after context cancellation
	// If broken, the test hangs and Go's test timeout catches it
	for range merged {
		// Drain any remaining records
	}
}

// TestMergeLogStreamsContextCancellationWhileBlocking verifies that mergeLogStreams
// properly responds to context cancellation even when blocked waiting for input.
func TestMergeLogStreamsContextCancellationWhileBlocking(t *testing.T) {
	// Create an unbuffered channel that will block on read
	ch := make(chan LogRecord)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Run mergeLogStreams - it will block waiting for input from ch
	merged := mergeLogStreams(ctx, false, ch)

	// Cancel context immediately (before any data is sent)
	cancel()

	// If the fix works, this read returns immediately with ok=false
	// If broken, the test hangs and Go's test timeout catches it
	_, ok := <-merged
	if ok {
		t.Fatal("expected channel to be closed, but received a value")
	}
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

func TestPaginateLogRecords(t *testing.T) {
	// Helper to create log records with specific timestamps
	makeRecords := func(timestamps ...time.Time) []LogRecord {
		records := make([]LogRecord, len(timestamps))
		for i, ts := range timestamps {
			records[i] = LogRecord{Timestamp: ts, Message: "msg"}
		}
		return records
	}

	now := time.Now()
	ts1 := now.Add(-4 * time.Second)
	ts2 := now.Add(-3 * time.Second)
	ts3 := now.Add(-2 * time.Second)
	ts4 := now.Add(-1 * time.Second)
	ts5 := now

	t.Run("empty records returns nil cursor", func(t *testing.T) {
		records, cursor := PaginateLogRecords([]LogRecord{}, 10, PaginationModeTail)
		assert.Empty(t, records)
		assert.Nil(t, cursor)
	})

	t.Run("zero limit returns all records with nil cursor", func(t *testing.T) {
		input := makeRecords(ts1, ts2, ts3)
		records, cursor := PaginateLogRecords(input, 0, PaginationModeTail)
		assert.Len(t, records, 3)
		assert.Nil(t, cursor)
	})

	t.Run("negative limit returns all records with nil cursor", func(t *testing.T) {
		input := makeRecords(ts1, ts2, ts3)
		records, cursor := PaginateLogRecords(input, -1, PaginationModeTail)
		assert.Len(t, records, 3)
		assert.Nil(t, cursor)
	})

	t.Run("records count equals limit returns nil cursor", func(t *testing.T) {
		input := makeRecords(ts1, ts2, ts3)
		records, cursor := PaginateLogRecords(input, 3, PaginationModeTail)
		assert.Len(t, records, 3)
		assert.Nil(t, cursor)
	})

	t.Run("records count less than limit returns nil cursor", func(t *testing.T) {
		input := makeRecords(ts1, ts2)
		records, cursor := PaginateLogRecords(input, 5, PaginationModeTail)
		assert.Len(t, records, 2)
		assert.Nil(t, cursor)
	})

	t.Run("TAIL mode trims first record and cursor points to first returned record", func(t *testing.T) {
		// TAIL fetches limit+1 records to detect if there's a previous page;
		// the extra (oldest) record is trimmed off and the cursor points to
		// the first record we actually returned. The resolver's exclusive
		// `before` filter then resumes the previous page immediately before.
		input := makeRecords(ts1, ts2, ts3, ts4, ts5)
		records, cursor := PaginateLogRecords(input, 4, PaginationModeTail)

		assert.Len(t, records, 4)
		assert.Equal(t, ts2, records[0].Timestamp)
		assert.Equal(t, ts5, records[3].Timestamp)

		assert.NotNil(t, cursor)
		assert.Equal(t, ts2.Format(time.RFC3339Nano), *cursor)
	})

	t.Run("HEAD mode trims last record and cursor points to last returned record", func(t *testing.T) {
		// HEAD fetches limit+1 records to detect if there's a next page; the
		// extra (newest) record is trimmed off and the cursor points to the
		// last record we actually returned. The resolver's exclusive `after`
		// filter then resumes the next page immediately after.
		input := makeRecords(ts1, ts2, ts3, ts4, ts5)
		records, cursor := PaginateLogRecords(input, 4, PaginationModeHead)

		assert.Len(t, records, 4)
		assert.Equal(t, ts1, records[0].Timestamp)
		assert.Equal(t, ts4, records[3].Timestamp)

		assert.NotNil(t, cursor)
		assert.Equal(t, ts4.Format(time.RFC3339Nano), *cursor)
	})

	t.Run("single extra record TAIL mode", func(t *testing.T) {
		input := makeRecords(ts1, ts2)
		records, cursor := PaginateLogRecords(input, 1, PaginationModeTail)

		assert.Len(t, records, 1)
		assert.Equal(t, ts2, records[0].Timestamp)
		assert.NotNil(t, cursor)
		assert.Equal(t, ts2.Format(time.RFC3339Nano), *cursor)
	})

	t.Run("single extra record HEAD mode", func(t *testing.T) {
		input := makeRecords(ts1, ts2)
		records, cursor := PaginateLogRecords(input, 1, PaginationModeHead)

		assert.Len(t, records, 1)
		assert.Equal(t, ts1, records[0].Timestamp)
		assert.NotNil(t, cursor)
		assert.Equal(t, ts1.Format(time.RFC3339Nano), *cursor)
	})

	t.Run("HEAD two-page simulation has no gap with exclusive after", func(t *testing.T) {
		// Mirrors the resolver: it fetches limit+1, paginates, and resumes
		// the next page with sinceTime = cursor + 1ns. With the cursor
		// pointing to the last *returned* record, the boundary record is
		// preserved across pages.
		all := makeRecords(ts1, ts2, ts3, ts4, ts5)

		page1, cursor := PaginateLogRecords(all[:3], 2, PaginationModeHead)
		require.NotNil(t, cursor)
		require.Len(t, page1, 2)

		afterTime, err := time.Parse(time.RFC3339Nano, *cursor)
		require.NoError(t, err)
		sinceTime := afterTime.Add(1 * time.Nanosecond)
		var remaining []LogRecord
		for _, r := range all {
			if !r.Timestamp.Before(sinceTime) {
				remaining = append(remaining, r)
			}
		}

		page2, cursor2 := PaginateLogRecords(remaining[:min(3, len(remaining))], 2, PaginationModeHead)

		got := append([]LogRecord{}, page1...)
		got = append(got, page2...)
		assert.Equal(t, []time.Time{ts1, ts2, ts3, ts4}, []time.Time{got[0].Timestamp, got[1].Timestamp, got[2].Timestamp, got[3].Timestamp},
			"page1+page2 must equal the original set with no gaps")
		assert.NotNil(t, cursor2, "ts5 still lies past page2")
	})

	t.Run("TAIL two-page simulation has no gap with exclusive before", func(t *testing.T) {
		// Symmetric to HEAD: untilTime = cursor - 1ns.
		all := makeRecords(ts1, ts2, ts3, ts4, ts5)

		page1, cursor := PaginateLogRecords(all[2:], 2, PaginationModeTail)
		require.NotNil(t, cursor)
		require.Len(t, page1, 2)

		beforeTime, err := time.Parse(time.RFC3339Nano, *cursor)
		require.NoError(t, err)
		untilTime := beforeTime.Add(-1 * time.Nanosecond)
		var remaining []LogRecord
		for _, r := range all {
			if !r.Timestamp.After(untilTime) {
				remaining = append(remaining, r)
			}
		}

		start := 0
		if len(remaining) > 3 {
			start = len(remaining) - 3
		}
		page2, cursor2 := PaginateLogRecords(remaining[start:], 2, PaginationModeTail)

		// Older page first to recover chronological order.
		got := append([]LogRecord{}, page2...)
		got = append(got, page1...)
		assert.Equal(t, []time.Time{ts2, ts3, ts4, ts5}, []time.Time{got[0].Timestamp, got[1].Timestamp, got[2].Timestamp, got[3].Timestamp},
			"page2+page1 must equal the original set with no gaps")
		assert.NotNil(t, cursor2, "ts1 still lies before page2")
	})

	t.Run("preserves nanosecond precision in cursor", func(t *testing.T) {
		// Create timestamp with nanosecond precision
		preciseTS := time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC)
		input := makeRecords(preciseTS, preciseTS.Add(time.Second))
		_, cursor := PaginateLogRecords(input, 1, PaginationModeTail)

		assert.NotNil(t, cursor)
		// Verify nanoseconds are preserved
		parsedTime, err := time.Parse(time.RFC3339Nano, *cursor)
		assert.NoError(t, err)
		assert.Equal(t, preciseTS.Nanosecond(), parsedTime.Nanosecond())
	})
}

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
	"bufio"
	"context"
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

func TestNextRecordFromReader(t *testing.T) {
	tests := []struct {
		name               string
		setLine            string
		setTruncateAtBytes uint64
		setBufSize         int
		wantTimestamp      time.Time
		wantMessage        string
		wantOrigSize       uint64
		wantTruncated      bool
		wantErr            error
	}{
		{
			name:               "parses valid record",
			setLine:            "2025-03-13T11:36:09.123456789Z hello world\n",
			setTruncateAtBytes: 0,
			wantTimestamp:      time.Date(2025, 3, 13, 11, 36, 9, 123456789, time.UTC),
			wantMessage:        "hello world",
			wantOrigSize:       uint64(len("hello world")),
			wantTruncated:      false,
			wantErr:            nil,
		},
		{
			name:               "truncates long message",
			setLine:            "2024-01-01T00:00:00Z 0123456789ABCDEFGHIJ\n",
			setTruncateAtBytes: 10,
			wantTimestamp:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			wantMessage:        "0123456789",
			wantOrigSize:       uint64(len("0123456789ABCDEFGHIJ")),
			wantTruncated:      true,
			wantErr:            nil,
		},
		{
			name:               "handles chunked lines",
			setLine:            "2024-06-15T12:00:00Z " + strings.Repeat("x", 200) + "\n",
			setTruncateAtBytes: 0,
			setBufSize:         30, // force multiple ReadLine chunks
			wantTimestamp:      time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			wantMessage:        strings.Repeat("x", 200),
			wantOrigSize:       uint64(200),
			wantTruncated:      false,
			wantErr:            nil,
		},
		{
			name:    "returns proper error at EOF",
			setLine: "",
			wantErr: io.EOF,
		},
		{
			name:    "invalid timestamp",
			setLine: "not-a-timestamp message body\n",
			wantErr: &time.ParseError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := func() *bufio.Reader {
				if tt.setBufSize > 0 {
					return bufio.NewReaderSize(strings.NewReader(tt.setLine), tt.setBufSize)
				}
				return bufio.NewReader(strings.NewReader(tt.setLine))
			}()

			record, err := nextRecordFromReader(reader, tt.setTruncateAtBytes)
			if tt.wantErr != nil {
				if tt.wantErr == io.EOF {
					assert.ErrorIs(t, tt.wantErr, err)
				} else if tt.wantErr != nil {
					assert.ErrorAs(t, tt.wantErr, &err)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTimestamp, record.Timestamp)
			assert.Equal(t, tt.wantMessage, record.Message)
			assert.Equal(t, tt.wantOrigSize, record.OriginalSizeBytes)
			assert.Equal(t, tt.wantTruncated, record.IsTruncated)
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

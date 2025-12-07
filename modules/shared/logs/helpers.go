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
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// RFC3339Nano max length is 35 bytes (e.g., "2006-01-02T15:04:05.999999999Z07:00")
// We add 1 for the space delimiter
const TIMESTAMP_MAX_SEARCH_LEN = 36

var (
	ErrExpectedData      = errors.New("expected data")
	ErrBufCapacity       = errors.New("buffer capacity too low")
	ErrDelimiterNotFound = errors.New("delimiter not found")
)

// Workload enum type
type WorkloadType int

// Workload enum values
const (
	WorkloadTypeUknown WorkloadType = iota
	WorkloadTypeCronJob
	WorkloadTypeDaemonSet
	WorkloadTypeDeployment
	WorkloadTypeJob
	WorkloadTypePod
	WorkloadTypeReplicaSet
	WorkloadTypeStatefulSet
)

// String method for readable output
func (w WorkloadType) String() string {
	switch w {
	case WorkloadTypeCronJob:
		return "CronJob"
	case WorkloadTypeDaemonSet:
		return "DaemonSet"
	case WorkloadTypeDeployment:
		return "Deployment"
	case WorkloadTypeJob:
		return "Job"
	case WorkloadTypePod:
		return "Pod"
	case WorkloadTypeReplicaSet:
		return "ReplicaSet"
	case WorkloadTypeStatefulSet:
		return "StatefulSet"
	default:
		return "Unknown"
	}
}

// Return group and resource
func (w WorkloadType) GroupResource() (string, string, error) {
	switch w {
	case WorkloadTypeCronJob:
		return "batch", "cronjobs", nil
	case WorkloadTypeDaemonSet:
		return "apps", "daemonsets", nil
	case WorkloadTypeDeployment:
		return "apps", "deployments", nil
	case WorkloadTypeJob:
		return "batch", "jobs", nil
	case WorkloadTypePod:
		return "", "pods", nil
	case WorkloadTypeReplicaSet:
		return "apps", "replicasets", nil
	case WorkloadTypeStatefulSet:
		return "apps", "statefulsets", nil
	default:
		return "", "", fmt.Errorf("not implemented: %s", w)
	}
}

// Return GroupResourveVersion schema instance
func (w WorkloadType) GVR() schema.GroupVersionResource {
	switch w {
	case WorkloadTypeCronJob:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
	case WorkloadTypeDaemonSet:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	case WorkloadTypeDeployment:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	case WorkloadTypeJob:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	case WorkloadTypePod:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	case WorkloadTypeReplicaSet:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	case WorkloadTypeStatefulSet:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	default:
		return schema.GroupVersionResource{}
	}
}

// Return result map key
func (w WorkloadType) Key(args ...string) string {
	return fmt.Sprintf("%s/%s", w.String(), strings.Join(args, "/"))
}

// Parse string and return corresponding workload
func parseWorkloadType(workloadStr string) WorkloadType {
	switch strings.ToLower(workloadStr) {
	case "cronjobs", "cronjob", "cj":
		return WorkloadTypeCronJob
	case "daemonsets", "daemonset", "ds":
		return WorkloadTypeDaemonSet
	case "deployments", "deployment", "deploy":
		return WorkloadTypeDeployment
	case "jobs", "job":
		return WorkloadTypeJob
	case "pods", "pod", "po":
		return WorkloadTypePod
	case "replicasets", "replicaset", "rs":
		return WorkloadTypeReplicaSet
	case "statefulsets", "statefulset", "sts":
		return WorkloadTypeStatefulSet
	default:
		return WorkloadTypeUknown
	}
}

// podLogsReader reads from podLogs and splits messages into chunks of max length maxChunkSize
func podLogsReader(podLogs io.ReadCloser) func() (LogRecord, error) {
	var zero LogRecord

	reader := bufio.NewReader(podLogs)
	n := 0

	// Generator function
	return func() (LogRecord, error) {
		for {
			// Read all bytes until next newline
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return zero, err
			}

			// Parse timestamp
			pos, ts, err := extractTimestampFromBytes(line)
			if err != nil {
				// This is to handle an edge case where the podLogs API returns lines without
				// timestamps or with invalid timestamps when following chunked logs
				continue
			}

			// Consume timestamp and remove newline
			start, end := pos+1, len(line)-1
			if start >= end {
				line = nil
			} else {
				line = line[start:end]
			}

			n += 1

			// Return record
			return LogRecord{
				Timestamp: ts,
				Message:   string(line),
			}, nil
		}
	}
}

// extractTimestampFromBytes reads and parses the timestamp prefix from a byte array
func extractTimestampFromBytes(line []byte) (int, time.Time, error) {
	var zero time.Time

	// Only search for delimiter within the maximum RFC3339Nano timestamp length
	searchLen := min(len(line), TIMESTAMP_MAX_SEARCH_LEN)

	pos := bytes.IndexByte(line[:searchLen], ' ')
	if pos < 0 {
		return 0, zero, ErrDelimiterNotFound
	}

	ts, err := time.Parse(time.RFC3339Nano, string(line[:pos]))
	if err != nil {
		return 0, zero, err
	}

	return pos, ts, nil
}

// mergeLogStreams merges multiple ordered log streams into a single channel
// that yields them in ascending order by timestamp.
func mergeLogStreams(ctx context.Context, reverse bool, streams ...<-chan LogRecord) <-chan LogRecord {
	outCh := make(chan LogRecord)

	// Run in goroutine
	go func() {
		defer close(outCh)

		// Build a min-heap of the first item from each stream.
		pq := newPriorityQueue(reverse)
		heap.Init(pq)

		// Initialize the heap with the first entry from each stream
		for _, ch := range streams {
			// Read one entry if available
			entry, ok := <-ch
			if ok {
				heap.Push(pq, recordWithSource{
					record: entry,
					srcCh:  ch,
				})
			}
		}

		// Repeatedly pop the earliest entry and replace it with
		// the next from the same source channel.
		for pq.Len() > 0 {
			// Pop the earliest entry
			earliest := heap.Pop(pq).(recordWithSource)

			select {
			case <-ctx.Done():
				return
			case outCh <- earliest.record:
			}

			// Read the next entry from the same source channel
			entry, ok := <-earliest.srcCh
			if ok {
				heap.Push(pq, recordWithSource{
					record: entry,
					srcCh:  earliest.srcCh,
				})
			}
		}
	}()

	return outCh
}

// Get first timestamp from a log
func getFirstTimestamp(ctx context.Context, clientset kubernetes.Interface, source LogSource, sinceTime time.Time) (time.Time, error) {
	var zero time.Time

	// build args
	opts := &corev1.PodLogOptions{
		Timestamps: true,
		LimitBytes: ptr.To[int64](100), // get more bytes than necessary
	}

	if !sinceTime.IsZero() {
		opts.SinceTime = &metav1.Time{Time: sinceTime}
	}

	opts.Container = source.ContainerName

	// execute query
	req := clientset.CoreV1().Pods(source.Namespace).GetLogs(source.PodName, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return zero, err
	}
	defer podLogs.Close()

	buf := make([]byte, 40) // timestamp is 30-35 bytes long
	n, err := podLogs.Read(buf)
	if err != nil {
		if err == io.EOF {
			// Log file is empty, return zero time with nil error
			return zero, nil
		}
		return zero, err
	}

	if n == 0 {
		// No data read, log file is empty
		return zero, nil
	}

	return time.Parse(time.RFC3339Nano, strings.Fields(string(buf[:n]))[0])
}

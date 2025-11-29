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
	"unicode/utf8"

	"github.com/smallnest/ringbuffer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const EXTRACT_TIMESTAMP_BUFFER_SIZE_MIN = 36

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
func podLogsReader(podLogs io.ReadCloser, maxChunkSize int) func() (LogRecord, error) {
	reader := bufio.NewReader(podLogs)
	ring := ringbuffer.New(maxChunkSize + bufio.MaxScanTokenSize)

	var err error
	var zero LogRecord
	var chunkTS time.Time
	var hasChunkTS bool
	var hasNewLine bool
	var isEOF bool

	// Reusable buffer to avoid allocations in the hot path
	buf := make([]byte, max(maxChunkSize+utf8.UTFMax, EXTRACT_TIMESTAMP_BUFFER_SIZE_MIN))

	// Generator function
	return func() (LogRecord, error) {
		for {
			if ring.Length() > 0 {
				// Parse timestamp if we don't have one yet
				if !hasChunkTS {
					chunkTS, err = extractTimestampFromRing(ring, buf)
					if err != nil {
						return zero, err
					} else if ring.Length() == 0 {
						return zero, ErrExpectedData
					}
					hasChunkTS = true
				}

				// Send record (if possible)
				if isEOF || hasNewLine || ring.Length() >= maxChunkSize {
					chunkLen, err := runeSafeChunkLenFromRing(ring, maxChunkSize, buf)
					if err != nil {
						return zero, err
					}

					// Read the chunk from ring buffer
					n, err := ring.Read(buf[:chunkLen])
					if err != nil {
						return zero, err
					}

					// Handle newlines
					var isFinal bool
					if buf[n-1] == '\n' {
						isFinal = true
						n -= 1
					} else if ring.Length() == 1 && hasNewLine {
						isFinal = true
						ring.Reset()
					} else if isEOF && ring.Length() == 0 {
						isFinal = true
					}

					if isFinal {
						hasChunkTS = false
						hasNewLine = false
					}

					return LogRecord{
						Timestamp: chunkTS,
						Message:   string(buf[:n]),
						IsFinal:   isFinal,
					}, nil
				}

			}

			// Read more data from source
			part, err := reader.ReadSlice('\n')

			if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
				return zero, err
			}

			if len(part) > 0 {
				ring.Write(part)

				if part[len(part)-1] == '\n' {
					hasNewLine = true
				}
			}

			if err == io.EOF {
				if ring.Length() == 0 {
					return zero, io.EOF
				}
				isEOF = true
			}
		}
	}
}

// extractTimestampFromRing reads and parses the timestamp prefix from the ring buffer
func extractTimestampFromRing(ring *ringbuffer.RingBuffer, buf []byte) (time.Time, error) {
	var zero time.Time

	// RFC3339Nano is ~35 chars max plus space
	if cap(buf) < EXTRACT_TIMESTAMP_BUFFER_SIZE_MIN {
		return zero, ErrBufCapacity
	}

	// Peek to find the space delimiter
	n, err := ring.Peek(buf[:EXTRACT_TIMESTAMP_BUFFER_SIZE_MIN])
	if err != nil {
		return zero, err
	} else if n == 0 {
		return zero, ErrExpectedData
	}

	i := bytes.IndexByte(buf[:n], ' ')
	if i < 0 {
		return zero, ErrDelimiterNotFound
	}

	ts, err := time.Parse(time.RFC3339Nano, string(buf[:i]))
	if err != nil {
		return zero, err
	}

	// Now actually consume the timestamp + space
	if _, err = ring.Read(buf[:i+1]); err != nil {
		return zero, err
	}

	return ts, nil
}

// runeSafeChunkLenFromRing determines safe chunk length from ring buffer
func runeSafeChunkLenFromRing(ring *ringbuffer.RingBuffer, maxChunkSize int, buf []byte) (int, error) {
	length := ring.Length()
	if length == 0 {
		return 0, nil
	}

	if maxChunkSize <= 0 || maxChunkSize >= length {
		return length, nil
	}

	// Check buffer
	wantLen := min(length, maxChunkSize+utf8.UTFMax)
	if cap(buf) < wantLen {
		return 0, ErrBufCapacity
	}

	n, err := ring.Peek(buf[:wantLen])
	if err != nil {
		return 0, err
	}

	chunkLen := min(maxChunkSize, n)

	// Fast path: trim from the end until last rune is complete
	for chunkLen > 0 {
		r, size := utf8.DecodeLastRune(buf[:chunkLen])

		// size == 0 shouldn't happen here; but guard anyway.
		if size == 0 {
			break
		}

		// Invalid encoding at the end => trim one byte and try again.
		if r == utf8.RuneError && size == 1 {
			chunkLen--
			continue
		}

		// Got a complete rune at the end -> everything up to here is fine
		return chunkLen, nil
	}

	// No rune fits inside maxChunkSize; return the first rune in full.
	_, size := utf8.DecodeRune(buf[:n])
	return size, nil
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

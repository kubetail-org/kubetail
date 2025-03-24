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
	"container/heap"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
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

// Return result map key
func (w WorkloadType) Key(args ...string) string {
	return fmt.Sprintf("%s/%s", w.String(), strings.Join(args, "/"))
}

// Parse string and return corresponding workload
func parseWorkloadType(workloadStr string) WorkloadType {
	switch strings.Trim(strings.ToLower(workloadStr), "s") {
	case "cronjob":
		return WorkloadTypeCronJob
	case "daemonset":
		return WorkloadTypeDaemonSet
	case "deployment":
		return WorkloadTypeDeployment
	case "job":
		return WorkloadTypeJob
	case "pod":
		return WorkloadTypePod
	case "replicaset":
		return WorkloadTypeReplicaSet
	case "statefulset":
		return WorkloadTypeStatefulSet
	default:
		return WorkloadTypeUknown
	}
}

// logProvider defines the interface for getting pod logs
type logProvider interface {
	GetLogs(ctx context.Context, source LogSource, opts *corev1.PodLogOptions) (<-chan LogRecord, error)
	GetLogsReverse(ctx context.Context, source LogSource, batchSize int64, sinceTime time.Time) (<-chan LogRecord, error)
}

// k8sLogProvider implements logProvider using Kubernetes clientset
type k8sLogProvider struct {
	clientset kubernetes.Interface
}

func newK8sLogProvider(clientset kubernetes.Interface) *k8sLogProvider {
	return &k8sLogProvider{
		clientset: clientset,
	}
}

func (p *k8sLogProvider) GetLogs(ctx context.Context, source LogSource, opts *corev1.PodLogOptions) (<-chan LogRecord, error) {
	return newPodLogStream(ctx, p.clientset, source, opts)
}

func (p *k8sLogProvider) GetLogsReverse(ctx context.Context, source LogSource, batchSize int64, sinceTime time.Time) (<-chan LogRecord, error) {
	return newPodLogStreamReverse(ctx, p.clientset, source, batchSize, sinceTime)
}

func newPodLogStream(ctx context.Context, clientset kubernetes.Interface, source LogSource, opts *corev1.PodLogOptions) (<-chan LogRecord, error) {
	outCh := make(chan LogRecord)

	if opts == nil {
		opts = &corev1.PodLogOptions{}
	}

	// Always add timestamps and set the container name from source
	opts.Timestamps = true
	opts.Container = source.ContainerName

	// Execute query
	req := clientset.CoreV1().Pods(source.Namespace).GetLogs(source.PodName, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}

	// Read in goroutine
	go func() {
		defer podLogs.Close()
		defer close(outCh)

		scanner := bufio.NewScanner(podLogs)

	Loop:
		for scanner.Scan() {
			record, err := newLogRecordFromLogLine(scanner.Text())
			if err != nil {
				continue
			}

			record.Source = source

			// Write to output channel
			select {
			case <-ctx.Done():
				break Loop
			case outCh <- record:
			}
		}
	}()

	return outCh, nil
}

// newPodLogStreamReverse creates a new log stream that fetches logs in reverse order
// It uses the Kubernetes API TailLines option to fetch logs from the end of the log file
// and streams them in reverse order (last-to-first)
func newPodLogStreamReverse(ctx context.Context, clientset kubernetes.Interface, source LogSource, batchSize int64, sinceTime time.Time) (<-chan LogRecord, error) {
	// Create a channel to send log records on
	outCh := make(chan LogRecord)

	// Get first timestamp
	firstTS, err := getFirstTimestamp(ctx, clientset, source, sinceTime)
	if err != nil {
		close(outCh)
		return nil, err
	} else if firstTS.IsZero() {
		close(outCh)
		return outCh, nil
	}

	go func() {
		defer close(outCh)

		tailLines := batchSize
		lastBatchTS := time.Time{}

		// Keep fetching batches until we've reached the beginning of the logs
	Loop:
		for {
			// Create options for this batch
			opts := &corev1.PodLogOptions{
				Timestamps: true,
				Container:  source.ContainerName,
				TailLines:  &tailLines,
			}

			// Execute query
			req := clientset.CoreV1().Pods(source.Namespace).GetLogs(source.PodName, opts)
			podLogs, err := req.Stream(ctx)
			if err != nil {
				if ctx.Err() != context.Canceled {
					// Log error but continue with what we have
					fmt.Printf("error getting logs for %s/%s: %v\n", source.Namespace, source.PodName, err)
				}
				break Loop // exit
			}

			// Read logs from this batch
			scanner := bufio.NewScanner(podLogs)
			batchRecords := []LogRecord{}
			isFirst := true
			increaseBatchSize := false

			for scanner.Scan() {
				// Check if context is done
				if ctx.Err() != nil {
					break
				}

				record, err := newLogRecordFromLogLine(scanner.Text())
				if err != nil {
					continue
				}

				// Check if log is getting ahead of us
				if isFirst {
					// If current batch starts after last batch then increase batch size
					if !lastBatchTS.IsZero() && record.Timestamp.After(lastBatchTS) {
						increaseBatchSize = true
						break
					}
					isFirst = false
				}

				// Stop reading if we've reached the beginning of the last batch
				if !lastBatchTS.IsZero() && record.Timestamp.Equal(lastBatchTS) {
					break
				}

				record.Source = source
				batchRecords = append(batchRecords, record)
			}
			podLogs.Close()

			// Check if context is done
			if ctx.Err() != nil {
				return // exit
			}

			if increaseBatchSize {
				// Increase batch size if this batch started after last batch
				batchSize = batchSize * 2
				tailLines += batchSize
				continue
			}

			// Reverse order
			slices.Reverse(batchRecords)

			// Send to output channel
			for _, record := range batchRecords {
				select {
				case <-ctx.Done():
					return // exit
				case outCh <- record:
					// Successfully sent record
				}
			}

			if batchRecords[len(batchRecords)-1].Timestamp == firstTS {
				break Loop
			}

			// Update batch timestamp
			lastBatchTS = batchRecords[0].Timestamp

			// Increase tailLines for the next batch
			tailLines += batchSize
		}
	}()

	return outCh, nil
}

// Create new log record
func newLogRecordFromLogLine(logLine string) (LogRecord, error) {
	var zero LogRecord

	parts := strings.SplitN(logLine, " ", 2)
	if len(parts) != 2 {
		return zero, fmt.Errorf("log line timestamp not found")
	}

	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return zero, err
	}

	return LogRecord{
		Timestamp: ts,
		Message:   parts[1],
	}, nil
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

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
	"container/heap"
	"context"
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

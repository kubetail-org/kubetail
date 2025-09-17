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
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
)

// FollowFrom defines the starting point for following logs
type FollowFrom string

const (
	FollowFromNoop    FollowFrom = ""
	FollowFromDefault FollowFrom = "default"
	FollowFromEnd     FollowFrom = "end"
)

const logScannerInitCapacity = 64 * 1024  // 64KB
const logScannerMaxCapacity = 1024 * 1024 // 1MB

// FetcherOptions defines options for fetching logs
type FetcherOptions struct {
	StartTime     time.Time
	StopTime      time.Time
	Grep          string
	GrepRegex     *regexp.Regexp
	FollowFrom    FollowFrom
	BatchSizeHint int64
}

// LogFetcher defines forward and backward streaming.
type LogFetcher interface {
	StreamForward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error)
	StreamBackward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error)
}

// KubeLogFetcher implements LogFetcher using Kubernetes clientset
type KubeLogFetcher struct {
	clientset kubernetes.Interface
}

// NewKubeLogFetcher creates a new KubeLogFetcher
func NewKubeLogFetcher(clientset kubernetes.Interface) *KubeLogFetcher {
	return &KubeLogFetcher{
		clientset: clientset,
	}
}

// StreamForward returns a channel of LogRecords in chronological order for the given source
func (f *KubeLogFetcher) StreamForward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	outCh := make(chan LogRecord)

	// Init options
	logOpts := &corev1.PodLogOptions{
		Timestamps: true,
		Container:  source.ContainerName,
		Follow:     opts.FollowFrom != FollowFromNoop,
	}

	if opts.FollowFrom == FollowFromEnd {
		logOpts.TailLines = ptr.To[int64](0)
	} else if !opts.StartTime.IsZero() {
		logOpts.SinceTime = &metav1.Time{Time: opts.StartTime}
	}

	// Execute query
	req := f.clientset.CoreV1().Pods(source.Namespace).GetLogs(source.PodName, logOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		// Check if the error is "pods <pod-name> not found"
		if strings.Contains(err.Error(), fmt.Sprintf("pods \"%s\" not found", source.PodName)) {
			// Return a closed channel instead of an error
			close(outCh)
			return outCh, nil
		}
		return nil, err
	}

	// Read in goroutine
	go func() {
		defer podLogs.Close()
		defer close(outCh)

		scanner := bufio.NewScanner(podLogs)

		buffer := make([]byte, logScannerInitCapacity)
		scanner.Buffer(buffer, logScannerMaxCapacity)

		for scanner.Scan() {
			record, err := newLogRecordFromLogLine(scanner.Text())
			if err != nil {
				continue
			}

			// Check start time
			if !opts.StartTime.IsZero() && record.Timestamp.Before(opts.StartTime) {
				continue
			}

			// Check stop time
			if !opts.StopTime.IsZero() && record.Timestamp.After(opts.StopTime) {
				break
			}

			// Check grep
			if opts.GrepRegex != nil && !opts.GrepRegex.MatchString(record.Message) {
				continue
			}

			// Set source
			record.Source = source

			// Write to output channel
			select {
			case <-ctx.Done():
				return
			case outCh <- record:
			}
		}

		// Handle errors
		if scanner.Err() != nil {
			select {
			case <-ctx.Done():
			case outCh <- LogRecord{err: scanner.Err()}:
			default:
			}
			return
		}
	}()

	return outCh, nil
}

// StreamBackward returns a channel of LogRecords in reverse chronological order for the given source
func (f *KubeLogFetcher) StreamBackward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	outCh := make(chan LogRecord)

	// Get first timestamp
	firstTS, err := getFirstTimestamp(ctx, f.clientset, source, opts.StartTime)
	if err != nil {
		close(outCh)
		return nil, err
	} else if firstTS.IsZero() {
		close(outCh)
		return outCh, nil
	}

	// Use the Kubernetes API TailLines option to fetch logs from the end of the log file
	// and streams them in reverse order (last-to-first)
	go func() {
		defer close(outCh)

		// Initialize batch size with hint value
		batchSize := max(opts.BatchSizeHint, 10)

		tailLines := batchSize
		lastBatchStartTS := time.Time{}

		// Keep fetching batches until we've reached the beginning of the logs
	Loop:
		for {
			// Create options for this batch
			podLogOpts := &corev1.PodLogOptions{
				Timestamps: true,
				Container:  source.ContainerName,
				TailLines:  &tailLines,
			}

			// Execute query
			req := f.clientset.CoreV1().Pods(source.Namespace).GetLogs(source.PodName, podLogOpts)
			podLogs, err := req.Stream(ctx)
			if err != nil {
				// Check if the error is "pods <pod-name> not found"
				if ctx.Err() != context.Canceled && !strings.Contains(err.Error(), fmt.Sprintf("pods \"%s\" not found", source.PodName)) {
					// Log error but continue with what we have
					fmt.Printf("error getting logs for %s/%s: %v\n", source.Namespace, source.PodName, err)
				}
				break Loop // exit
			}

			// Read logs from this batch
			scanner := bufio.NewScanner(podLogs)

			buffer := make([]byte, logScannerInitCapacity)
			scanner.Buffer(buffer, logScannerMaxCapacity)

			batchRecords := []LogRecord{}
			isEmpty := true
			isFirst := true
			increaseBatchSize := false

			for scanner.Scan() {
				isEmpty = false

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
					if !lastBatchStartTS.IsZero() && (record.Timestamp.Equal(lastBatchStartTS) || record.Timestamp.After(lastBatchStartTS)) {
						increaseBatchSize = true
						break
					}
					isFirst = false
				}

				// Stop reading if we've reached the beginning of the last batch
				if !lastBatchStartTS.IsZero() && record.Timestamp.Equal(lastBatchStartTS) {
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

			// Handle errors
			if scanner.Err() != nil {
				select {
				case <-ctx.Done():
				case outCh <- LogRecord{err: scanner.Err()}:
				default:
				}
				return
			}

			if increaseBatchSize {
				// Increase batch size if this batch started after last batch
				batchSize = batchSize * 2
				tailLines += batchSize
				continue
			}

			if isEmpty {
				break Loop
			}

			// Update batch timestamp
			if len(batchRecords) > 0 {
				lastBatchStartTS = batchRecords[0].Timestamp
			}

			// Reverse order
			slices.Reverse(batchRecords)

			// Send to output channel
			for _, record := range batchRecords {
				// Check stop time
				if !opts.StopTime.IsZero() && record.Timestamp.After(opts.StopTime) {
					continue
				}

				// Check start time
				if !opts.StartTime.IsZero() && record.Timestamp.Before(opts.StartTime) {
					break Loop
				}

				// Check grep
				if opts.GrepRegex != nil && !opts.GrepRegex.MatchString(record.Message) {
					continue
				}

				select {
				case <-ctx.Done():
					return // exit
				case outCh <- record:
					// Successfully sent record
				}
			}

			// Check if we've reached the first batch
			if lastBatchStartTS.Equal(firstTS) || lastBatchStartTS.Before(firstTS) {
				break Loop
			}

			// Increase tailLines for the next batch
			tailLines += batchSize
		}
	}()

	return outCh, nil
}

// AgentLogFetcher implements LogFetcher using Kubetail Cluster Agent
type AgentLogFetcher struct {
	grpcDispatcher *grpcdispatcher.Dispatcher
}

// NewAgentLogFetcher creates a new AgentLogFetcher
func NewAgentLogFetcher(grpcDispatcher *grpcdispatcher.Dispatcher) *AgentLogFetcher {
	return &AgentLogFetcher{
		grpcDispatcher: grpcDispatcher,
	}
}

// StreamForward returns a channel of LogRecords in chronological order for the given source
func (f *AgentLogFetcher) StreamForward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	// Init output channel
	outCh := make(chan LogRecord)

	err := f.grpcDispatcher.UnicastSubscribeOnce(ctx, source.Metadata.Node, func(ctx context.Context, conn *grpc.ClientConn) {
		defer close(outCh)

		// init client
		c := clusteragentpb.NewLogRecordsServiceClient(conn)

		// Init gRPC request
		req := &clusteragentpb.LogRecordsStreamRequest{
			Namespace:     source.Namespace,
			PodName:       source.PodName,
			ContainerName: source.ContainerName,
			ContainerId:   source.ContainerID,
			Grep:          opts.Grep,
		}

		if !opts.StartTime.IsZero() {
			req.StartTime = opts.StartTime.Format(time.RFC3339Nano)
		}

		if !opts.StopTime.IsZero() {
			req.StopTime = opts.StopTime.Format(time.RFC3339Nano)
		}

		switch opts.FollowFrom {
		case FollowFromNoop:
			req.FollowFrom = clusteragentpb.FollowFrom_NOOP
		case FollowFromDefault:
			req.FollowFrom = clusteragentpb.FollowFrom_DEFAULT
		case FollowFromEnd:
			req.FollowFrom = clusteragentpb.FollowFrom_END
		default:
			return
		}

		// Execute
		stream, err := c.StreamForward(ctx, req)
		if err != nil {
			// Send error
			outCh <- LogRecord{err: err}
			return
		}

		for {
			ev, err := stream.Recv()

			// Handle errors
			if err != nil {
				// Ignore normal errors
				if errors.Is(err, io.EOF) ||
					errors.Is(err, context.Canceled) ||
					status.Code(err) == codes.Canceled {
					break
				}

				// Send unexpected error
				outCh <- LogRecord{err: err}
				break
			}

			// Send event
			outCh <- LogRecord{
				Message:   ev.Message,
				Timestamp: ev.Timestamp.AsTime(),
				Source:    source,
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return outCh, nil
}

// StreamBackward returns a channel of LogRecords in reverse chronological order for the given source
func (f *AgentLogFetcher) StreamBackward(ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error) {
	// Init output channel
	outCh := make(chan LogRecord)

	err := f.grpcDispatcher.UnicastSubscribeOnce(ctx, source.Metadata.Node, func(ctx context.Context, conn *grpc.ClientConn) {
		defer close(outCh)

		// init client
		c := clusteragentpb.NewLogRecordsServiceClient(conn)

		// Init gRPC request
		req := &clusteragentpb.LogRecordsStreamRequest{
			Namespace:     source.Namespace,
			PodName:       source.PodName,
			ContainerName: source.ContainerName,
			ContainerId:   source.ContainerID,
			Grep:          opts.Grep,
		}

		if !opts.StartTime.IsZero() {
			req.StartTime = opts.StartTime.Format(time.RFC3339Nano)
		}

		if !opts.StopTime.IsZero() {
			req.StopTime = opts.StopTime.Format(time.RFC3339Nano)
		}

		// Execute
		stream, err := c.StreamBackward(ctx, req)
		if err != nil {
			// Send error
			outCh <- LogRecord{err: err}
			return
		}

		for {
			ev, err := stream.Recv()

			// Handle errors
			if err != nil {
				// Ignore normal errors
				if errors.Is(err, io.EOF) ||
					errors.Is(err, context.Canceled) ||
					status.Code(err) == codes.Canceled {
					break
				}

				// Send unexpected error
				outCh <- LogRecord{err: err}
				break
			}

			// Send event
			outCh <- LogRecord{
				Message:   ev.Message,
				Timestamp: ev.Timestamp.AsTime(),
				Source:    source,
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return outCh, nil
}

// Copyright 2024 Andres Morey
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

package graph

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/sosodev/duration"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/graph/model"
)

type Key int

const K8STokenCtxKey Key = iota

// Tail enums
type TailSince int8
type TailUntil int8

const (
	TailSinceUnset TailSince = iota
	TailSinceBeginning
	TailSinceNow
	TailSinceLine
	TailSinceTime

	TailUntilUnset TailUntil = iota
	TailUntilForever
	TailUntilLine
	TailUntilTime
)

type TailArgs struct {
	After string
	Since string
	Until string
	Limit uint
}

func watchEventProxyChannel(ctx context.Context, watchAPI watch.Interface) <-chan *watch.Event {
	evCh := watchAPI.ResultChan()
	outCh := make(chan *watch.Event)

	go func() {
	Loop:
		for {
			select {
			case <-ctx.Done():
				// listener closed connection
				break Loop
			case ev := <-evCh:
				// just-in-case (maybe this is unnecessary)
				if ev.Type == "" || ev.Object == nil {
					break Loop
				}

				// exit if error
				if ev.Type == watch.Error {
					status, ok := ev.Object.(*metav1.Status)
					if ok {
						transport.AddSubscriptionError(ctx, NewWatchError(status))
					} else {
						transport.AddSubscriptionError(ctx, ErrInternalServerError)
					}
					break Loop
				}

				// write to output channel
				outCh <- &ev
			}
		}

		// cleanup
		watchAPI.Stop()
		close(outCh)
	}()

	return outCh
}

func getHealth(ctx context.Context, clientset kubernetes.Interface, endpoint string) model.HealthCheckResponse {
	resp := model.HealthCheckResponse{
		Status:    model.HealthCheckStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	// execute request
	_, err := clientset.Discovery().RESTClient().Get().AbsPath("/" + endpoint).DoRaw(ctx)
	if err != nil {
		resp.Status = model.HealthCheckStatusFailure
		resp.Message = ptr.To[string](err.Error())
	}

	return resp
}

func watchHealthChannel(ctx context.Context, clientset kubernetes.Interface, endpoint string) <-chan model.HealthCheckResponse {
	outCh := make(chan model.HealthCheckResponse)

	go func() {
		var lastMessage *string
		ticker := time.NewTicker(3 * time.Second)

		resp := getHealth(ctx, clientset, endpoint)
		lastMessage = resp.Message
		outCh <- resp

	Loop:
		for {
			select {
			case <-ctx.Done():
				// listener closed connection
				break Loop
			case <-ticker.C:
				resp := getHealth(ctx, clientset, endpoint)
				if !ptr.Equal(lastMessage, resp.Message) {
					lastMessage = resp.Message
					outCh <- resp
				}
			}
		}

		// cleanup
		ticker.Stop()
		close(outCh)
	}()

	return outCh
}

func toListOptions(options *metav1.ListOptions) metav1.ListOptions {
	opts := metav1.ListOptions{}
	if options != nil {
		opts = *options
	}
	return opts
}

func toGetOptions(options *metav1.GetOptions) metav1.GetOptions {
	opts := metav1.GetOptions{}
	if options != nil {
		opts = *options
	}
	return opts
}

func toPodLogOptions(options *corev1.PodLogOptions) corev1.PodLogOptions {
	opts := corev1.PodLogOptions{}
	if options != nil {
		opts = *options
	}
	return opts
}

func typeassertRuntimeObject[T any](object runtime.Object) (T, error) {
	var zeroVal T

	if object == nil {
		return zeroVal, nil
	}

	switch o := object.(type) {
	case T:
		return o, nil
	default:
		return zeroVal, fmt.Errorf("not expecting type %T", o)
	}
}

func newLogRecordFromLogLine(logLine string) model.LogRecord {
	// handle logs from kubernetes fake clientset
	if logLine == "fake logs" {
		return model.LogRecord{
			Timestamp: time.Now().UTC(),
			Message:   "fake logs",
		}
	}

	parts := strings.SplitN(logLine, " ", 2)
	if len(parts) != 2 {
		panic(errors.New("log line timestamp not found"))
	}

	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		panic(err)
	}

	return model.LogRecord{
		Timestamp: ts,
		Message:   parts[1],
	}
}

func tailPodLog(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, container *string, args TailArgs) (<-chan model.LogRecord, error) {
	// init output channel
	ch := make(chan model.LogRecord)

	now := time.Now()

	var (
		tailSince TailSince
		tailUntil TailUntil
		sinceLine int64
		sinceTime time.Time
		//untilLine int64
		untilTime time.Time
	)

	// handle `since`
	since := strings.TrimSpace(args.Since)
	if strings.ToLower(since) == "beginning" {
		tailSince = TailSinceBeginning
	} else if strings.ToLower(since) == "now" {
		tailSince = TailSinceNow
	} else if line, err := strconv.ParseInt(since, 10, 64); err == nil {
		tailSince = TailSinceLine
		sinceLine = line
		if sinceLine >= 0 {
			return nil, fmt.Errorf("`since` line argument must be less than 0 (`%s`)", args.Since)
		}
	} else if timeAgo, err := duration.Parse(since); err == nil {
		tailSince = TailSinceTime
		sinceTime = now.Add(-1 * timeAgo.ToTimeDuration())
	} else if ts, err := time.Parse(time.RFC3339Nano, since); err == nil {
		tailSince = TailSinceTime
		sinceTime = ts
		if sinceTime.After(now) {
			return nil, fmt.Errorf("`since` time argument (%s) must be in past (current time: %s)", args.Since, now.UTC().Format(time.RFC3339Nano))
		}
	} else {
		return nil, fmt.Errorf("did not understand `since` (`%s`)", since)
	}

	// handle `until`
	until := strings.TrimSpace(args.Until)
	if strings.ToLower(until) == "forever" {
		tailUntil = TailUntilForever
	} else if strings.ToLower(until) == "now" {
		tailUntil = TailUntilTime
		untilTime = now
	} else if _, err := strconv.ParseInt(until, 10, 64); err == nil {
		return nil, fmt.Errorf("`until` line argument not currently supported")
	} else if timeAgo, err := duration.Parse(until); err == nil {
		tailUntil = TailUntilTime
		untilTime = now.Add(-1 * timeAgo.ToTimeDuration())
	} else if ts, err := time.Parse(time.RFC3339Nano, until); err == nil {
		tailUntil = TailUntilTime
		untilTime = ts
		if untilTime.After(now) {
			return nil, fmt.Errorf("`until` time argument (%s) must be in past (current time: %s)", args.Until, now.UTC().Format(time.RFC3339Nano))
		}
	} else {
		return nil, fmt.Errorf("did not understand `until` (`%s`)", until)
	}

	// handle `after`
	if ts, err := time.Parse(time.RFC3339Nano, args.After); err == nil {
		tailSince = TailSinceTime
		sinceTime = ts.Add(1 * time.Nanosecond)
	}

	exitEarly := func() (<-chan model.LogRecord, error) {
		close(ch)
		return ch, nil
	}

	// exit early if untilTime < startTime
	if tailSince == TailSinceTime && tailUntil == TailUntilTime && untilTime.Before(sinceTime) {
		return exitEarly()
	}

	// exit early if tail since "now" and untilTime < "now"
	if tailSince == TailSinceNow && tailUntil == TailUntilTime && untilTime.Before(now) {
		return exitEarly()
	}

	// init kubernetes logging options
	opts := &corev1.PodLogOptions{
		Timestamps: true,
	}

	if container != nil {
		opts.Container = *container
	}

	switch tailSince {
	case TailSinceNow:
		opts.TailLines = ptr.To[int64](0)
	case TailSinceTime:
		t := metav1.NewTime(sinceTime)
		opts.SinceTime = &t
	case TailSinceLine:
		opts.TailLines = ptr.To[int64](-1 * sinceLine)
	}

	if tailUntil == TailUntilForever {
		opts.Follow = true
	}

	// execute query
	req := clientset.CoreV1().Pods(namespace).GetLogs(name, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		defer podLogs.Close()

		n := uint(0)

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			logRecord := newLogRecordFromLogLine(scanner.Text())

			// ignore if log record comes before time window
			if tailSince == TailSinceTime && logRecord.Timestamp.Before(sinceTime) {
				continue
			}

			// exit if log record comes after time window
			if tailUntil == TailUntilTime && logRecord.Timestamp.After(untilTime) {
				break
			}

			ch <- logRecord

			n += 1

			// exit if we've reached `Limit`
			if args.Limit != 0 && n >= args.Limit {
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

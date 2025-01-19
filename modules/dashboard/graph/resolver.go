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

package graph

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	sharedk8shelpers "github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/sosodev/duration"

	"github.com/kubetail-org/kubetail/modules/dashboard/graph/model"
	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
	"github.com/kubetail-org/kubetail/modules/dashboard/internal/k8shelpers"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	config            *config.Config
	cm                k8shelpers.ConnectionManager
	hm                clusterapi.HealthMonitor
	environment       config.Environment
	allowedNamespaces []string
}

// Teardown
func (r *Resolver) Teardown() {
	r.hm.Shutdown()
}

// listResource
func (r *Resolver) listResource(ctx context.Context, kubeContext *string, namespace *string, options *metav1.ListOptions, modelPtr runtime.Object) error {
	// Deref namespace
	nsList, err := sharedk8shelpers.DerefNamespaceToList(r.allowedNamespaces, namespace, r.cm.GetDefaultNamespace(kubeContext))
	if err != nil {
		return err
	}

	// Get client
	dynamicClient, err := r.cm.GetOrCreateDynamicClient(kubeContext)
	if err != nil {
		return err
	}

	gvr, err := GetGVR(modelPtr)
	if err != nil {
		return err
	}

	client := dynamicClient.Resource(gvr)

	// in it list options
	opts := toListOptions(options)

	// execute requests
	list, err := func() (*unstructured.UnstructuredList, error) {
		if len(nsList) == 1 {
			return client.Namespace(nsList[0]).List(ctx, opts)
		} else {
			return listResourceMulti(ctx, client, nsList, opts)
		}
	}()
	if err != nil {
		return err
	}

	// return de-serialized object
	return runtime.DefaultUnstructuredConverter.FromUnstructured(list.UnstructuredContent(), modelPtr)
}

// watchResourceMulti
func (r *Resolver) watchResourceMulti(ctx context.Context, kubeContext *string, namespace *string, options *metav1.ListOptions, gvr schema.GroupVersionResource) (<-chan *watch.Event, error) {
	// Deref namespace
	nsList, err := sharedk8shelpers.DerefNamespaceToList(r.allowedNamespaces, namespace, r.cm.GetDefaultNamespace(kubeContext))
	if err != nil {
		return nil, err
	}

	// Get client
	dynamicClient, err := r.cm.GetOrCreateDynamicClient(kubeContext)
	if err != nil {
		return nil, err
	}

	client := dynamicClient.Resource(gvr)

	// init list options
	opts := toListOptions(options)

	// decode resource version
	// TODO: fix me
	resourceVersionMap := map[string]string{}
	if len(nsList) == 1 {
		resourceVersionMap[nsList[0]] = opts.ResourceVersion
	} else {
		if tmp, err := decodeResourceVersionMulti(opts.ResourceVersion); err != nil {
			return nil, err
		} else {
			resourceVersionMap = tmp
		}
	}

	// init watch api's
	watchAPIs := []watch.Interface{}
	for _, ns := range nsList {
		// init options
		thisOpts := opts

		thisResourceVersion, exists := resourceVersionMap[ns]
		if exists {
			thisOpts.ResourceVersion = thisResourceVersion
		} else {
			thisOpts.ResourceVersion = ""
		}

		// init watch api
		watchAPI, err := client.Namespace(ns).Watch(ctx, thisOpts)
		if err != nil {
			return nil, err
		}
		watchAPIs = append(watchAPIs, watchAPI)
	}

	// start watchers
	outCh := make(chan *watch.Event)
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	for _, watchAPI := range watchAPIs {
		wg.Add(1)
		go watchResource(ctx, watchAPI, outCh, cancel, &wg)
	}

	// cleanup
	go func() {
		wg.Wait()
		cancel()
		close(outCh)
	}()

	return outCh, nil
}

// kubernetesAPIHealthzGet
func (r *Resolver) kubernetesAPIHealthzGet(ctx context.Context, kubeContext *string) *model.HealthCheckResponse {
	resp := &model.HealthCheckResponse{
		Status:    model.HealthCheckStatusFailure,
		Timestamp: time.Now().UTC(),
	}

	// Get client
	clientset, err := r.cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		resp.Message = ptr.To(err.Error())
		return resp
	}

	// Execute request
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err = clientset.CoreV1().RESTClient().Get().AbsPath("/livez").DoRaw(ctx)
	if err != nil {
		if ctx.Err() != nil {
			resp.Message = ptr.To("Bad Gateway")
		} else {
			resp.Message = ptr.To(err.Error())
		}
		return resp
	}

	resp.Status = model.HealthCheckStatusSuccess
	return resp
}

// podLogHead
func (r *Resolver) podLogHead(ctx context.Context, kubeContext *string, namespace string, name string, container *string, args HeadArgs) (*model.PodLogQueryResponse, error) {
	clientset, err := r.cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		return nil, err
	}

	var (
		headSince HeadSince
		sinceTime time.Time
	)

	// handle `since`
	since := strings.TrimSpace(args.Since)
	if strings.ToLower(since) == "beginning" {
		headSince = HeadSinceBeginning
	} else if timeAgo, err := duration.Parse(since); err == nil {
		headSince = HeadSinceTime
		sinceTime = time.Now().Add(-1 * timeAgo.ToTimeDuration())
	} else if ts, err := time.Parse(time.RFC3339Nano, since); err == nil {
		headSince = HeadSinceTime
		sinceTime = ts
	} else {
		return nil, fmt.Errorf("did not understand `since` (`%s`)", since)
	}

	// handle `after`
	if ts, err := time.Parse(time.RFC3339Nano, args.After); err == nil {
		headSince = HeadSinceTime
		sinceTime = ts.Add(1 * time.Nanosecond)
	}

	// init kubernetes logging options
	opts := &corev1.PodLogOptions{
		Timestamps: true,
		Follow:     false,
	}

	if container != nil {
		opts.Container = *container
	}

	if headSince == HeadSinceTime {
		t := metav1.NewTime(sinceTime)
		opts.SinceTime = &t
	}

	// execute query
	req := clientset.CoreV1().Pods(namespace).GetLogs(name, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer podLogs.Close()

	// iterate through results
	records := []*model.LogRecord{}
	n := uint(0)

	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		logRecord, err := newLogRecordFromLogLine(scanner.Text())
		if err != nil {
			continue
		}

		// ignore if log record comes before time window
		if headSince == HeadSinceTime && logRecord.Timestamp.Before(sinceTime) {
			continue
		}

		n += 1

		// exit if we've reached `First`
		if args.First != 0 && n >= args.First+1 {
			break
		}

		records = append(records, logRecord)
	}

	// stop streaming asap
	podLogs.Close()

	// build response
	response := &model.PodLogQueryResponse{}

	// page info
	response.PageInfo = &model.PageInfo{}

	if args.First != 0 && n > args.First {
		response.PageInfo.HasNextPage = true
	}

	if len(records) > 0 {
		response.PageInfo.EndCursor = ptr.To[string](records[len(records)-1].Timestamp.Format(time.RFC3339Nano))
	} else if headSince == HeadSinceTime {
		response.PageInfo.EndCursor = ptr.To[string](sinceTime.Format(time.RFC3339Nano))
	} else if headSince == HeadSinceBeginning {
		response.PageInfo.EndCursor = ptr.To[string]("BEGINNING")
	}

	response.Results = records

	return response, nil
}

func (r *Resolver) podLogTail(ctx context.Context, kubeContext *string, namespace string, name string, container *string, args TailArgs) (*model.PodLogQueryResponse, error) {
	clientset, err := r.cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		return nil, err
	}

	var (
		firstTS   time.Time
		tailLines int64
		tailUntil TailUntil
		untilTime time.Time
	)

	// handle `before`
	if args.Before != "" {
		cursor, err := decodeTailCursor(args.Before)
		if err != nil {
			return nil, err
		}
		firstTS = cursor.FirstTS
		tailLines = cursor.TailLines
		tailUntil = TailUntilTime
		untilTime = cursor.Time.Add(-1 * time.Nanosecond)
	}

	// first timestamp
	if firstTS.IsZero() {
		ts, err := getFirstTimestamp(ctx, clientset, namespace, name, container)
		switch {
		case err == io.EOF:
			// empty log
			return &model.PodLogQueryResponse{PageInfo: &model.PageInfo{EndCursor: ptr.To[string]("BEGINNING")}}, nil
		case err != nil:
			// other error
			return nil, err
		default:
			firstTS = ts
		}
	}

	// look back with increasing batch size until we have enough records or reach beginning
	records := []*model.LogRecord{}
	batchSize := int64(args.Last)

Loop:
	for {
		// look back farther with each iteration
		tailLines += batchSize

		// init kubernetes logging options
		opts := &corev1.PodLogOptions{
			Timestamps: true,
			Follow:     false,
			TailLines:  ptr.To[int64](tailLines),
		}

		if container != nil {
			opts.Container = *container
		}

		// execute query
		req := clientset.CoreV1().Pods(namespace).GetLogs(name, opts)
		podLogs, err := req.Stream(ctx)
		if err != nil {
			return nil, err
		}
		defer podLogs.Close()

		loopRecords := []*model.LogRecord{}

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			logRecord, err := newLogRecordFromLogLine(scanner.Text())
			if err != nil {
				continue
			}

			// exit if log record comes after time window
			if tailUntil == TailUntilTime && logRecord.Timestamp.After(untilTime) {
				break
			}

			loopRecords = append(loopRecords, logRecord)
		}

		// prepend loop records to outer records
		records = append(loopRecords, records...)

		// stop streaming asap
		podLogs.Close()

		// exit if we have enough records
		if len(records) >= int(args.Last) {
			break Loop
		}

		// exit if we've reached beginning
		if len(records) > 0 && records[0].Timestamp == firstTS {
			break Loop
		}

		// update loop time window
		if len(records) > 0 {
			untilTime = records[0].Timestamp.Add(-1 * time.Nanosecond)
		}

		// increase batch size with each iteration
		batchSize += batchSize / 2
	}

	// build response
	response := &model.PodLogQueryResponse{}

	// page info
	response.PageInfo = &model.PageInfo{}

	if len(records) == 0 {
		response.PageInfo.EndCursor = ptr.To[string]("BEGINNING")
	} else {
		// get last N items
		startIndex := len(records) - int(args.Last)
		if startIndex < 0 {
			startIndex = 0
		}
		response.Results = records[startIndex:]

		// start cursor
		if records[0].Timestamp != firstTS {
			cursorStr, _ := encodeTailCursor(TailCursor{
				TailLines: tailLines,
				Time:      records[0].Timestamp,
				FirstTS:   firstTS,
			})
			response.PageInfo.StartCursor = &cursorStr
			response.PageInfo.HasPreviousPage = true
		}

		// end cursor
		response.PageInfo.EndCursor = ptr.To[string](records[len(records)-1].Timestamp.Format(time.RFC3339Nano))
		if args.Before != "" {
			response.PageInfo.HasNextPage = true
		}
	}

	return response, nil
}

// podLogFollow
func (r *Resolver) podLogFollow(ctx context.Context, kubeContext *string, namespace string, name string, container *string, args FollowArgs) (<-chan *model.LogRecord, error) {
	clientset, err := r.cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		return nil, err
	}

	// init output channel
	ch := make(chan *model.LogRecord)

	var sinceTime time.Time

	// handle `since`
	since := strings.TrimSpace(args.Since)
	if strings.ToLower(since) == "beginning" {
		// do nothing
	} else if strings.ToLower(since) == "now" {
		sinceTime = time.Now()
	} else if ts, err := time.Parse(time.RFC3339Nano, since); err == nil {
		sinceTime = ts
	} else {
		return nil, fmt.Errorf("did not understand `since` (`%s`)", since)
	}

	// handle `after`
	after := strings.TrimSpace(args.After)
	if strings.ToLower(after) == "beginning" {
		sinceTime = time.Time{}
	} else if ts, err := time.Parse(time.RFC3339Nano, args.After); err == nil {
		sinceTime = ts.Add(1 * time.Nanosecond)
	}

	// init kubernetes logging options
	opts := &corev1.PodLogOptions{
		Timestamps: true,
		Follow:     true,
	}

	if container != nil {
		opts.Container = *container
	}

	if !sinceTime.IsZero() {
		t := metav1.NewTime(sinceTime)
		opts.SinceTime = &t
	}

	// execute query
	req := clientset.CoreV1().Pods(namespace).GetLogs(name, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}

	go func() {
		defer podLogs.Close()

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			logRecord, err := newLogRecordFromLogLine(scanner.Text())
			if err != nil {
				continue
			}

			// ignore if log record comes before time window
			if logRecord.Timestamp.Before(sinceTime) {
				continue
			}

			ch <- logRecord
		}
		close(ch)
	}()

	return ch, nil
}

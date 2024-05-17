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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/sosodev/duration"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/graph/model"
)

type Key int

const K8STokenCtxKey Key = iota

// Head enums
type HeadSince int8

const (
	HeadSinceUnset HeadSince = iota
	HeadSinceBeginning
	HeadSinceTime
)

// Tail enums
type TailUntil int8

const (
	TailUntilUnset TailUntil = iota
	TailUntilNow
	TailUntilTime
)

// Tail cursor
type TailCursor struct {
	TailLines int64     `json:"tail_lines"`
	Time      time.Time `json:"time"`
	FirstTS   time.Time `json:"first_ts"`
}

// Log API args
type HeadArgs struct {
	After string
	Since string
	First uint
}

type TailArgs struct {
	Before string
	Last   uint
}

type FollowArgs struct {
	After string
	Since string
}

// GetGVR
func GetGVR(obj runtime.Object) (schema.GroupVersionResource, error) {
	switch (obj).(type) {
	case *appsv1.DaemonSet, *appsv1.DaemonSetList:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, nil
	case *appsv1.Deployment, *appsv1.DeploymentList:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, nil
	case *appsv1.ReplicaSet, *appsv1.ReplicaSetList:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, nil
	case *appsv1.StatefulSet, *appsv1.StatefulSetList:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, nil
	case *batchv1.Job, *batchv1.JobList:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, nil
	case *batchv1.CronJob, *batchv1.CronJobList:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, nil
	case *corev1.Pod, *corev1.PodList:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("not implemented: %T", obj)
	}
}

// Represents response from fetchListResource()
type FetchResponse struct {
	Namespace string
	Result    *unstructured.UnstructuredList
	Error     error
}

// represents multi-namespace continue token
type continueMultiToken struct {
	ResourceVersions map[string]string `json:"rv"`
	StartKey         string            `json:"start"`
}

// encode resource version
func encodeResourceVersionMulti(resourceVersionMap map[string]string) (string, error) {
	// json encode
	resourceVersionBytes, err := json.Marshal(resourceVersionMap)
	if err != nil {
		return "", err
	}

	// base64 encode
	return base64.StdEncoding.EncodeToString(resourceVersionBytes), nil
}

// decode resource version
func decodeResourceVersionMulti(resourceVersionToken string) (map[string]string, error) {
	resourceVersionMap := map[string]string{}

	if resourceVersionToken == "" {
		return resourceVersionMap, nil
	}

	// base64 decode
	resourceVersionBytes, err := base64.StdEncoding.DecodeString(resourceVersionToken)
	if err != nil {
		return nil, err
	}

	// json decode
	err = json.Unmarshal(resourceVersionBytes, &resourceVersionMap)
	if err != nil {
		return nil, err
	}

	return resourceVersionMap, nil
}

// encode continue token
func encodeContinueMulti(resourceVersions map[string]string, startKey string) (string, error) {
	token := continueMultiToken{ResourceVersions: resourceVersions, StartKey: startKey}

	// json-encoding
	tokenBytes, err := json.Marshal(token)
	if err != nil {
		return "", err
	}

	// base64-encode
	return base64.StdEncoding.EncodeToString(tokenBytes), nil
}

// decode continue token
func decodeContinueMulti(tokenStr string) (map[string]string, error) {
	if tokenStr == "" {
		return map[string]string{}, nil
	}

	// base64-decode
	tokenBytes, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, err
	}

	// json-decode
	token := &continueMultiToken{}
	err = json.Unmarshal(tokenBytes, token)
	if err != nil {
		return nil, err
	}

	// generate continue tokens
	continueMap := map[string]string{}
	for namespace, rvStr := range token.ResourceVersions {
		rvInt64, err := strconv.ParseInt(rvStr, 10, 64)
		if err != nil {
			return nil, err
		}

		continueStr, err := storage.EncodeContinue("/"+token.StartKey+"\u0000", "/", rvInt64)
		if err != nil {
			return nil, err
		}
		continueMap[namespace] = continueStr
	}

	return continueMap, nil
}

// mergeResults
func mergeResults(responses []FetchResponse, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	// loop through results
	items := []unstructured.Unstructured{}
	remainingItemCount := int64(0)
	resourceVersionMap := map[string]string{}

	for _, resp := range responses {
		// exit if any query resulted in error
		if resp.Error != nil {
			return nil, resp.Error
		}

		result := resp.Result

		// metadata
		remainingItemCount += ptr.Deref(result.GetRemainingItemCount(), 0)
		resourceVersionMap[resp.Namespace] = result.GetResourceVersion()

		// items
		items = append(items, result.Items...)
	}

	// sort items
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetName() < items[j].GetName()
	})

	// slice items
	ignoreCount := int64(len(items)) - options.Limit
	if ignoreCount > 0 {
		remainingItemCount += ignoreCount
		items = items[:options.Limit]
	}

	// encode resourceVersionMap
	resourceVersion, err := encodeResourceVersionMulti(resourceVersionMap)
	if err != nil {
		return nil, err
	}

	// generate continue token
	var continueToken string
	if len(items) > 0 && remainingItemCount > 0 {
		continueToken, err = encodeContinueMulti(resourceVersionMap, items[len(items)-1].GetName())
		if err != nil {
			return nil, err
		}
	}

	// init merged object
	output := new(unstructured.UnstructuredList)
	output.SetRemainingItemCount(&remainingItemCount)
	output.SetResourceVersion(resourceVersion)
	output.SetContinue(continueToken)
	output.Items = items

	return output, nil
}

// listResourceMulti
func listResourceMulti(ctx context.Context, client dynamic.NamespaceableResourceInterface, namespaces []string, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	var wg sync.WaitGroup
	ch := make(chan FetchResponse, len(namespaces))

	// decode continue token
	continueMap, err := decodeContinueMulti(options.Continue)
	if err != nil {
		return nil, err
	}

	// execute queries
	for _, namespace := range namespaces {
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()

			thisOpts := options

			thisContinue, exists := continueMap[namespace]
			if exists {
				thisOpts.Continue = thisContinue
			} else {
				thisOpts.Continue = ""
			}

			list, err := client.Namespace(namespace).List(ctx, thisOpts)
			if err != nil {
				ch <- FetchResponse{Error: err}
				return
			}

			ch <- FetchResponse{Namespace: namespace, Result: list}
		}(namespace)
	}

	wg.Wait()
	close(ch)

	// gather responses
	responses := make([]FetchResponse, len(namespaces))
	i := 0
	for resp := range ch {
		responses[i] = resp
		i += 1
	}

	// merge results
	return mergeResults(responses, options)
}

// listResource
func listResource(r *queryResolver, ctx context.Context, namespace *string, options *metav1.ListOptions, modelPtr runtime.Object) error {
	// init client
	gvr, err := GetGVR(modelPtr)
	if err != nil {
		return err
	}

	client := r.K8SDynamicClient(ctx).Resource(gvr)

	// init namespaces
	namespaces, err := r.ToNamespaces(namespace)
	if err != nil {
		return err
	}

	// init list options
	opts := toListOptions(options)

	// execute requests
	list, err := func() (*unstructured.UnstructuredList, error) {
		if len(namespaces) == 1 {
			return client.Namespace(namespaces[0]).List(ctx, opts)
		} else {
			return listResourceMulti(ctx, client, namespaces, opts)
		}
	}()
	if err != nil {
		return err
	}

	// return de-serialized object
	return runtime.DefaultUnstructuredConverter.FromUnstructured(list.UnstructuredContent(), modelPtr)
}

// watchEventProxyChannel
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

// watchResource
func watchResource(ctx context.Context, watchAPI watch.Interface, outCh chan<- *watch.Event, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	evCh := watchAPI.ResultChan()

Loop:
	for {
		select {
		case <-ctx.Done():
			// listener closed connection or another goroutine encountered an error
			break Loop
		case ev := <-evCh:
			// just-in-case (maybe this is unnecessary)
			if ev.Type == "" || ev.Object == nil {
				// stop all
				cancel()
			}

			// exit if error
			if ev.Type == watch.Error {
				status, ok := ev.Object.(*metav1.Status)
				if ok {
					transport.AddSubscriptionError(ctx, NewWatchError(status))
				} else {
					transport.AddSubscriptionError(ctx, ErrInternalServerError)
				}

				// stop all
				cancel()
			}

			// write to output channel
			outCh <- &ev
		}
	}

	// cleanup
	watchAPI.Stop()
}

// watchResourceMulti
func watchResourceMulti(r *subscriptionResolver, ctx context.Context, gvr schema.GroupVersionResource, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	client := r.K8SDynamicClient(ctx).Resource(gvr)

	// init namespaces
	namespaces, err := r.ToNamespaces(namespace)
	if err != nil {
		return nil, err
	}

	// init list options
	opts := toListOptions(options)

	// decode resource version
	// TODO: fix me
	resourceVersionMap := map[string]string{}
	if len(r.allowedNamespaces) > 0 {
		resourceVersionMap, err = decodeResourceVersionMulti(opts.ResourceVersion)
		if err != nil {
			return nil, err
		}
	}

	// init watch api's
	watchAPIs := []watch.Interface{}
	for _, ns := range namespaces {
		// init options
		thisOpts := opts

		thisResourceVersion, exists := resourceVersionMap[ns]
		if exists {
			thisOpts.ResourceVersion = thisResourceVersion
		} else {
			thisOpts.ResourceVersion = ""
		}

		// TODO: replace me
		if len(r.allowedNamespaces) == 0 {
			thisOpts.ResourceVersion = opts.ResourceVersion
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

// getHealth
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

// watchHealthChannel
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

// conversion helpers
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
	case *unstructured.Unstructured:
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &zeroVal)
		return zeroVal, err
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

// encode cursor to base64-encoded json
func encodeTailCursor(cursor TailCursor) (string, error) {
	jsonData, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	output := base64.StdEncoding.EncodeToString(jsonData)
	return output, nil
}

// decode cursor from base64-encoded json
func decodeTailCursor(input string) (*TailCursor, error) {
	decodedData, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, err
	}
	cursor := &TailCursor{}
	if err = json.Unmarshal(decodedData, cursor); err != nil {
		panic(err)
	}
	return cursor, nil
}

// get first timestamp in log
func getFirstTimestamp(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, container *string) (time.Time, error) {
	var ts time.Time

	// build args
	opts := &corev1.PodLogOptions{
		Timestamps: true,
		LimitBytes: ptr.To[int64](100), // get more bytes than necessary
	}

	if container != nil {
		opts.Container = *container
	}

	// execute query
	req := clientset.CoreV1().Pods(namespace).GetLogs(name, opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return ts, err
	}
	defer podLogs.Close()

	buf := make([]byte, 40) // timestamp is 30-35 bytes long
	if _, err := podLogs.Read(buf); err != nil {
		return ts, err
	}

	return time.Parse(time.RFC3339Nano, strings.Fields(string(buf))[0])
}

// log methods
func headPodLog(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, container *string, args HeadArgs) (*model.PodLogQueryResponse, error) {
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
	records := []model.LogRecord{}
	n := uint(0)

	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		logRecord := newLogRecordFromLogLine(scanner.Text())

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
	response.PageInfo = model.PageInfo{}

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

func tailPodLog(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, container *string, args TailArgs) (*model.PodLogQueryResponse, error) {
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
			return &model.PodLogQueryResponse{PageInfo: model.PageInfo{EndCursor: ptr.To[string]("BEGINNING")}}, nil
		case err != nil:
			// other error
			return nil, err
		default:
			firstTS = ts
		}
	}

	// look back with increasing batch size until we have enough records or reach beginning
	records := []model.LogRecord{}
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

		loopRecords := []model.LogRecord{}

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			logRecord := newLogRecordFromLogLine(scanner.Text())

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
	response.PageInfo = model.PageInfo{}

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

func followPodLog(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, container *string, args FollowArgs) (<-chan model.LogRecord, error) {
	// init output channel
	ch := make(chan model.LogRecord)

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
			logRecord := newLogRecordFromLogLine(scanner.Text())

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

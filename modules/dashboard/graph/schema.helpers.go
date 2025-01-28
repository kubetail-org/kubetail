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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	zlog "github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	gqlerrors "github.com/kubetail-org/kubetail/modules/shared/graphql/errors"

	"github.com/kubetail-org/kubetail/modules/dashboard/graph/model"
	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
)

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
	case *corev1.Service, *corev1.ServiceList:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("not implemented: %T", obj)
	}
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
					transport.AddSubscriptionError(ctx, newWatchErrorFromMetaV1Status(status))
				} else {
					transport.AddSubscriptionError(ctx, gqlerrors.ErrInternalServerError)
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
						transport.AddSubscriptionError(ctx, newWatchErrorFromMetaV1Status(status))
					} else {
						transport.AddSubscriptionError(ctx, gqlerrors.ErrInternalServerError)
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

// Create new log record
func newLogRecordFromLogLine(logLine string) (*model.LogRecord, error) {
	// handle logs from kubernetes fake clientset
	if logLine == "fake logs" {
		return &model.LogRecord{
			Timestamp: time.Now().UTC(),
			Message:   "fake logs",
		}, nil
	}

	parts := strings.SplitN(logLine, " ", 2)
	if len(parts) != 2 {
		panic("log line timestamp not found")
	}

	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return &model.LogRecord{}, err
	}

	return &model.LogRecord{
		Timestamp: ts,
		Message:   parts[1],
	}, nil
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
		zlog.Fatal().Err(err).Send()
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

func healthCheckStatusFromClusterAPIHealthStatus(statusIn clusterapi.HealthStatus) model.HealthCheckStatus {
	switch statusIn {
	case clusterapi.HealthStatusSuccess:
		return model.HealthCheckStatusSuccess
	case clusterapi.HealthStatusFailure:
		return model.HealthCheckStatusFailure
	case clusterapi.HealthStatusPending:
		return model.HealthCheckStatusPending
	case clusterapi.HealthStatusNotFound:
		return model.HealthCheckStatusNotfound
	case clusterapi.HealthStatusUknown:
		return model.HealthCheckStatusUnknown
	default:
		panic("not implemented")
	}
}

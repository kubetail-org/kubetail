package graph

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.
// Code generated by github.com/99designs/gqlgen version v0.17.46

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	zlog "github.com/rs/zerolog/log"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/kubetail-org/kubetail/backend/server/graph/model"
	"github.com/kubetail-org/kubetail/backend/server/internal/grpchelpers"
)

// Object is the resolver for the object field.
func (r *appsV1DaemonSetsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*appsv1.DaemonSet, error) {
	return typeassertRuntimeObject[*appsv1.DaemonSet](obj.Object)
}

// Object is the resolver for the object field.
func (r *appsV1DeploymentsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*appsv1.Deployment, error) {
	return typeassertRuntimeObject[*appsv1.Deployment](obj.Object)
}

// Object is the resolver for the object field.
func (r *appsV1ReplicaSetsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*appsv1.ReplicaSet, error) {
	return typeassertRuntimeObject[*appsv1.ReplicaSet](obj.Object)
}

// Object is the resolver for the object field.
func (r *appsV1StatefulSetsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*appsv1.StatefulSet, error) {
	return typeassertRuntimeObject[*appsv1.StatefulSet](obj.Object)
}

// Object is the resolver for the object field.
func (r *batchV1CronJobsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*batchv1.CronJob, error) {
	return typeassertRuntimeObject[*batchv1.CronJob](obj.Object)
}

// Object is the resolver for the object field.
func (r *batchV1JobsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*batchv1.Job, error) {
	return typeassertRuntimeObject[*batchv1.Job](obj.Object)
}

// Object is the resolver for the object field.
func (r *coreV1NamespacesWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*corev1.Namespace, error) {
	return typeassertRuntimeObject[*corev1.Namespace](obj.Object)
}

// Object is the resolver for the object field.
func (r *coreV1NodesWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*corev1.Node, error) {
	return typeassertRuntimeObject[*corev1.Node](obj.Object)
}

// Object is the resolver for the object field.
func (r *coreV1PodsWatchEventResolver) Object(ctx context.Context, obj *watch.Event) (*corev1.Pod, error) {
	return typeassertRuntimeObject[*corev1.Pod](obj.Object)
}

// AppsV1DaemonSetsGet is the resolver for the appsV1DaemonSetsGet field.
func (r *queryResolver) AppsV1DaemonSetsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*appsv1.DaemonSet, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).AppsV1().DaemonSets(ns).Get(ctx, name, toGetOptions(options))
}

// AppsV1DaemonSetsList is the resolver for the appsV1DaemonSetsList field.
func (r *queryResolver) AppsV1DaemonSetsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*appsv1.DaemonSetList, error) {
	daemonSetList := &appsv1.DaemonSetList{}
	if err := listResource(r, ctx, namespace, options, daemonSetList); err != nil {
		return nil, err
	}
	return daemonSetList, nil
}

// AppsV1DeploymentsGet is the resolver for the appsV1DeploymentsGet field.
func (r *queryResolver) AppsV1DeploymentsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*appsv1.Deployment, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).AppsV1().Deployments(ns).Get(ctx, name, toGetOptions(options))
}

// AppsV1DeploymentsList is the resolver for the appsV1DeploymentsList field.
func (r *queryResolver) AppsV1DeploymentsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*appsv1.DeploymentList, error) {
	deploymentList := &appsv1.DeploymentList{}
	if err := listResource(r, ctx, namespace, options, deploymentList); err != nil {
		return nil, err
	}
	return deploymentList, nil
}

// AppsV1ReplicaSetsGet is the resolver for the appsV1ReplicaSetsGet field.
func (r *queryResolver) AppsV1ReplicaSetsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*appsv1.ReplicaSet, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).AppsV1().ReplicaSets(ns).Get(ctx, name, toGetOptions(options))
}

// AppsV1ReplicaSetsList is the resolver for the appsV1ReplicaSetsList field.
func (r *queryResolver) AppsV1ReplicaSetsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*appsv1.ReplicaSetList, error) {
	replicaSetList := &appsv1.ReplicaSetList{}
	if err := listResource(r, ctx, namespace, options, replicaSetList); err != nil {
		return nil, err
	}
	return replicaSetList, nil
}

// AppsV1StatefulSetsGet is the resolver for the appsV1StatefulSetsGet field.
func (r *queryResolver) AppsV1StatefulSetsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*appsv1.StatefulSet, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).AppsV1().StatefulSets(ns).Get(ctx, name, toGetOptions(options))
}

// AppsV1StatefulSetsList is the resolver for the appsV1StatefulSetsList field.
func (r *queryResolver) AppsV1StatefulSetsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*appsv1.StatefulSetList, error) {
	statefulSetList := &appsv1.StatefulSetList{}
	if err := listResource(r, ctx, namespace, options, statefulSetList); err != nil {
		return nil, err
	}
	return statefulSetList, nil
}

// BatchV1CronJobsGet is the resolver for the batchV1CronJobsGet field.
func (r *queryResolver) BatchV1CronJobsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*batchv1.CronJob, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).BatchV1().CronJobs(ns).Get(ctx, name, toGetOptions(options))
}

// BatchV1CronJobsList is the resolver for the batchV1CronJobsList field.
func (r *queryResolver) BatchV1CronJobsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*batchv1.CronJobList, error) {
	cronJobList := &batchv1.CronJobList{}
	if err := listResource(r, ctx, namespace, options, cronJobList); err != nil {
		return nil, err
	}
	return cronJobList, nil
}

// BatchV1JobsGet is the resolver for the batchV1JobsGet field.
func (r *queryResolver) BatchV1JobsGet(ctx context.Context, name string, namespace *string, options *metav1.GetOptions) (*batchv1.Job, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).BatchV1().Jobs(ns).Get(ctx, name, toGetOptions(options))
}

// BatchV1JobsList is the resolver for the batchV1JobsList field.
func (r *queryResolver) BatchV1JobsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*batchv1.JobList, error) {
	jobList := &batchv1.JobList{}
	if err := listResource(r, ctx, namespace, options, jobList); err != nil {
		return nil, err
	}
	return jobList, nil
}

// CoreV1NamespacesList is the resolver for the coreV1NamespacesList field.
func (r *queryResolver) CoreV1NamespacesList(ctx context.Context, options *metav1.ListOptions) (*corev1.NamespaceList, error) {
	response, err := r.K8SClientset(ctx).CoreV1().Namespaces().List(ctx, toListOptions(options))
	if err != nil {
		return response, nil
	}

	// apply app namespace filter
	if len(r.allowedNamespaces) > 0 {
		items := []corev1.Namespace{}
		for _, item := range response.Items {
			if slices.Contains(r.allowedNamespaces, item.Name) {
				items = append(items, item)
			}
		}
		response.Items = items
	}

	return response, err
}

// CoreV1NodesList is the resolver for the coreV1NodesList field.
func (r *queryResolver) CoreV1NodesList(ctx context.Context, options *metav1.ListOptions) (*corev1.NodeList, error) {
	return r.K8SClientset(ctx).CoreV1().Nodes().List(ctx, toListOptions(options))
}

// CoreV1PodsGet is the resolver for the coreV1PodsGet field.
func (r *queryResolver) CoreV1PodsGet(ctx context.Context, namespace *string, name string, options *metav1.GetOptions) (*corev1.Pod, error) {
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}
	return r.K8SClientset(ctx).CoreV1().Pods(ns).Get(ctx, name, toGetOptions(options))
}

// CoreV1PodsList is the resolver for the coreV1PodsList field.
func (r *queryResolver) CoreV1PodsList(ctx context.Context, namespace *string, options *metav1.ListOptions) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	if err := listResource(r, ctx, namespace, options, podList); err != nil {
		return nil, err
	}
	return podList, nil
}

// CoreV1PodsGetLogs is the resolver for the coreV1PodsGetLogs field.
func (r *queryResolver) CoreV1PodsGetLogs(ctx context.Context, namespace *string, name string, options *corev1.PodLogOptions) ([]model.LogRecord, error) {
	// init namespace
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}

	// init options
	opts := toPodLogOptions(options)
	opts.Follow = false
	opts.Timestamps = true

	// execute query
	req := r.K8SClientset(ctx).CoreV1().Pods(ns).GetLogs(name, &opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return nil, err
	}

	logLines := strings.Split(strings.Trim(buf.String(), "\n"), "\n")
	out := []model.LogRecord{}
	for _, line := range logLines {
		if len(line) != 0 {
			out = append(out, newLogRecordFromLogLine(line))
		}
	}

	return out, nil
}

// LogMetadataList is the resolver for the logMetadataList field.
func (r *queryResolver) LogMetadataList(ctx context.Context, namespace *string) (*agentpb.LogMetadataList, error) {
	// init namespaces
	namespaces, err := r.ToNamespaces(namespace)
	if err != nil {
		return nil, err
	}

	// init response
	outList := &agentpb.LogMetadataList{}

	// init request
	req := &agentpb.LogMetadataListRequest{Namespaces: namespaces}

	// get gprc connections
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := gqlerror.List{}

	for nodeName, conn := range r.gcm.GetAll() {
		wg.Add(1)
		go func(nodeName string, conn grpchelpers.ClientConnInterface) {
			defer wg.Done()

			// init client
			c := agentpb.NewLogMetadataServiceClient(conn)

			// execute
			resp, err := c.List(ctx, req)

			// aquire lock
			mu.Lock()
			defer mu.Unlock()

			// update vars
			if err != nil {
				errs = append(errs, NewGrpcError(conn, err))
			} else {
				// update items
				outList.Items = append(outList.Items, resp.GetItems()...)
			}
		}(nodeName, conn)
	}

	wg.Wait()

	// throw error if response is missing
	if len(errs) != 0 {
		return nil, errs
	}

	return outList, nil
}

// PodLogHead is the resolver for the podLogHead field.
func (r *queryResolver) PodLogHead(ctx context.Context, namespace *string, name string, container *string, after *string, since *string, first *int) (*model.PodLogQueryResponse, error) {
	// init namespace
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}

	// build query args
	args := HeadArgs{}

	if after != nil {
		args.After = *after
	}

	if since != nil {
		args.Since = *since
	}

	if first != nil {
		args.First = uint(*first)
	}

	return headPodLog(ctx, r.K8SClientset(ctx), ns, name, container, args)
}

// PodLogTail is the resolver for the podLogTail field.
func (r *queryResolver) PodLogTail(ctx context.Context, namespace *string, name string, container *string, before *string, last *int) (*model.PodLogQueryResponse, error) {
	// init namespace
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}

	// build query args
	args := TailArgs{}

	if before != nil {
		args.Before = *before
	}

	if last != nil {
		args.Last = uint(*last)
	}

	return tailPodLog(ctx, r.K8SClientset(ctx), ns, name, container, args)
}

// LivezGet is the resolver for the livezGet field.
func (r *queryResolver) LivezGet(ctx context.Context) (model.HealthCheckResponse, error) {
	return getHealth(ctx, r.K8SClientset(ctx), "livez"), nil
}

// ReadyzGet is the resolver for the readyzGet field.
func (r *queryResolver) ReadyzGet(ctx context.Context) (model.HealthCheckResponse, error) {
	return getHealth(ctx, r.K8SClientset(ctx), "readyz"), nil
}

// AppsV1DaemonSetsWatch is the resolver for the appsV1DaemonSetsWatch field.
func (r *subscriptionResolver) AppsV1DaemonSetsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// AppsV1DeploymentsWatch is the resolver for the appsV1DeploymentsWatch field.
func (r *subscriptionResolver) AppsV1DeploymentsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// AppsV1ReplicaSetsWatch is the resolver for the appsV1ReplicaSetsWatch field.
func (r *subscriptionResolver) AppsV1ReplicaSetsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// AppsV1StatefulSetsWatch is the resolver for the appsV1StatefulSetsWatch field.
func (r *subscriptionResolver) AppsV1StatefulSetsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// BatchV1CronJobsWatch is the resolver for the batchV1CronJobsWatch field.
func (r *subscriptionResolver) BatchV1CronJobsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// BatchV1JobsWatch is the resolver for the batchV1JobsWatch field.
func (r *subscriptionResolver) BatchV1JobsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// CoreV1NamespacesWatch is the resolver for the coreV1NamespacesWatch field.
func (r *subscriptionResolver) CoreV1NamespacesWatch(ctx context.Context, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	watchAPI, err := r.K8SClientset(ctx).CoreV1().Namespaces().Watch(ctx, toListOptions(options))
	if err != nil {
		return nil, err
	}

	outCh := make(chan *watch.Event)
	go func() {
		for ev := range watchEventProxyChannel(ctx, watchAPI) {
			ns, err := typeassertRuntimeObject[*corev1.Namespace](ev.Object)
			if err != nil {
				transport.AddSubscriptionError(ctx, ErrInternalServerError)
				break
			}

			// filter out non-authorized namespaces
			if len(r.allowedNamespaces) == 0 || (len(r.allowedNamespaces) > 0 && slices.Contains(r.allowedNamespaces, ns.Name)) {
				outCh <- ev
			}
		}
		close(outCh)
	}()

	return outCh, nil
}

// CoreV1NodesWatch is the resolver for the coreV1NodesWatch field.
func (r *subscriptionResolver) CoreV1NodesWatch(ctx context.Context, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	watchAPI, err := r.K8SClientset(ctx).CoreV1().Nodes().Watch(ctx, toListOptions(options))
	if err != nil {
		return nil, err
	}
	return watchEventProxyChannel(ctx, watchAPI), nil
}

// CoreV1PodsWatch is the resolver for the coreV1PodsWatch field.
func (r *subscriptionResolver) CoreV1PodsWatch(ctx context.Context, namespace *string, options *metav1.ListOptions) (<-chan *watch.Event, error) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	return watchResourceMulti(r, ctx, gvr, namespace, options)
}

// CoreV1PodLogTail is the resolver for the coreV1PodLogTail field.
func (r *subscriptionResolver) CoreV1PodLogTail(ctx context.Context, namespace *string, name string, options *corev1.PodLogOptions) (<-chan *model.LogRecord, error) {
	// init namespace
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}

	// init options
	opts := toPodLogOptions(options)
	opts.Follow = true
	opts.Timestamps = true

	// execute query
	req := r.K8SClientset(ctx).CoreV1().Pods(ns).GetLogs(name, &opts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}

	outCh := make(chan *model.LogRecord)

	go func() {
		defer podLogs.Close()

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			logRecord := newLogRecordFromLogLine(scanner.Text())
			outCh <- &logRecord
		}
		close(outCh)
	}()

	return outCh, nil
}

// LogMetadataWatch is the resolver for the logMetadataWatch field.
func (r *subscriptionResolver) LogMetadataWatch(ctx context.Context, namespace *string) (<-chan *agentpb.LogMetadataWatchEvent, error) {
	// init namespaces
	namespaces, err := r.ToNamespaces(namespace)
	if err != nil {
		return nil, err
	}

	outCh := make(chan *agentpb.LogMetadataWatchEvent)

	sub, err := r.grpcDispatcher.FanoutSubscribe(ctx, func(ctx context.Context, conn *grpc.ClientConn) error {
		// init client
		c := agentpb.NewLogMetadataServiceClient(conn)

		// init request
		req := &agentpb.LogMetadataWatchRequest{Namespaces: namespaces}

		// execute
		stream, err := c.Watch(ctx, req)
		if err != nil {
			return err
		}

		for {
			ev, err := stream.Recv()

			// handle errors
			if err != nil {
				// ignore normal errors
				if err == io.EOF {
					zlog.Debug().Caller().Msg("connection closed by server")
					break
				} else if err == context.Canceled {
					zlog.Debug().Caller().Msg("connection closed by client")
					break
				}

				// check for grpc status error
				if s, ok := status.FromError(err); ok {
					switch s.Code() {
					case codes.Unavailable:
						// server down (probably restarting)
						zlog.Debug().Caller().Msg("server unavailable")
					case codes.Canceled:
						// connection closed client-side
						zlog.Debug().Caller().Msg("connection closed by clientconn")
					default:
						zlog.Error().Caller().Err(err).Msgf("Unexpected gRPC error: %v\n", s.Message())
					}
					break
				}

				zlog.Error().Caller().Err(err).Msg("unexpected error")

				break
			}

			// forward event
			outCh <- ev
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// unsubscribe when client disconnects
	go func() {
		<-ctx.Done()
		zlog.Debug().Msg("client disconnected")
		sub.Unsubscribe()
	}()

	/*
		// doit
		unsubscribe, err := r.gcm.Doit(func(conn grpchelpers.ClientConnInterface) {
			// init client
			c := agentpb.NewLogMetadataServiceClient(conn)

			// init request
			req := &agentpb.LogMetadataWatchRequest{Namespaces: namespaces}

			// execute
			stream, err := c.Watch(ctx, req)
			if err != nil {
				return
			}

			for {
				ev, err := stream.Recv()

				// handle errors
				if err != nil {
					// ignore normal errors
					if err == io.EOF {
						zlog.Debug().Caller().Msg("connection closed by server")
						break
					} else if err == context.Canceled {
						zlog.Debug().Caller().Msg("connection closed by client")
						break
					}

					// check for grpc status error
					if s, ok := status.FromError(err); ok {
						switch s.Code() {
						case codes.Unavailable:
							// server down (probably restarting)
							zlog.Debug().Caller().Msg("server unavailable")
						case codes.Canceled:
							// connection closed client-side
							zlog.Debug().Caller().Msg("connection closed by clientconn")
						default:
							zlog.Error().Caller().Err(err).Msgf("Unexpected gRPC error: %v\n", s.Message())
						}
						break
					}

					zlog.Error().Caller().Err(err).Msg("unexpected error")

					break
				}

				// forward event
				outCh <- ev
			}
		})
		if err != nil {
			return nil, err
		}

		// unsubscribe when client disconnects
		go func() {
			<-ctx.Done()
			zlog.Debug().Msg("client disconnected")
			unsubscribe()
		}()
	*/
	return outCh, nil
}

// PodLogFollow is the resolver for the podLogFollow field.
func (r *subscriptionResolver) PodLogFollow(ctx context.Context, namespace *string, name string, container *string, after *string, since *string) (<-chan *model.LogRecord, error) {
	// init namespace
	ns, err := r.ToNamespace(namespace)
	if err != nil {
		return nil, err
	}

	// build follow args
	args := FollowArgs{}

	if after != nil {
		args.After = *after
	}

	if since != nil {
		args.Since = *since
	}

	// init follow
	inCh, err := followPodLog(ctx, r.K8SClientset(ctx), ns, name, container, args)
	if err != nil {
		return nil, err
	}

	// init output channel
	outCh := make(chan *model.LogRecord)

	// forward data from input to output channel
	go func() {
	Loop:
		for record := range inCh {
			select {
			case outCh <- &record:
				// wrote to output channel
			case <-ctx.Done():
				// listener closed connection
				break Loop
			}
		}
		close(outCh)
	}()

	return outCh, nil
}

// LivezWatch is the resolver for the livezWatch field.
func (r *subscriptionResolver) LivezWatch(ctx context.Context) (<-chan model.HealthCheckResponse, error) {
	return watchHealthChannel(ctx, r.K8SClientset(ctx), "livez"), nil
}

// ReadyzWatch is the resolver for the readyzWatch field.
func (r *subscriptionResolver) ReadyzWatch(ctx context.Context) (<-chan model.HealthCheckResponse, error) {
	return watchHealthChannel(ctx, r.K8SClientset(ctx), "readyz"), nil
}

// AppsV1DaemonSetsWatchEvent returns AppsV1DaemonSetsWatchEventResolver implementation.
func (r *Resolver) AppsV1DaemonSetsWatchEvent() AppsV1DaemonSetsWatchEventResolver {
	return &appsV1DaemonSetsWatchEventResolver{r}
}

// AppsV1DeploymentsWatchEvent returns AppsV1DeploymentsWatchEventResolver implementation.
func (r *Resolver) AppsV1DeploymentsWatchEvent() AppsV1DeploymentsWatchEventResolver {
	return &appsV1DeploymentsWatchEventResolver{r}
}

// AppsV1ReplicaSetsWatchEvent returns AppsV1ReplicaSetsWatchEventResolver implementation.
func (r *Resolver) AppsV1ReplicaSetsWatchEvent() AppsV1ReplicaSetsWatchEventResolver {
	return &appsV1ReplicaSetsWatchEventResolver{r}
}

// AppsV1StatefulSetsWatchEvent returns AppsV1StatefulSetsWatchEventResolver implementation.
func (r *Resolver) AppsV1StatefulSetsWatchEvent() AppsV1StatefulSetsWatchEventResolver {
	return &appsV1StatefulSetsWatchEventResolver{r}
}

// BatchV1CronJobsWatchEvent returns BatchV1CronJobsWatchEventResolver implementation.
func (r *Resolver) BatchV1CronJobsWatchEvent() BatchV1CronJobsWatchEventResolver {
	return &batchV1CronJobsWatchEventResolver{r}
}

// BatchV1JobsWatchEvent returns BatchV1JobsWatchEventResolver implementation.
func (r *Resolver) BatchV1JobsWatchEvent() BatchV1JobsWatchEventResolver {
	return &batchV1JobsWatchEventResolver{r}
}

// CoreV1NamespacesWatchEvent returns CoreV1NamespacesWatchEventResolver implementation.
func (r *Resolver) CoreV1NamespacesWatchEvent() CoreV1NamespacesWatchEventResolver {
	return &coreV1NamespacesWatchEventResolver{r}
}

// CoreV1NodesWatchEvent returns CoreV1NodesWatchEventResolver implementation.
func (r *Resolver) CoreV1NodesWatchEvent() CoreV1NodesWatchEventResolver {
	return &coreV1NodesWatchEventResolver{r}
}

// CoreV1PodsWatchEvent returns CoreV1PodsWatchEventResolver implementation.
func (r *Resolver) CoreV1PodsWatchEvent() CoreV1PodsWatchEventResolver {
	return &coreV1PodsWatchEventResolver{r}
}

// Query returns QueryResolver implementation.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

// Subscription returns SubscriptionResolver implementation.
func (r *Resolver) Subscription() SubscriptionResolver { return &subscriptionResolver{r} }

type appsV1DaemonSetsWatchEventResolver struct{ *Resolver }
type appsV1DeploymentsWatchEventResolver struct{ *Resolver }
type appsV1ReplicaSetsWatchEventResolver struct{ *Resolver }
type appsV1StatefulSetsWatchEventResolver struct{ *Resolver }
type batchV1CronJobsWatchEventResolver struct{ *Resolver }
type batchV1JobsWatchEventResolver struct{ *Resolver }
type coreV1NamespacesWatchEventResolver struct{ *Resolver }
type coreV1NodesWatchEventResolver struct{ *Resolver }
type coreV1PodsWatchEventResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }

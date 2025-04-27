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
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	evbus "github.com/asaskevich/EventBus"
	set "github.com/deckarep/golang-set/v2"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// Event enum
type SourceWatcherEvent string

const (
	SourceWatcherEventAdded    SourceWatcherEvent = "ADDED"
	SourceWatcherEventModified SourceWatcherEvent = "MODIFIED"
	SourceWatcherEventDeleted  SourceWatcherEvent = "DELETED"
)

// Convenience struct for organizing unique (namespace, workload-type)'s
type fetchTuple struct {
	namespace    string
	workloadType WorkloadType
}

// Represents log source
type LogSource struct {
	Metadata      LogSourceMetadata
	Namespace     string
	PodName       string
	ContainerName string
	ContainerID   string
}

type LogSourceMetadata struct {
	Region string
	Zone   string
	OS     string
	Arch   string
	Node   string
}

// SourceWatcher interface
type SourceWatcher interface {
	Start(ctx context.Context) error
	Set() set.Set[LogSource]
	Subscribe(event SourceWatcherEvent, fn any)
	Unsubscribe(event SourceWatcherEvent, fn any)
	Close()
}

// Represents SourceWatcher
type sourceWatcher struct {
	cm       k8shelpers.ConnectionManager
	eventbus evbus.Bus

	kubeContext string
	bearerToken string
	regions     []string
	zones       []string
	oses        []string
	arches      []string
	nodes       []string
	containers  []string

	allowedNamespaces []string
	parsedPaths       []parsedPath
	sources           set.Set[LogSource]
	index             *workloadIndex
	nodeMap           map[string]*corev1.Node

	mu        sync.Mutex
	isReady   bool
	stopCh    chan struct{}
	closeOnce sync.Once
}

// Initialize new source watcher
func NewSourceWatcher(cm k8shelpers.ConnectionManager, sourcePaths []string, opts ...Option) (SourceWatcher, error) {
	// Init source watcher instance
	sw := &sourceWatcher{
		cm:       cm,
		sources:  set.NewSet[LogSource](),
		index:    newWorkloadIndex(),
		nodeMap:  make(map[string]*corev1.Node),
		eventbus: evbus.New(),
		stopCh:   make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(sw); err != nil {
			return nil, err
		}
	}

	// Get default namespace
	defaultNamespace := cm.GetDefaultNamespace(sw.kubeContext)

	// Parse paths
	parsedPaths := []parsedPath{}
	for _, p := range sourcePaths {
		pp, err := parsePath(p, defaultNamespace)
		if err != nil {
			return nil, err
		}

		// Remove paths that do not match allowed namespaces
		if len(sw.allowedNamespaces) > 0 && !slices.Contains(sw.allowedNamespaces, pp.Namespace) {
			continue
		}

		parsedPaths = append(parsedPaths, pp)
	}
	sw.parsedPaths = parsedPaths

	return sw, nil
}

// Current sources as a set
func (w *sourceWatcher) Set() set.Set[LogSource] {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sources.Clone()
}

// Subscribe to events
func (w *sourceWatcher) Subscribe(event SourceWatcherEvent, fn any) {
	w.eventbus.SubscribeAsync(string(event), fn, true)
}

// Unsubscribe from events
func (w *sourceWatcher) Unsubscribe(event SourceWatcherEvent, fn any) {
	w.eventbus.Unsubscribe(string(event), fn)
}

// Close background processes
func (w *sourceWatcher) Close() {
	w.closeOnce.Do(func() {
		close(w.stopCh)
	})
}

// Start background processes
func (w *sourceWatcher) Start(ctx context.Context) error {
	set := set.NewSet[fetchTuple]()

	for _, pp := range w.parsedPaths {
		set.Add(fetchTuple{pp.Namespace, pp.WorkloadType})

		// Fetch related data
		switch pp.WorkloadType {
		case WorkloadTypeDeployment:
			set.Add(fetchTuple{pp.Namespace, WorkloadTypeReplicaSet})
		case WorkloadTypeCronJob:
			set.Add(fetchTuple{pp.Namespace, WorkloadTypeJob})
		}

		// Always get pods
		set.Add(fetchTuple{pp.Namespace, WorkloadTypePod})
	}

	// Initialize informers in background
	var wg sync.WaitGroup
	errs := ThreadSafeSlice[error]{}

	set.Each(func(ft fetchTuple) bool {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Init informer
			informer, start, err := w.cm.NewInformer(ctx, w.kubeContext, w.bearerToken, ft.namespace, ft.workloadType.GVR())
			if err != nil {
				errs.Add(err)
				return
			}

			// Add event handlers
			handle, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc:    w.handleWorkloadAdd,
				UpdateFunc: w.handleWorkloadUpdate,
				DeleteFunc: w.handleWorkloadDelete,
			})
			if err != nil {
				errs.Add(err)
				return
			}

			// Start informer
			start()

			// Wait for cache to sync
			if !cache.WaitForCacheSync(w.stopCh, handle.HasSynced) {
				errs.Add(fmt.Errorf("cache did not sync"))
				return
			}
		}()

		return false // continue
	})

	// Get nodes
	wg.Add(1)
	go func() {
		defer wg.Done()

		gvr := schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "nodes",
		}

		// Init informer
		informer, start, err := w.cm.NewInformer(ctx, w.kubeContext, w.bearerToken, "", gvr)
		if err != nil {
			errs.Add(err)
			return
		}

		// Add event handlers
		handle, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    w.handleNodeAdd,
			UpdateFunc: w.handleNodeUpdate,
			DeleteFunc: w.handleNodeDelete,
		})
		if err != nil {
			errs.Add(err)
			return
		}

		// Start informer
		start()

		// Wait for cache to sync
		if !cache.WaitForCacheSync(w.stopCh, handle.HasSynced) {
			errs.Add(fmt.Errorf("cache did not sync"))
			return
		}
	}()

	wg.Wait()

	// Check errors
	if errs.Len() > 0 {
		return fmt.Errorf("encountered errors: %v", errs.ToSlice())
	}

	// Acquire lock
	w.mu.Lock()
	defer w.mu.Unlock()

	// Update sources
	w.updateSources_UNSAFE()

	// Update ready flag
	w.isReady = true

	return nil
}

// Handle workload resource addition
func (w *sourceWatcher) handleWorkloadAdd(obj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.index.Add(obj)

	if w.isReady {
		w.updateSources_UNSAFE()
	}
}

// Handle workload resource update
func (w *sourceWatcher) handleWorkloadUpdate(oldObj any, newObj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Only update pods
	switch newObj.(type) {
	case *corev1.Pod:
		w.index.Update(newObj)

		if w.isReady {
			w.updateSources_UNSAFE()
		}
	default:
		// do nothing
	}
}

// Handle workload resource deletion
func (w *sourceWatcher) handleWorkloadDelete(obj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.index.Remove(obj)

	if w.isReady {
		w.updateSources_UNSAFE()
	}
}

// Handle node addition
func (w *sourceWatcher) handleNodeAdd(obj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	node, ok := obj.(*corev1.Node)
	if !ok {
		return
	}

	// Add to or update map
	w.nodeMap[node.Name] = node

	if w.isReady {
		w.updateSources_UNSAFE()
	}
}

// Handle node update
func (w *sourceWatcher) handleNodeUpdate(oldObj any, newObj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	node, ok := newObj.(*corev1.Node)
	if !ok {
		return
	}

	// Update map
	w.nodeMap[node.Name] = node

	if w.isReady {
		w.updateSources_UNSAFE()
	}
}

// Handle node resource deletion
func (w *sourceWatcher) handleNodeDelete(obj any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	node, ok := obj.(*corev1.Node)
	if !ok {
		return
	}

	// Remove from map
	delete(w.nodeMap, node.Name)

	if w.isReady {
		w.updateSources_UNSAFE()
	}
}

// Update sources and publish events
func (w *sourceWatcher) updateSources_UNSAFE() {
	wantSources := set.NewSet[LogSource]()

	for _, pp := range w.parsedPaths {
		for _, workload := range w.index.GetWorkloads(pp.Namespace, pp.WorkloadType, pp.WorkloadName) {
			for _, pod := range w.index.GetPodsOwnedByWorkload(workload.GetUID()) {
				wantName := pp.ContainerName
				for n, status := range pod.Status.ContainerStatuses {
					// Wait until we have an ID
					if status.ContainerID == "" {
						continue
					}

					// Filter by container
					k := fmt.Sprintf("%s:%s/%s", pod.Namespace, pod.Name, status.Name)
					if len(w.containers) > 0 && !slices.Contains(w.containers, k) {
						continue
					}

					// Ensure node is available
					node, exists := w.nodeMap[pod.Spec.NodeName]
					if !exists {
						continue
					}

					// Filter by node
					if len(w.nodes) > 0 && !slices.Contains(w.nodes, node.Name) {
						continue
					}

					// Filter by region
					if len(w.regions) > 0 && !slices.Contains(w.regions, node.Labels["topology.kubernetes.io/region"]) {
						continue
					}

					// Filter by zone
					if len(w.zones) > 0 && !slices.Contains(w.zones, node.Labels["topology.kubernetes.io/zone"]) {
						continue
					}

					// Filter by os
					if len(w.oses) > 0 && !slices.Contains(w.oses, node.Status.NodeInfo.OperatingSystem) {
						continue
					}

					// Filter by arch
					if len(w.arches) > 0 && !slices.Contains(w.arches, node.Status.NodeInfo.Architecture) {
						continue
					}

					if wantName == "*" || wantName == status.Name || (wantName == "" && n == 0) {
						wantSources.Add(LogSource{
							Metadata: LogSourceMetadata{
								Region: node.Labels["topology.kubernetes.io/region"],
								Zone:   node.Labels["topology.kubernetes.io/zone"],
								OS:     node.Status.NodeInfo.OperatingSystem,
								Arch:   node.Status.NodeInfo.Architecture,
								Node:   pod.Spec.NodeName,
							},
							Namespace:     pod.Namespace,
							PodName:       pod.Name,
							ContainerName: status.Name,
							ContainerID:   status.ContainerID,
						})
					}
				}
			}
		}
	}

	// Publish ADDED events
	wantSources.Difference(w.sources).Each(func(source LogSource) bool {
		w.eventbus.Publish("ADDED", source)
		return false // continue
	})

	// Publish DELETED events
	w.sources.Difference(wantSources).Each(func(source LogSource) bool {
		w.eventbus.Publish("DELETED", source)
		return false // continue
	})

	w.sources = wantSources
}

// Represents result of parsePath()
type parsedPath struct {
	Namespace     string
	WorkloadType  WorkloadType
	WorkloadName  string
	ContainerName string
}

// Parse source path
func parsePath(path string, defaultNamespace string) (parsedPath, error) {
	// Remove leading and trailing slashes
	trimmedPath := strings.Trim(path, "/")

	// First split on colon to extract namespace if present
	namespaceParts := strings.SplitN(trimmedPath, ":", 2)

	if defaultNamespace == "" {
		defaultNamespace = "default"
	}

	out := parsedPath{
		Namespace: defaultNamespace,
	}

	var pathToParse string
	if len(namespaceParts) == 2 {
		// If we found a colon, the first part is the namespace
		out.Namespace = namespaceParts[0]
		pathToParse = namespaceParts[1]
	} else {
		// No namespace specified, use the whole path
		pathToParse = trimmedPath
	}

	// Split remaining path on slashes
	parts := strings.Split(pathToParse, "/")

	// Parse parts
	switch len(parts) {
	case 1:
		// Parse as <pod>
		out.WorkloadType = WorkloadTypePod
		out.WorkloadName = parts[0]
	case 2:
		out.WorkloadType = parseWorkloadType(parts[0])

		if out.WorkloadType == WorkloadTypeUknown {
			// Parse as <pod-name>/<container-name>
			out.WorkloadType = WorkloadTypePod
			out.WorkloadName = parts[0]
			out.ContainerName = parts[1]
		} else {
			// Parse as <workload-type>/<workload-name>
			out.WorkloadName = parts[1]
		}
	case 3:
		// Parse as <workload-type>/<workload-name>/<container-name>
		out.WorkloadType = parseWorkloadType(parts[0])
		out.WorkloadName = parts[1]
		out.ContainerName = parts[2]
	}

	// Ensure we were able to determine the workload type
	if out.WorkloadType == WorkloadTypeUknown {
		return parsedPath{}, fmt.Errorf("unable to parse %s", path)
	}

	return out, nil
}

// Represents generic workload
type workload interface {
	GetUID() types.UID
	GetName() string
}

// Represents workload index
type workloadIndex struct {
	dataMap      map[types.UID]any
	listMap      MapSet[string, types.UID]
	ownershipMap MapSet[types.UID, types.UID]
	mu           sync.RWMutex
}

// Initialize index
func newWorkloadIndex() *workloadIndex {
	return &workloadIndex{
		dataMap:      make(map[types.UID]any),
		listMap:      NewMapSet[string, types.UID](),
		ownershipMap: NewMapSet[types.UID, types.UID](),
	}
}

// Get workloads filtered by `name_filter`
func (wi *workloadIndex) GetWorkloads(namespace string, t WorkloadType, name_filter string) []workload {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	k := wi.generateDataKey(namespace, t)
	objIDs, exists := wi.listMap.Get(k)
	if !exists {
		return nil
	}

	var outList []workload
	for _, objID := range objIDs.ToSlice() {
		obj, exists := wi.dataMap[objID]
		if !exists {
			continue
		}

		workload, ok := obj.(workload)
		if !ok {
			continue
		}

		if name_filter == "*" || workload.GetName() == name_filter {
			outList = append(outList, workload)
		}
	}

	return outList
}

// Get pods owned by a given workload
func (wi *workloadIndex) GetPodsOwnedByWorkload(workloadID types.UID) []*corev1.Pod {
	wi.mu.RLock()
	defer wi.mu.RUnlock()

	pods := []*corev1.Pod{}
	for _, podID := range wi.getLeafIDs_UNSAFE(workloadID) {
		if obj, exists := wi.dataMap[podID]; exists {
			if pod, ok := obj.(*corev1.Pod); ok {
				pods = append(pods, pod)
			}
		}
	}

	return pods
}

// Add workload object to index
func (wi *workloadIndex) Add(obj any) error {
	wi.mu.Lock()
	defer wi.mu.Unlock()

	var k string
	var objID types.UID

	switch v := obj.(type) {
	case *batchv1.CronJob:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeCronJob)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	case *appsv1.DaemonSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeDaemonSet)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	case *appsv1.Deployment:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeDeployment)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	case *batchv1.Job:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeJob)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	case *corev1.Pod:
		k = wi.generateDataKey(v.Namespace, WorkloadTypePod)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	case *appsv1.ReplicaSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeReplicaSet)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}

	case *appsv1.StatefulSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeStatefulSet)
		objID = v.UID

		// Add to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Add(ownerRef.UID, v.UID)
		}
	default:
		return fmt.Errorf("not implemented")
	}

	// Add to list map
	wi.listMap.Add(k, objID)

	// Add to data map
	wi.dataMap[objID] = obj

	return nil
}

// Update workload object in index
func (wi *workloadIndex) Update(obj any) error {
	wi.mu.Lock()
	defer wi.mu.Unlock()

	var objID types.UID

	switch v := obj.(type) {
	case *batchv1.CronJob:
		objID = v.UID
	case *appsv1.DaemonSet:
		objID = v.UID
	case *appsv1.Deployment:
		objID = v.UID
	case *batchv1.Job:
		objID = v.UID
	case *corev1.Pod:
		objID = v.UID
	case *appsv1.ReplicaSet:
		objID = v.UID
	case *appsv1.StatefulSet:
		objID = v.UID
	default:
		return fmt.Errorf("not implemented")
	}

	// Update in data map
	wi.dataMap[objID] = obj

	return nil
}

// Remove workload object from index
func (wi *workloadIndex) Remove(obj any) error {
	wi.mu.Lock()
	defer wi.mu.Unlock()

	// Remove from data map
	var k string
	var objID types.UID

	switch v := obj.(type) {
	case *batchv1.CronJob:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeCronJob)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	case *appsv1.DaemonSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeDaemonSet)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	case *appsv1.Deployment:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeDeployment)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	case *batchv1.Job:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeJob)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	case *corev1.Pod:
		k = wi.generateDataKey(v.Namespace, WorkloadTypePod)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	case *appsv1.ReplicaSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeReplicaSet)
		objID = v.UID

		// Remove from ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}

	case *appsv1.StatefulSet:
		k = wi.generateDataKey(v.Namespace, WorkloadTypeStatefulSet)
		objID = v.UID

		// Remove to ownership map
		for _, ownerRef := range v.OwnerReferences {
			wi.ownershipMap.Remove(ownerRef.UID, v.UID)
		}
	default:
		return fmt.Errorf("not implemented")
	}

	// Remove from list map
	wi.listMap.Remove(k, objID)

	// Delete from data map
	delete(wi.dataMap, objID)

	return nil
}

// Return key for use with data map
func (wi *workloadIndex) generateDataKey(namespace string, t WorkloadType) string {
	return fmt.Sprintf("%s:%s", namespace, t.String())
}

// Get leaf ids from ownership map
func (wi *workloadIndex) getLeafIDs_UNSAFE(nodeID types.UID) []types.UID {
	// If the node has no children, it is a leaf node
	children, exists := wi.ownershipMap.Get(nodeID)
	if !exists {
		return []types.UID{nodeID}
	}

	// Recursively collect leaf nodes from children
	var leaves []types.UID
	children.Each(func(childID types.UID) bool {
		leaves = append(leaves, wi.getLeafIDs_UNSAFE(childID)...)
		return false
	})

	return leaves
}

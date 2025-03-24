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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewSourceWatcher(t *testing.T) {
	obj, err := NewSourceWatcher(nil, []string{}, &sourceWatcherConfig{})
	require.NoError(t, err)

	w, ok := obj.(*sourceWatcher)
	require.True(t, ok)
	assert.NotNil(t, w.parsedPaths)
	assert.NotNil(t, w.sources)
	assert.NotNil(t, w.index)
	assert.NotNil(t, w.fm)
	assert.NotNil(t, w.stopCh)
}

func TestHandleWorkloadAdd(t *testing.T) {
	// Mock data
	mockNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
	}

	mockPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			UID:       "pod1-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "container1",
					ContainerID: "container1-id",
				},
			},
		},
	}

	mockPodWithoutContainer := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			UID:       "pod1-uid",
		},
	}

	mockSource := LogSource{
		NodeName:      "node1",
		Namespace:     "default",
		PodName:       "pod1",
		ContainerName: "container1",
		ContainerID:   "container1-id",
	}

	// Table-driven tests
	tests := []struct {
		name        string
		setIsReady  bool
		addObj      any
		wantSources []LogSource
		wantEvents  []string
	}{
		{
			name:        "add pod when not ready",
			setIsReady:  false,
			addObj:      mockPod,
			wantSources: []LogSource{},
			wantEvents:  nil,
		},
		{
			name:        "add pod when ready",
			setIsReady:  true,
			addObj:      mockPod,
			wantSources: []LogSource{mockSource},
			wantEvents:  []string{"ADDED"},
		},
		{
			name:        "add pod without running container",
			setIsReady:  true,
			addObj:      mockPodWithoutContainer,
			wantSources: []LogSource{},
			wantEvents:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize source watcher
			w, err := NewSourceWatcher(fake.NewSimpleClientset(), []string{"default:pods/pod1"}, &sourceWatcherConfig{})
			assert.NoError(t, err)

			sw := w.(*sourceWatcher)
			sw.isReady = tt.setIsReady

			// Track events
			var events []string
			sw.Subscribe(watchEventAdded, func(s LogSource) {
				events = append(events, "ADDED")
			})

			// Execute add
			sw.handleNodeAdd(mockNode)
			sw.handleWorkloadAdd(tt.addObj)

			// Wait for events
			sw.eventbus.WaitAsync()

			// Verify results
			assert.Equal(t, tt.wantSources, sw.sources.ToSlice())
			assert.Equal(t, tt.wantEvents, events)
		})
	}
}

func TestHandleWorkloadDelete(t *testing.T) {
	// Mock data
	mockNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
	}

	mockPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			UID:       "pod1-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "container1",
					ContainerID: "container1-id",
				},
			},
		},
	}

	mockDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy1",
			Namespace: "default",
			UID:       "deploy1-uid",
		},
	}

	mockSource := LogSource{
		NodeName:      "node1",
		Namespace:     "default",
		PodName:       "pod1",
		ContainerName: "container1",
		ContainerID:   "container1-id",
	}

	// Table-driven tests
	tests := []struct {
		name        string
		setIsReady  bool
		setObjs     []any
		deleteObj   any
		wantSources []LogSource
		wantEvents  []string
	}{
		{
			name:        "delete pod when not ready",
			setIsReady:  false,
			setObjs:     []any{},
			deleteObj:   mockPod,
			wantSources: []LogSource{},
			wantEvents:  nil,
		},
		{
			name:        "delete pod when ready",
			setIsReady:  true,
			setObjs:     []any{mockPod},
			deleteObj:   mockPod,
			wantSources: []LogSource{},
			wantEvents:  []string{"DELETED"},
		},
		{
			name:        "delete deployment",
			setIsReady:  true,
			setObjs:     []any{mockPod, mockDeployment},
			deleteObj:   mockDeployment,
			wantSources: []LogSource{mockSource},
			wantEvents:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize source watcher
			w, err := NewSourceWatcher(fake.NewSimpleClientset(), []string{"default:pods/pod1"}, &sourceWatcherConfig{})
			assert.NoError(t, err)

			sw := w.(*sourceWatcher)
			sw.isReady = tt.setIsReady

			// Add data
			sw.handleNodeAdd(mockNode)
			if len(tt.setObjs) > 0 {
				for _, obj := range tt.setObjs {
					sw.handleWorkloadAdd(obj)
				}
			}

			// Track events
			var events []string
			sw.Subscribe(watchEventDeleted, func(s LogSource) {
				events = append(events, "DELETED")
			})

			// Execute delete
			sw.handleWorkloadDelete(tt.deleteObj)

			// Wait for events
			sw.eventbus.WaitAsync()

			// Verify results
			assert.Equal(t, tt.wantSources, sw.sources.ToSlice())
			assert.Equal(t, tt.wantEvents, events)
		})
	}
}

func TestParsePath(t *testing.T) {
	defaultNamespace := "default"

	tests := []struct {
		name           string
		setPath        string
		wantParsedPath parsedPath
	}{
		{
			"<pod-name>",
			"pod-123",
			parsedPath{
				Namespace:     defaultNamespace,
				WorkloadType:  WorkloadTypePod,
				WorkloadName:  "pod-123",
				ContainerName: "",
			},
		},
		{
			"<pod-name>/<container-name>",
			"pod-123/container-1",
			parsedPath{
				Namespace:     defaultNamespace,
				WorkloadType:  WorkloadTypePod,
				WorkloadName:  "pod-123",
				ContainerName: "container-1",
			},
		},
		{
			"<workload-type>/<workload-name>",
			"deployments/web",
			parsedPath{
				Namespace:     defaultNamespace,
				WorkloadType:  WorkloadTypeDeployment,
				WorkloadName:  "web",
				ContainerName: "",
			},
		},
		{
			"<workload-type>/<workload-name>/<container-name>",
			"deployments/web/container-1",
			parsedPath{
				Namespace:     defaultNamespace,
				WorkloadType:  WorkloadTypeDeployment,
				WorkloadName:  "web",
				ContainerName: "container-1",
			},
		},
		{
			"<namespace>:<pod-name>",
			"frontend:pod-123",
			parsedPath{
				Namespace:     "frontend",
				WorkloadType:  WorkloadTypePod,
				WorkloadName:  "pod-123",
				ContainerName: "",
			},
		},
		{
			"<namespace>:<pod-name>/<container-name>",
			"frontend:pod-123/container-1",
			parsedPath{
				Namespace:     "frontend",
				WorkloadType:  WorkloadTypePod,
				WorkloadName:  "pod-123",
				ContainerName: "container-1",
			},
		},
		{
			"<namespace>:<workload-type>/<workload-name>",
			"frontend:deployments/web",
			parsedPath{
				Namespace:     "frontend",
				WorkloadType:  WorkloadTypeDeployment,
				WorkloadName:  "web",
				ContainerName: "",
			},
		},
		{
			"<namespace>:<workload-type>/<workload-name>/<container-name>",
			"frontend:deployments/web/container-1",
			parsedPath{
				Namespace:     "frontend",
				WorkloadType:  WorkloadTypeDeployment,
				WorkloadName:  "web",
				ContainerName: "container-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parsePath(tt.setPath, defaultNamespace)
			require.Nil(t, err)
			assert.Equal(t, tt.wantParsedPath, parsed)
		})
	}
}

func TestNewWorkloadIndex(t *testing.T) {
	wi := newWorkloadIndex()
	assert.NotNil(t, wi)
	assert.NotNil(t, wi.dataMap)
	assert.NotNil(t, wi.listMap)
	assert.NotNil(t, wi.ownershipMap)
}

func TestWorkloadIndexAdd(t *testing.T) {
	wi := newWorkloadIndex()

	t.Run("Add Pod", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "pod1-uid",
				Name:      "pod1",
				Namespace: "ns1",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "owner1-uid"},
				},
			},
		}

		err := wi.Add(pod)
		assert.NoError(t, err)

		// Check dataMap
		obj, exists := wi.dataMap["pod1-uid"]
		assert.True(t, exists)
		assert.Equal(t, pod, obj)

		// Check listMap
		key := wi.generateDataKey("ns1", WorkloadTypePod)
		assert.True(t, wi.listMap.ContainsOne(key, "pod1-uid"))

		// Check ownershipMap
		children, exists := wi.ownershipMap.Get("owner1-uid")
		assert.True(t, exists)
		assert.True(t, children.Contains("pod1-uid"))
	})

	t.Run("Add unsupported type", func(t *testing.T) {
		err := wi.Add("invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not implemented")
	})
}

func TestWorkloadIndexRemove(t *testing.T) {
	wi := newWorkloadIndex()

	t.Run("Remove pod", func(t *testing.T) {
		pod1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "pod1-uid",
				Name:      "pod1",
				Namespace: "ns1",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "owner1-uid"},
				},
			},
		}

		pod2 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "pod2-uid",
				Name:      "pod2",
				Namespace: "ns1",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "owner1-uid"},
				},
			},
		}

		// Add data
		err := wi.Add(pod1)
		assert.NoError(t, err)

		err = wi.Add(pod2)
		assert.NoError(t, err)

		// Remove
		err = wi.Remove(pod1)
		assert.NoError(t, err)

		// Check dataMap
		_, exists := wi.dataMap["pod1-uid"]
		assert.False(t, exists)

		_, exists = wi.dataMap["pod2-uid"]
		assert.True(t, exists)

		// Check listMap
		key := wi.generateDataKey("ns1", WorkloadTypePod)
		assert.False(t, wi.listMap.ContainsOne(key, "pod1-uid"))
		assert.True(t, wi.listMap.ContainsOne(key, "pod2-uid"))

		// Check ownershipMap
		children, exists := wi.ownershipMap.Get("owner1-uid")
		assert.True(t, exists)
		assert.False(t, children.Contains("pod1-uid"))
		assert.True(t, children.Contains("pod2-uid"))
	})

	t.Run("Remove unsupported type", func(t *testing.T) {
		err := wi.Remove("invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not implemented")
	})
}

func TestWorkloadIndexUpdate(t *testing.T) {
	wi := newWorkloadIndex()

	t.Run("Update pod", func(t *testing.T) {
		// Add data
		oldPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "pod1-uid",
				Name:      "pod1",
				Namespace: "ns1",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "owner1-uid"},
				},
			},
		}

		err := wi.Add(oldPod)
		require.NoError(t, err)

		// Update data
		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:               "pod1-uid",
				Name:              "pod1",
				Namespace:         "ns1",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				OwnerReferences: []metav1.OwnerReference{
					{UID: "owner1-uid"},
				},
			},
		}

		err = wi.Update(newPod)
		require.NoError(t, err)

		// Check dataMap
		obj, exists := wi.dataMap["pod1-uid"]
		assert.True(t, exists)
		assert.Equal(t, newPod, obj)
	})

	t.Run("Update unsupported type", func(t *testing.T) {
		err := wi.Update("invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not implemented")
	})
}

func TestWorkloadIndexGetWorkloads(t *testing.T) {
	wi := newWorkloadIndex()

	// Add data
	err := wi.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "pod1-uid",
			Name:      "pod1",
			Namespace: "ns1",
		},
	})
	require.NoError(t, err)

	err = wi.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "pod2-uid",
			Name:      "pod2",
			Namespace: "ns1",
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		namespace  string
		wType      WorkloadType
		nameFilter string
		wantLen    int
	}{
		{
			"Get all pods",
			"ns1",
			WorkloadTypePod,
			"*",
			2,
		},
		{
			"Get specific pod",
			"ns1",
			WorkloadTypePod,
			"pod1",
			1,
		},
		{
			"No matching pods",
			"ns1",
			WorkloadTypePod,
			"nonexistent",
			0,
		},
		{
			"Wrong namespace",
			"other",
			WorkloadTypePod,
			"*",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workloads := wi.GetWorkloads(tt.namespace, tt.wType, tt.nameFilter)
			assert.Equal(t, tt.wantLen, len(workloads))
		})
	}
}

func TestWorkloadIndexGetPodsOwnedByWorkload(t *testing.T) {
	wi := newWorkloadIndex()

	// Add pods
	err := wi.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "pod1-uid",
			Name:      "pod1",
			Namespace: "ns1",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "rs1-uid"},
			},
		},
	})
	require.NoError(t, err)

	err = wi.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "pod2-uid",
			Name:      "pod2",
			Namespace: "ns1",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "rs1-uid"},
			},
		},
	})
	require.NoError(t, err)

	// Add replicaset
	err = wi.Add(&appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs1",
			UID:       "rs1-uid",
			Namespace: "ns1",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "deploy1-uid"},
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		workloadID types.UID
		wantLen    int
	}{
		{
			"Get pods owned by ReplicaSet",
			"rs1-uid",
			2,
		},
		{
			"Get pods owned by Deployment",
			"deploy1-uid",
			2,
		},
		{
			"No pods owned by workload",
			"nonexistent-uid",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := wi.GetPodsOwnedByWorkload(tt.workloadID)
			assert.Equal(t, tt.wantLen, len(pods))
		})
	}
}

func TestWorkloadIndexGenerateDataKey(t *testing.T) {
	wi := newWorkloadIndex()
	tests := []struct {
		name      string
		namespace string
		wType     WorkloadType
		want      string
	}{
		{
			"Pod in default namespace",
			"default",
			WorkloadTypePod,
			"default:Pod",
		},
		{
			"Deployment in custom namespace",
			"custom-ns",
			WorkloadTypeDeployment,
			"custom-ns:Deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wi.generateDataKey(tt.namespace, tt.wType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateSourcesWithNodeFilter(t *testing.T) {
	// Mock data
	mockNode1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
	}

	mockNode2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node2",
		},
	}

	mockPod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			UID:       "pod1-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "container1",
					ContainerID: "container1-id",
				},
			},
		},
	}

	mockPod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "default",
			UID:       "pod2-uid",
		},
		Spec: corev1.PodSpec{
			NodeName: "node2",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "container1",
					ContainerID: "container2-id",
				},
			},
		},
	}

	mockSource1 := LogSource{
		NodeName:      "node1",
		Namespace:     "default",
		PodName:       "pod1",
		ContainerName: "container1",
		ContainerID:   "container1-id",
	}

	mockSource2 := LogSource{
		NodeName:      "node2",
		Namespace:     "default",
		PodName:       "pod2",
		ContainerName: "container1",
		ContainerID:   "container2-id",
	}

	// Table-driven tests
	tests := []struct {
		name        string
		nodes       []string
		pods        []*corev1.Pod
		wantSources []LogSource
	}{
		{
			name:        "no node filter",
			nodes:       []string{},
			pods:        []*corev1.Pod{mockPod1, mockPod2},
			wantSources: []LogSource{mockSource1, mockSource2},
		},
		{
			name:        "filter to node1",
			nodes:       []string{"node1"},
			pods:        []*corev1.Pod{mockPod1, mockPod2},
			wantSources: []LogSource{mockSource1},
		},
		{
			name:        "filter to node2",
			nodes:       []string{"node2"},
			pods:        []*corev1.Pod{mockPod1, mockPod2},
			wantSources: []LogSource{mockSource2},
		},
		{
			name:        "filter to multiple nodes",
			nodes:       []string{"node1", "node2"},
			pods:        []*corev1.Pod{mockPod1, mockPod2},
			wantSources: []LogSource{mockSource1, mockSource2},
		},
		{
			name:        "filter to non-existent node",
			nodes:       []string{"node3"},
			pods:        []*corev1.Pod{mockPod1, mockPod2},
			wantSources: []LogSource{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize source watcher with the node filter
			w, err := NewSourceWatcher(
				fake.NewSimpleClientset(),
				[]string{"default:pods/*"},
				&sourceWatcherConfig{
					Nodes: tt.nodes,
				},
			)
			require.NoError(t, err)

			sw := w.(*sourceWatcher)
			sw.isReady = true

			// Add nodes
			sw.handleNodeAdd(mockNode1)
			sw.handleNodeAdd(mockNode2)

			// Add pods to the index
			for _, pod := range tt.pods {
				err := sw.index.Add(pod)
				require.NoError(t, err)
			}

			// Call updateSources_UNSAFE
			sw.updateSources_UNSAFE()

			// Verify results
			assert.ElementsMatch(t, tt.wantSources, sw.sources.ToSlice())
		})
	}
}

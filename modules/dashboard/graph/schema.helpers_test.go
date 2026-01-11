// Copyright 2024 The Kubetail Authors
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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/utils/ptr"

	gqlerrors "github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

func TestGetGVRSuccess(t *testing.T) {
	newGVR := func(group, version, resource string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	}

	tests := []struct {
		name    string
		object  runtime.Object
		wantGVR schema.GroupVersionResource
	}{
		{"CronJob", &batchv1.CronJob{}, newGVR("batch", "v1", "cronjobs")},
		{"CronJobList", &batchv1.CronJobList{}, newGVR("batch", "v1", "cronjobs")},
		{"DaemonSet", &appsv1.DaemonSet{}, newGVR("apps", "v1", "daemonsets")},
		{"DaemonSetList", &appsv1.DaemonSetList{}, newGVR("apps", "v1", "daemonsets")},
		{"Deployment", &appsv1.Deployment{}, newGVR("apps", "v1", "deployments")},
		{"DeploymentList", &appsv1.DeploymentList{}, newGVR("apps", "v1", "deployments")},
		{"Job", &batchv1.Job{}, newGVR("batch", "v1", "jobs")},
		{"JobList", &batchv1.JobList{}, newGVR("batch", "v1", "jobs")},
		{"Pod", &corev1.Pod{}, newGVR("", "v1", "pods")},
		{"PodList", &corev1.PodList{}, newGVR("", "v1", "pods")},
		{"ReplicaSet", &appsv1.ReplicaSet{}, newGVR("apps", "v1", "replicasets")},
		{"ReplicaSetList", &appsv1.ReplicaSetList{}, newGVR("apps", "v1", "replicasets")},
		{"StatefulSet", &appsv1.StatefulSet{}, newGVR("apps", "v1", "statefulsets")},
		{"StatefulSetList", &appsv1.StatefulSetList{}, newGVR("apps", "v1", "statefulsets")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvr, err := GetGVR(tt.object)
			assert.Nil(t, err)
			assert.Equal(t, gvr, tt.wantGVR)
		})
	}
}

func TestContinueMulti(t *testing.T) {
	rv := map[string]string{
		"ns1": "1000",
		"ns2": "2000",
	}

	startKey := "xxx"

	// create continue-multi token
	continueMultiToken, err := encodeContinueMulti(rv, startKey)
	assert.Nil(t, err)

	// decode continue-multi token into map of k8s continue tokens
	continueMap, err := decodeContinueMulti(continueMultiToken)
	assert.Nil(t, err)

	// check token 1
	c1, _ := storage.EncodeContinue("/"+startKey+"\u0000", "/", 1000)
	assert.Equal(t, continueMap["ns1"], c1)

	// check token 2
	c2, _ := storage.EncodeContinue("/"+startKey+"\u0000", "/", 2000)
	assert.Equal(t, continueMap["ns2"], c2)
}

func TestMergeResultsSuccess(t *testing.T) {
	tests := []struct {
		name        string
		results     []corev1.PodList
		listOptions metav1.ListOptions
		wantMerged  corev1.PodList
	}{
		{
			"no items",
			[]corev1.PodList{
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "1", RemainingItemCount: ptr.To[int64](0)},
					Items:    []corev1.Pod{},
				},
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "2", RemainingItemCount: ptr.To[int64](0)},
					Items:    []corev1.Pod{},
				},
			},
			metav1.ListOptions{Limit: 10},
			corev1.PodList{
				ListMeta: metav1.ListMeta{
					ResourceVersion:    "eyJuczAiOiIxIiwibnMxIjoiMiJ9",
					RemainingItemCount: ptr.To[int64](0),
					Continue:           "",
				},
				Items: []corev1.Pod{},
			},
		},
		{
			"num items less than limit",
			[]corev1.PodList{
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "1", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-1-1"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-1-2"}},
					},
				},
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "2", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-2-1"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-2-2"}},
					},
				},
			},
			metav1.ListOptions{Limit: 10},
			corev1.PodList{
				ListMeta: metav1.ListMeta{
					ResourceVersion:    "eyJuczAiOiIxIiwibnMxIjoiMiJ9",
					RemainingItemCount: ptr.To[int64](0),
					Continue:           "",
				},
				Items: []corev1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "item-1-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-1-2"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-2-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-2-2"}},
				},
			},
		},
		{
			"num items more than limit",
			[]corev1.PodList{
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "1", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-1-1"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-1-2"}},
					},
				},
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "2", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-2-1"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-2-2"}},
					},
				},
			},
			metav1.ListOptions{Limit: 3},
			corev1.PodList{
				ListMeta: metav1.ListMeta{
					ResourceVersion:    "eyJuczAiOiIxIiwibnMxIjoiMiJ9",
					RemainingItemCount: ptr.To[int64](1),
					Continue:           "eyJydiI6eyJuczAiOiIxIiwibnMxIjoiMiJ9LCJzdGFydCI6Iml0ZW0tMi0xIn0=",
				},
				Items: []corev1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "item-1-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-1-2"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-2-1"}},
				},
			},
		},
		{
			"items with mixed sorting",
			[]corev1.PodList{
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "1", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-A"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-C"}},
					},
				},
				{
					ListMeta: metav1.ListMeta{ResourceVersion: "2", RemainingItemCount: ptr.To[int64](0)},
					Items: []corev1.Pod{
						{ObjectMeta: metav1.ObjectMeta{Name: "item-B"}},
						{ObjectMeta: metav1.ObjectMeta{Name: "item-D"}},
					},
				},
			},
			metav1.ListOptions{Limit: 3},
			corev1.PodList{
				ListMeta: metav1.ListMeta{
					ResourceVersion:    "eyJuczAiOiIxIiwibnMxIjoiMiJ9",
					RemainingItemCount: ptr.To[int64](1),
					Continue:           "eyJydiI6eyJuczAiOiIxIiwibnMxIjoiMiJ9LCJzdGFydCI6Iml0ZW0tQyJ9",
				},
				Items: []corev1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "item-A"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-B"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "item-C"}},
				},
			},
		},
	}

	for _, tt := range tests {
		fetchResponses := []FetchResponse{}

		// build response objects
		for i, result := range tt.results {
			// convert object
			rObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&result)
			assert.Nil(t, err)

			// convert items
			items := []unstructured.Unstructured{}
			for _, item := range result.Items {
				itemObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&item)
				assert.Nil(t, err)
				items = append(items, unstructured.Unstructured{Object: itemObj})
			}

			resp := FetchResponse{
				Namespace: "ns" + strconv.Itoa(i),
				Result:    &unstructured.UnstructuredList{Object: rObj, Items: items},
			}

			fetchResponses = append(fetchResponses, resp)
		}

		// merge results
		mergedResultObj, err := mergeResults(fetchResponses, tt.listOptions)
		assert.Nil(t, err)

		// check result
		mergedResult := corev1.PodList{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(mergedResultObj.UnstructuredContent(), &mergedResult)
		assert.Nil(t, err)

		// check metadata
		assert.Equal(t, tt.wantMerged.ResourceVersion, mergedResult.ResourceVersion)
		assert.Equal(t, tt.wantMerged.RemainingItemCount, mergedResult.RemainingItemCount)
		assert.Equal(t, tt.wantMerged.Continue, mergedResult.Continue)

		// check number of items returned
		assert.Equal(t, len(tt.wantMerged.Items), len(mergedResult.Items))

		// check order
		for i, wantItem := range tt.wantMerged.Items {
			assert.Equal(t, wantItem.Name, mergedResult.Items[i].Name)
		}
	}
}

func TestMergeResultsError(t *testing.T) {
	// build first result
	r1 := corev1.PodList{}
	r1Obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&r1)

	// build fetch responses
	fetchResponses := []FetchResponse{
		{Namespace: "ns1", Result: &unstructured.UnstructuredList{Object: r1Obj}},
		{Namespace: "ns2", Error: gqlerrors.ErrForbidden},
	}

	// merge results
	_, err := mergeResults(fetchResponses, metav1.ListOptions{})
	assert.NotNil(t, err)
}

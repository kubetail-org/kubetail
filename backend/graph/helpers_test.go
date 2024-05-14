package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage"
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

func TestMergeResults(t *testing.T) {
	fetchResponses := 
}
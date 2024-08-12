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

package logmetadata2

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestLogfileRegex(t *testing.T) {
	tests := []struct {
		name        string
		setInput    string
		wantMatches []string
	}{
		{
			"without slash",
			"pn_ns_cn-123.log",
			[]string{"pn", "ns", "cn", "123"},
		},
		{
			"pod name with hyphen",
			"pn-123_ns_cn-123.log",
			[]string{"pn-123", "ns", "cn", "123"},
		},
		{
			"namespace with hyphen",
			"pn_ns-123_cn-123.log",
			[]string{"pn", "ns-123", "cn", "123"},
		},
		{
			"container name with hyphen",
			"pn_ns_cn-123-123.log",
			[]string{"pn", "ns", "cn-123", "123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := logfileRegex.FindStringSubmatch(tt.setInput)

			// check number of matches
			require.Equal(t, len(tt.wantMatches)+1, len(matches))

			// check matched values
			for i := 0; i < len(tt.wantMatches); i++ {
				require.Equal(t, tt.wantMatches[i], matches[i+1])
			}
		})
	}
}

func TestCheckPermissionFailure(t *testing.T) {
	t.Run("namespaces required", func(t *testing.T) {
		err := checkPermission(context.Background(), nil, []string{}, "x")
		require.ErrorContains(t, err, "namespaces required")
	})

	t.Run("single namespace", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		err := checkPermission(context.Background(), clientset, []string{"ns1"}, "x")
		require.ErrorContains(t, err, "permission denied")
	})

	t.Run("multiple namespaces", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		err := checkPermission(context.Background(), clientset, []string{"ns1", "ns2"}, "x")
		require.ErrorContains(t, err, "permission denied")
	})

	t.Run("one of several not allowed", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		allowSSAR(clientset, []string{"ns1"}, []string{"x"})
		err := checkPermission(context.Background(), clientset, []string{"ns1", "ns2"}, "x")
		require.ErrorContains(t, err, "permission denied")
	})
}

func TestCheckPermissionSuccess(t *testing.T) {
	t.Run("single namespace", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		allowSSAR(clientset, []string{"ns1"}, []string{"x"})
		err := checkPermission(context.Background(), clientset, []string{"ns1"}, "x")
		require.Nil(t, err)
	})

	t.Run("multiple namespaces", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		allowSSAR(clientset, []string{"ns1", "ns2"}, []string{"x"})
		err := checkPermission(context.Background(), clientset, []string{"ns1", "ns2"}, "x")
		require.Nil(t, err)
	})
}

func TestCheckPermissionRequest(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Cast the action to CreateAction to access the object being created
		createAction := action.(k8stesting.CreateAction)
		ssar := createAction.GetObject().(*authv1.SelfSubjectAccessReview)

		// check ssar
		require.Equal(t, "ns1", ssar.Spec.ResourceAttributes.Namespace)
		require.Equal(t, "", ssar.Spec.ResourceAttributes.Group)
		require.Equal(t, "x", ssar.Spec.ResourceAttributes.Verb)
		require.Equal(t, "pods/log", ssar.Spec.ResourceAttributes.Resource)

		// Set the Allowed field to true in the response
		ssar.Status.Allowed = true

		// Return the modified SelfSubjectAccessReview
		return true, ssar, nil
	})

	checkPermission(context.Background(), clientset, []string{"ns1"}, "x")
}

func TestContainerLogsWatcher(t *testing.T) {
	t.Run("handles close", func(t *testing.T) {
		// temporary directory for container log links
		dirname, err := os.MkdirTemp("", "logmetadata-containerlogsdir-")
		require.Nil(t, err)
		defer os.RemoveAll(dirname)

		// init watcher
		watcher, err := newContainerLogsWatcher(context.Background(), dirname, []string{})
		require.Nil(t, err)

		// check that events passes through close event
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok := <-watcher.Events
			require.False(t, ok)
		}()

		// execute close
		err = watcher.Close()
		require.Nil(t, err)

		// wait
		wg.Wait()
	})
}

// Copyright 2024-2026 The Kubetail Authors
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

package k8shelpers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestServiceAccountToken_refreshToken_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Mock the CreateToken response
	clientset.Fake.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		tokenResponse := &authv1.TokenRequest{
			Status: authv1.TokenRequestStatus{
				Token:               "mock-token",
				ExpirationTimestamp: metav1.Now(),
			},
		}
		return true, tokenResponse, nil
	})

	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	// Initialize
	sat, err := NewServiceAccountToken(context.Background(), clientset, "", "", shutdownCh)
	assert.Nil(t, err)

	// Check token
	token, err := sat.Token(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "mock-token", token)
}

func TestServiceAccountToken_refreshToken_Failure(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Mock the CreateToken response
	clientset.Fake.PrependReactor("create", "serviceaccounts", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		err = errors.NewNotFound(
			schema.GroupResource{Group: "", Resource: "serviceaccounts"},
			"name-goes-here",
		)
		return true, nil, err
	})

	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	// Initialize
	_, err := NewServiceAccountToken(context.Background(), clientset, "", "", shutdownCh)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "serviceaccounts \"name-goes-here\" not found")
}

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

package mock

import (
	"context"

	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Represents mock for connection manager
type MockConnectionManager struct {
	mock.Mock
}

func (m *MockConnectionManager) GetOrCreateRestConfig(kubeContext string) (*rest.Config, error) {
	ret := m.Called(kubeContext)

	var r0 *rest.Config
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*rest.Config)
	}

	return r0, ret.Error(1)
}

func (m *MockConnectionManager) GetOrCreateClientset(kubeContext string) (kubernetes.Interface, error) {
	ret := m.Called(kubeContext)

	var r0 kubernetes.Interface
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(kubernetes.Interface)
	}

	return r0, ret.Error(1)
}

func (m *MockConnectionManager) GetOrCreateDynamicClient(kubeContext string) (dynamic.Interface, error) {
	ret := m.Called(kubeContext)

	var r0 dynamic.Interface
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(dynamic.Interface)
	}

	return r0, ret.Error(1)
}

func (m *MockConnectionManager) NewInformer(ctx context.Context, kubeContext string, token string, namespace string, gvr schema.GroupVersionResource) (informers.GenericInformer, func(), error) {
	ret := m.Called(ctx, kubeContext, token, namespace, gvr)

	var r0 informers.GenericInformer
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(informers.GenericInformer)
	}

	var r1 func()
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(func())
	}

	return r0, r1, ret.Error(2)
}

func (m *MockConnectionManager) GetDefaultNamespace(kubeContext string) string {
	ret := m.Called(kubeContext)
	return ret.String(0)
}

func (m *MockConnectionManager) GetNamespaceList(kubeContext string) ([]string, error) {
	ret := m.Called(kubeContext)

	var r0 []string
	if ret.Get(0) != nil {
		r0 = ret.Get(0).([]string)
	}

	return r0, ret.Error(1)
}

func (m *MockConnectionManager) DerefKubeContext(kubeContextPtr *string) string {
	ret := m.Called(kubeContextPtr)
	return ret.String(0)
}

func (m *MockConnectionManager) WaitUntilReady(ctx context.Context, kubeContext string) error {
	ret := m.Called(ctx, kubeContext)
	return ret.Error(0)
}

func (m *MockConnectionManager) Shutdown(ctx context.Context) error {
	ret := m.Called(ctx)
	return ret.Error(0)
}

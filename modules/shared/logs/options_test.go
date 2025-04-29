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
	"testing"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOptions(t *testing.T) {
	opts := []Option{
		WithKubeContext("test"),
		WithRegions([]string{"us-east-1"}),
	}

	// Init connection manager
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
	cm.On("GetDefaultNamespace", mock.Anything).Return("default")

	s, err := NewStream(context.Background(), cm, nil, opts...)
	fmt.Println(err)
	fmt.Println(s)
}

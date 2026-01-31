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

package mock

import (
	"context"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
	"github.com/stretchr/testify/mock"
)

type MockStream struct {
	mock.Mock
}

func (m *MockStream) Start(ctx context.Context) error {
	args := m.Called(ctx)

	return args.Error(0)
}

func (m *MockStream) Records() <-chan logs.LogRecord {
	args := m.Called()

	if args.Get(0) == nil {
		return nil
	}

	return args.Get(0).(<-chan logs.LogRecord)
}

func (m *MockStream) Sources() []logs.LogSource {
	args := m.Called()

	return args.Get(0).([]logs.LogSource)
}

func (m *MockStream) Err() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockStream) Close() {
	m.Called()
}

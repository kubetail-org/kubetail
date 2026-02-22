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
	"github.com/stretchr/testify/mock"

	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"
)

// MockChecker is a mock implementation of versioncheck.Checker
type MockChecker struct {
	mock.Mock
}

func (m *MockChecker) GetLatestCLIVersion() (*versioncheck.VersionInfo, error) {
	ret := m.Called()

	var r0 *versioncheck.VersionInfo
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*versioncheck.VersionInfo)
	}

	return r0, ret.Error(1)
}

func (m *MockChecker) GetLatestHelmChartVersion() (*versioncheck.VersionInfo, error) {
	ret := m.Called()

	var r0 *versioncheck.VersionInfo
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*versioncheck.VersionInfo)
	}

	return r0, ret.Error(1)
}

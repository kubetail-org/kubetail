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

//go:build unix

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// The build's cloud platforms, in Settings' order.
func TestTheBuildsCloudPlatforms(t *testing.T) {
	assert.Equal(t, []permissions.Target{
		permissions.AWS, permissions.GitHub, permissions.GCP, permissions.Azure,
	}, cloudTargets(builtCloudPlatforms(nil, nil)))
}

// Each AWS login made is its own, so no two profiles share one.
func TestEachAWSLoginIsItsOwn(t *testing.T) {
	assert.NotSame(t, newAWSLogin("prod"), newAWSLogin("dev"))
}

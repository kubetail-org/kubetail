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

package util

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserAgent(t *testing.T) {
	ua := UserAgent("kubetail", "0.11.1")

	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	goVersion := strings.TrimPrefix(runtime.Version(), "go")

	assert.Contains(t, ua, "kubetail/0.11.1")
	assert.Contains(t, ua, osName)
	assert.Contains(t, ua, runtime.GOARCH)
	assert.Contains(t, ua, "Go/"+goVersion)
}

func TestUserAgent_Format(t *testing.T) {
	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	goVersion := strings.TrimPrefix(runtime.Version(), "go")

	tests := []struct {
		name     string
		product  string
		version  string
		expected string
	}{
		{
			"cli",
			"kubetail", "0.11.1",
			fmt.Sprintf("kubetail/0.11.1 (%s; %s) Go/%s", osName, runtime.GOARCH, goVersion),
		},
		{
			"dashboard",
			"kubetail-dashboard", "0.9.1",
			fmt.Sprintf("kubetail-dashboard/0.9.1 (%s; %s) Go/%s", osName, runtime.GOARCH, goVersion),
		},
		{
			"cluster-api",
			"kubetail-cluster-api", "0.5.1",
			fmt.Sprintf("kubetail-cluster-api/0.5.1 (%s; %s) Go/%s", osName, runtime.GOARCH, goVersion),
		},
		{
			"cluster-agent",
			"kubetail-cluster-agent", "0.6.1",
			fmt.Sprintf("kubetail-cluster-agent/0.6.1 (%s; %s) Go/%s", osName, runtime.GOARCH, goVersion),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ua := UserAgent(tt.product, tt.version)
			assert.Equal(t, tt.expected, ua)
		})
	}
}

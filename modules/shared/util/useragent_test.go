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
	ua := UserAgent("0.11.1", "dashboard", "desktop")

	// Check product token
	assert.Contains(t, ua, "kubetail/0.11.1")

	// Check component
	assert.Contains(t, ua, "dashboard")

	// Check env
	assert.Contains(t, ua, "env=desktop")

	// Check OS name is title-cased
	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	assert.Contains(t, ua, osName)

	// Check architecture
	assert.Contains(t, ua, runtime.GOARCH)

	// Check Go version
	goVersion := strings.TrimPrefix(runtime.Version(), "go")
	assert.Contains(t, ua, "Go/"+goVersion)
}

func TestUserAgent_Format(t *testing.T) {
	ua := UserAgent("1.2.3", "cli", "kubernetes")

	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	goVersion := strings.TrimPrefix(runtime.Version(), "go")

	expected := fmt.Sprintf("kubetail/1.2.3 (cli; env=kubernetes; %s; %s) Go/%s",
		osName, runtime.GOARCH, goVersion,
	)
	assert.Equal(t, expected, ua)
}

func TestUserAgent_DifferentInputs(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		component string
		env       string
	}{
		{"dashboard desktop", "0.11.1", "dashboard", "desktop"},
		{"cli kubernetes", "1.0.0", "cli", "kubernetes"},
		{"cluster-api kubernetes", "2.5.0", "cluster-api", "kubernetes"},
		{"dev version", "dev", "dashboard", "desktop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ua := UserAgent(tt.version, tt.component, tt.env)

			assert.Contains(t, ua, fmt.Sprintf("kubetail/%s", tt.version))
			assert.Contains(t, ua, tt.component)
			assert.Contains(t, ua, fmt.Sprintf("env=%s", tt.env))
			assert.Contains(t, ua, "Go/")
		})
	}
}

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

package logrecords

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripRuntimePrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with prefix",
			input:    "runtime://abc123",
			expected: "abc123",
		},
		{
			name:     "without prefix",
			input:    "containerd://xyz456",
			expected: "xyz456",
		},
		{
			name:     "with multiple separators",
			input:    "runtime://xxx://xyz456",
			expected: "xxx://xyz456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripRuntimePrefix(tt.input)
			assert.Equal(t, tt.expected, result, "stripRuntimePrefix(%q) should return %q", tt.input, tt.expected)
		})
	}
}

func TestFindLogFile(t *testing.T) {
	// Create a temporary directory for tests
	tempDir, err := os.MkdirTemp("", "test-find-log-files")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Add files
	fileNames := []string{
		"pn1_ns1_cn1-id1.log",
		"pn2_ns1_cn1-id2.log",
		"pn2_ns1_cn2-id3.log",
		"pn2_ns1_cn2-id4.log",
	}

	for _, fileName := range fileNames {
		if err := os.WriteFile(filepath.Join(tempDir, fileName), []byte("t"), 0644); err != nil {
			t.Fatalf("Failed to create test log file: %v", err)
		}
	}

	// Test cases
	tests := []struct {
		name          string
		namespace     string
		podName       string
		containerName string
		containerID   string
		wantPathname  string
		wantErr       error
	}{
		{
			name:          "match pod with only one entry",
			namespace:     "ns1",
			podName:       "pn1",
			containerName: "cn1",
			containerID:   "id1",
			wantPathname:  filepath.Join(tempDir, fileNames[0]),
			wantErr:       nil,
		},
		{
			name:          "match pod with two containers with different names",
			namespace:     "ns1",
			podName:       "pn2",
			containerName: "cn1",
			containerID:   "id2",
			wantPathname:  filepath.Join(tempDir, fileNames[1]),
			wantErr:       nil,
		},
		{
			name:          "match pod with two containers with same name",
			namespace:     "ns1",
			podName:       "pn2",
			containerName: "cn2",
			containerID:   "id3",
			wantPathname:  filepath.Join(tempDir, fileNames[2]),
			wantErr:       nil,
		},
		{
			name:          "return err if file doesn't exist",
			namespace:     "x",
			podName:       "x",
			containerName: "x",
			containerID:   "x",
			wantPathname:  "",
			wantErr:       fmt.Errorf("log file not found: %s", filepath.Join(tempDir, "x_x_x-x.log")),
		},
		{
			name:          "return err if id is mismatched",
			namespace:     "ns1",
			podName:       "pn1",
			containerName: "cn1",
			containerID:   "id2",
			wantPathname:  "",
			wantErr:       fmt.Errorf("log file not found: %s", filepath.Join(tempDir, "pn1_ns1_cn1-id2.log")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findLogFile(tempDir, tt.namespace, tt.podName, tt.containerName, tt.containerID)
			assert.Equal(t, tt.wantPathname, result)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

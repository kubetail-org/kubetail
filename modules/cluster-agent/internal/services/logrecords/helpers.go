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
	"strings"
)

// Strip runtime prefix from container IDs
func stripRuntimePrefix(containerID string) string {
	parts := strings.SplitN(containerID, "://", 2)
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[1]
}

// Find container log file on disk given metadata
func findLogFile(containerLogsDir string, namespace string, podName string, containerName string, containerID string) (string, error) {
	// Strip runtime prefix from container ID
	containerID = stripRuntimePrefix(containerID)

	// Check if file exists
	filePath := filepath.Join(containerLogsDir, fmt.Sprintf("%s_%s_%s-%s.log", podName, namespace, containerName, containerID))
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("log file not found: %s", filePath)
		}
		return "", err
	}
	return filePath, nil
}

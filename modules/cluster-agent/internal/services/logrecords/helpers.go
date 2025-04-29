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
	"path/filepath"
)

func findLogFile(containerLogsDir string, namespace string, podName string, containerName string) (string, error) {
	// Construct pattern
	pattern := filepath.Join(containerLogsDir, fmt.Sprintf("%s_%s_%s-*.log", podName, namespace, containerName))

	// Find file
	matchingFiles, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	} else if len(matchingFiles) != 1 {
		return "", fmt.Errorf("matched %d files", len(matchingFiles))
	}

	return matchingFiles[0], nil
}

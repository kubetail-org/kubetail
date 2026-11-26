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

//go:build !linux

package bash

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A stopped run with the internet keeps its exit code, which is the command's.
func TestAStoppedNetworkRunsHeaderKeepsItsExitCode(t *testing.T) {
	timedOut := result{Output: "partial\n", ExitCode: 143, Stop: stopTimeout}
	cancelled := result{Output: "partial\n", ExitCode: 130, Stop: stopCancel}

	assert.Equal(t, "Command timed out after 30s (exit code 143)\npartial\n", resultText(timedOut, 30*time.Second, nil, confinedWithInternet))
	assert.Equal(t, "Command cancelled (exit code 130)\npartial\n", resultText(cancelled, time.Minute, nil, confinedWithInternet))
}

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

package bash

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A stopped run with the internet exits by pasta, so its header names how it
// was stopped and no exit code; one without keeps the code.
func TestAStoppedNetworkRunsHeaderHasNoExitCode(t *testing.T) {
	t.Parallel()

	timedOut := result{Output: "partial\n", Stop: stopTimeout}
	cancelled := result{Output: "partial\n", Stop: stopCancel}

	assert.Equal(t, "Command timed out after 30s\npartial\n", resultText(timedOut, 30*time.Second, nil, confinedWithInternet))
	assert.Equal(t, "Command cancelled\npartial\n", resultText(cancelled, time.Minute, nil, confinedWithInternet))
	assert.Equal(t, "Command timed out after 30s (exit code 0)\npartial\n", resultText(timedOut, 30*time.Second, nil, confined))
}

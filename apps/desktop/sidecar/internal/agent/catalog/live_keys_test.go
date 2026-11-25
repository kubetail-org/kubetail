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

//go:build livecache || liveswitch

package catalog

import (
	"os"
	"testing"
)

// liveKeys is every provider key the environment holds, by provider id, for the
// live checks. A check with none set is skipped.
func liveKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	for id, name := range KeyVars() {
		if key := os.Getenv(name); key != "" {
			keys[id] = key
		}
	}
	if len(keys) == 0 {
		t.Skip("no provider key is set")
	}
	return keys
}

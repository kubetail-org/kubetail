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

package app

import (
	"maps"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
)

// launch is what the launch's run of the login shell answered.
type launch struct {
	path  []string          // the shell's PATH; nil when it was not read
	fault string            // why it was not read; "" when it was
	keys  map[string]string // the provider keys the shell set, by variable
}

// withShellKeys is keys, by provider id, with each key the shell set added
// under its provider. A key the launch environment set wins.
func withShellKeys(keys, shell map[string]string) map[string]string {
	merged := maps.Clone(keys)
	for id, keyVar := range catalog.KeyVars() {
		if key := shell[keyVar]; key != "" && merged[id] == "" {
			if merged == nil {
				merged = map[string]string{}
			}
			merged[id] = key
		}
	}
	return merged
}

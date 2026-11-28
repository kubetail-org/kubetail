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

package kubesignals

import (
	"encoding/json"
	"unicode/utf8"
)

// What the cluster names is attacker-controlled text of any length. Each value
// is cut to fit the monitor's bounds as JSON stores it, so a long name or one of
// control characters is stored cut rather than dropping its observation.
const (
	maxLabelBytes = 256 // the monitor's bound on a label
	// maxAttributeJSON keeps the four attributes inside the monitor's 4 KiB.
	maxAttributeJSON = 900
	maxDetailJSON    = 1 << 10
)

// cutLabel is s cut to maxLabelBytes, at a rune boundary.
func cutLabel(s string) string {
	if len(s) <= maxLabelBytes {
		return s
	}
	s = s[:maxLabelBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func cutAttribute(s string) string { return fitJSON(s, maxAttributeJSON) }

func cutDetail(s string) string { return fitJSON(s, maxDetailJSON) }

// fitJSON is s cut, at a rune boundary, so its JSON string takes at most n bytes.
func fitJSON(s string, n int) string {
	for jsonLen(s) > n {
		cut := len(s) * 3 / 4
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

func jsonLen(s string) int {
	b, _ := json.Marshal(s) // a string always marshals
	return len(b)
}

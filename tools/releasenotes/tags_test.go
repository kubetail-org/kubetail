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

package main

import "testing"

func TestPreviousTag(t *testing.T) {
	tags := []string{
		"cli/v0.1.0", "cli/v0.2.0-rc.1", "cli/v0.2.0-rc.2", "cli/v0.2.0-rc.10", "cli/v0.2.0", "cli/v0.3.0-rc.1",
		"dashboard/v0.2.1", "cli/v0.2", "cli/vX", "other",
	}
	for _, tc := range []struct {
		component, tag, want string
	}{
		{"cli", "cli/v0.1.0", ""},
		{"cli", "cli/v0.2.0-rc.1", "cli/v0.1.0"},
		{"cli", "cli/v0.2.0-rc.2", "cli/v0.2.0-rc.1"},
		{"cli", "cli/v0.2.0-rc.10", "cli/v0.2.0-rc.2"}, // a number after a dot compares as one
		{"cli", "cli/v0.2.0", "cli/v0.1.0"},            // a stable release folds its rcs
		{"cli", "cli/v0.3.0-rc.1", "cli/v0.2.0"},
		{"cli", "cli/v0.3.0", "cli/v0.2.0"},
		{"dashboard", "dashboard/v0.3.0", "dashboard/v0.2.1"}, // another component's tags are not its own
		{"dashboard", "dashboard/v0.2.1", ""},
	} {
		got, err := previousTag(tags, tc.component, tc.tag)
		if err != nil {
			t.Fatalf("%s: %v", tc.tag, err)
		}
		if got != tc.want {
			t.Errorf("previousTag(%s) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestPreviousTagRejectsAnotherComponentsOrAMalformedTag(t *testing.T) {
	for _, tag := range []string{"dashboard/v0.1.0", "cli/v0.1", "cli/0.1.0", "cli/v0.1.0+build"} {
		if _, err := previousTag(nil, "cli", tag); err == nil {
			t.Errorf("previousTag(%s) accepted it", tag)
		}
	}
}

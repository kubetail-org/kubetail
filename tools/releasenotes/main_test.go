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

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestCommandRejectsBadUsage(t *testing.T) {
	tl, _ := fixtureTool(t)
	for _, args := range [][]string{nil, {"bump"}, {"collect", "cli"}, {"check", "a", "b"}} {
		err := tl.command(context.Background(), args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "usage: releasenotes") {
			t.Errorf("%q: err = %v", args, err)
		}
	}
}

func TestCommandDispatches(t *testing.T) {
	tl, _ := fixtureTool(t)
	notesPath := writeNotes(t, testNotes())
	for _, tc := range []struct {
		args []string
		out  string // empty when the command fails
	}{
		{[]string{"collect", "cli", "cli/v0.2.0"}, `"range": "cli/v0.1.0..cli/v0.2.0"`},
		{[]string{"check", notesPath}, "passes against"},
		{[]string{"render", notesPath}, "## Fixes"},
		{[]string{"publish", "cli/v0.2.0", notesPath}, ""},
	} {
		var out strings.Builder
		err := tl.command(context.Background(), tc.args, &out)
		if (err != nil) != (tc.out == "") {
			t.Errorf("%q: err = %v", tc.args, err)
		}
		if !strings.Contains(out.String(), tc.out) {
			t.Errorf("%q: output lacks %q:\n%s", tc.args, tc.out, out.String())
		}
	}
}

func TestExecRunReportsStderr(t *testing.T) {
	_, err := execRun(context.Background(), "git", "-C", t.TempDir(), "rev-parse", "--show-toplevel")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("err = %v", err)
	}
}

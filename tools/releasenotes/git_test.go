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
	"reflect"
	"strings"
	"testing"
)

func TestParseCommit(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    change
	}{
		{"feat(cli): add a flag (#12)", change{Subject: "feat(cli): add a flag", PR: 12, Type: "feat", Scope: "cli"}},
		{"fix!: drop a flag (#3)", change{Subject: "fix!: drop a flag", PR: 3, Type: "fix", Breaking: true}},
		{"refactor(a b)!: move it", change{Subject: "refactor(a b)!: move it", Type: "refactor", Scope: "a b", Breaking: true}},
		{`Revert "feat: x" (#9)`, change{Subject: `Revert "feat: x"`, PR: 9}},
		{"mention (#9) mid-subject", change{Subject: "mention (#9) mid-subject"}},
	} {
		got := parseCommit("sha", tc.subject)
		tc.want.Commit = "sha"
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseCommit(%q) = %+v, want %+v", tc.subject, got, tc.want)
		}
	}
}

func TestLogReadsTheRangeUnderThePaths(t *testing.T) {
	ctx := context.Background()
	tl := tool{dir: fixtureRepo(t), run: execRun}

	changes, err := tl.log(ctx, "cli/v0.1.0..cli/v0.2.0", []string{":(top,glob)cli/**"})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range changes {
		subjects = append(subjects, c.Subject)
		if len(c.Commit) != 40 {
			t.Errorf("sha %q", c.Commit)
		}
	}
	want := "fix(cli): handle empty|tweak the cli by hand|docs(cli): tweak the help|feat(cli)!: rename the flag"
	if got := strings.Join(subjects, "|"); got != want {
		t.Errorf("subjects = %s, want %s", got, want)
	}

	// A range with no commit under the paths is empty.
	changes, err = tl.log(ctx, "cli/v0.2.0", []string{":(top,glob)none/**"})
	if err != nil || len(changes) != 0 {
		t.Errorf("changes = %+v, err = %v", changes, err)
	}
}

func TestLogFailsOutsideACheckout(t *testing.T) {
	tl := tool{dir: t.TempDir(), run: execRun}
	if _, err := tl.log(context.Background(), "HEAD", nil); err == nil {
		t.Error("log outside a checkout succeeded")
	}
}

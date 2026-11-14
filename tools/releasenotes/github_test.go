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
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGHReadsAPullRequest(t *testing.T) {
	tl, _ := fixtureTool(t)
	pr, err := tl.pullRequest(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "PR 5" || pr.Author.Login != "app/dependabot" || !pr.Author.IsBot ||
		len(pr.Labels) != 1 || pr.Labels[0].Name != "l5" {
		t.Errorf("pr = %+v", pr)
	}
}

// The fixture's replies are keyed by cli/v0.1.0's commit time in UTC.
func TestGHAsksWhetherAnAuthorMergedBefore(t *testing.T) {
	tl, _ := fixtureTool(t)
	for login, want := range map[string]bool{"alice": true, "bob": false} {
		got, err := tl.mergedBefore(context.Background(), login, "cli/v0.1.0")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("mergedBefore(%s) = %t", login, got)
		}
	}
}

func TestGHFails(t *testing.T) {
	ctx := context.Background()
	tl, f := fixtureTool(t)
	if _, err := tl.mergedBefore(ctx, "alice", "no-such-ref"); err == nil {
		t.Error("mergedBefore a missing ref succeeded")
	}
	fail := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("offline") }
	garbage := func(context.Context, string, ...string) ([]byte, error) { return []byte("{"), nil }
	for _, run := range []func(context.Context, string, ...string) ([]byte, error){fail, garbage} {
		tl.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "git" {
				return f.run(ctx, name, args...)
			}
			return run(ctx, name, args...)
		}
		if _, err := tl.pullRequest(ctx, 1); err == nil {
			t.Error("pullRequest succeeded")
		}
		if _, err := tl.mergedBefore(ctx, "alice", "cli/v0.1.0"); err == nil {
			t.Error("mergedBefore succeeded")
		}
	}
	tl.run = func(context.Context, string, ...string) ([]byte, error) { return []byte("yesterday"), nil }
	if _, err := tl.mergedBefore(ctx, "alice", "cli/v0.1.0"); err == nil {
		t.Error("mergedBefore read a malformed time")
	}
}

func TestSectionsSplitsAtHeadingsAndDropsComments(t *testing.T) {
	body := "Closes #1\r\n\r\n## Summary\r\n\r\n<!-- why -->\r\nBecause.\r\n\r\n## Key Changes\n\n- one\n- two\n<!--\nmulti\n-->\n## Checklist\n\n<!-- tick -->\n"
	got := sections(body)
	want := map[string]string{"summary": "Because.", "key changes": "- one\n- two", "checklist": ""}
	if len(got) != len(want) {
		t.Errorf("sections = %q", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestClipCutsOnARuneBoundary(t *testing.T) {
	if s := clip("short"); s != "short" {
		t.Errorf("clip = %q", s)
	}
	got := clip("a" + strings.Repeat("é", maxText))
	if !utf8.ValidString(got) || !strings.HasSuffix(got, " […]") || len(got) > maxText+len(" […]") {
		t.Errorf("clip gave %d bytes, valid %t", len(got), utf8.ValidString(got))
	}
}

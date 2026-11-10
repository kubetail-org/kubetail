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
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	got := render(testNotes(), testCollection())
	want := "A release that renames a flag.\n\n" +
		"This release has changes from **2 contributors** over **3 pull requests**. Thank you to everyone who helped make it.\n\n" +
		"## Highlights\n\n" +
		"### A new name for the flag\n\n" +
		"PR: [#3](https://github.com/o/r/pull/3)\n\n" +
		"The flag is now `--name`.\n\n" +
		"Scripts that pass `--flag` fail until you rename it.\n\n" +
		"## New\n\n" +
		"- The flag is now `--name`. ([#3](https://github.com/o/r/pull/3))\n" +
		"\n## Fixes\n\n" +
		"- An empty input no longer fails. ([#5](https://github.com/o/r/pull/5))\n" +
		"\n## Upgrade notes\n\n" +
		"- Rename `--flag` to `--name` in scripts. ([#3](https://github.com/o/r/pull/3))\n" +
		"\n<details>\n<summary>All changes</summary>\n\n" +
		"- fix(cli): handle empty ([#5](https://github.com/o/r/pull/5)) by @dependabot\n" +
		"- tweak the cli by hand ([`0000000`](https://github.com/o/r/commit/0000000000))\n" +
		"- docs(cli): tweak the help ([#4](https://github.com/o/r/pull/4)) by @bob\n" +
		"- feat(cli)!: rename the flag ([#3](https://github.com/o/r/pull/3)) by @alice\n" +
		"\n</details>\n" +
		"\n## New contributors\n\n" +
		"- @bob made their first contribution in [#4](https://github.com/o/r/pull/4)\n" +
		"\n**Full changelog**: [cli/v0.1.0...cli/v0.2.0](https://github.com/o/r/compare/cli/v0.1.0...cli/v0.2.0)\n" +
		"\nEach download's SHA-256 is in `SHA256SUMS`, signed in `SHA256SUMS.asc`.\n"
	if got != want {
		t.Errorf("render =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderAFirstReleaseWithNothingInIt(t *testing.T) {
	c := collection{Repo: testRepo, Tag: "cli/v0.1.0", Range: "cli/v0.1.0"}
	got := render(notes{Summary: []string{"First."}}, c)
	for _, want := range []string{
		"First.\n\n<details>",
		"No change touches this component.",
		"**Full changelog**: [cli/v0.1.0](https://github.com/o/r/commits/cli/v0.1.0)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"## Highlights", "## New", "## New contributors", "Thank you"} {
		if strings.Contains(got, unwanted+"\n") {
			t.Errorf("render has %q:\n%s", unwanted, got)
		}
	}
}

// Only the CLI's release has downloads to check.
func TestRenderNamesChecksumsForTheCLIAlone(t *testing.T) {
	c := collection{Repo: testRepo, Tag: "dashboard/v0.1.0", Range: "dashboard/v0.1.0"}
	got := render(notes{Summary: []string{"First."}}, c)
	if strings.Contains(got, "SHA256SUMS") {
		t.Errorf("render names checksums for an image:\n%s", got)
	}
	if !strings.HasSuffix(got, "(https://github.com/o/r/commits/dashboard/v0.1.0)\n") {
		t.Errorf("render ends wrongly:\n%s", got)
	}
}

func TestRenderCitesSeveralPullRequestsUnderAHighlight(t *testing.T) {
	n := testNotes()
	n.Highlights[0].PRs = []int{3, 5}
	got := render(n, testCollection())
	want := "### A new name for the flag\n\nPRs: [#3](https://github.com/o/r/pull/3), [#5](https://github.com/o/r/pull/5)\n\n"
	if !strings.Contains(got, want) {
		t.Errorf("render lacks %q:\n%s", want, got)
	}
}

func TestRenderNamesNoMaintainerAsANewContributor(t *testing.T) {
	c := testCollection()
	c.Changes = append(c.Changes, change{Commit: "6666666666", Subject: "docs: a maintainer's first", PR: 6, Author: "amorey"})
	c.NewContributors = append(c.NewContributors, contributor{"amorey", 6})
	got := render(testNotes(), c)
	_, callouts, _ := strings.Cut(got, "## New contributors")
	if strings.Contains(callouts, "@amorey") {
		t.Errorf("render names a maintainer as a new contributor:\n%s", got)
	}
	for _, want := range []string{"- @bob made their first contribution", "**3 contributors**"} {
		if !strings.Contains(got, want) {
			t.Errorf("render lacks %q:\n%s", want, got)
		}
	}

	c.NewContributors = []contributor{{"amorey", 6}}
	if got := render(testNotes(), c); strings.Contains(got, "## New contributors") {
		t.Errorf("render has a New contributors heading with no one under it:\n%s", got)
	}
}

func TestThanks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes []change
		want    string
	}{
		{"one person, one pull request", []change{{PR: 1, Author: "alice"}},
			"This release has changes from **1 contributor** over **1 pull request**."},
		{"a bot's pull request counts, the bot does not", []change{{PR: 1, Author: "alice"}, {PR: 2, Author: "app/dependabot", Bot: true}, {PR: 3, Author: "alice"}},
			"This release has changes from **1 contributor** over **3 pull requests**."},
		{"only bots", []change{{PR: 1, Author: "app/dependabot", Bot: true}}, ""},
		{"no pull request", []change{{Commit: "abc"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := thanks(collection{Changes: tc.changes})
			if !strings.HasPrefix(got, tc.want) || (tc.want == "") != (got == "") {
				t.Errorf("thanks = %q, want it to start %q", got, tc.want)
			}
		})
	}
}

func TestEscape(t *testing.T) {
	for in, want := range map[string]string{
		"fix: a [link](x) <b>*bold*</b>": `fix: a \[link\](x) \<b\>\*bold\*\</b\>`,
		"feat: add `--a_b` and a_b":      "feat: add `--a_b` and a\\_b",
		"fix: one ` tick [a](b)":         "fix: one ` tick \\[a\\](b)",
		`a \ backslash ~strike~`:         `a \\ backslash \~strike\~`,
	} {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderCmd(t *testing.T) {
	ctx := context.Background()
	tl, _ := fixtureTool(t)
	var out strings.Builder
	if err := tl.renderCmd(ctx, writeNotes(t, testNotes()), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- An empty input no longer fails. ([#5](https://github.com/o/r/pull/5))",
		"- tweak the cli by hand ([`",
		"- @bob made their first contribution in [#4]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("render lacks %q:\n%s", want, out.String())
		}
	}

	// Notes that fail the check are not rendered.
	bad := testNotes()
	bad.Fixes = nil
	out.Reset()
	if err := tl.renderCmd(ctx, writeNotes(t, bad), &out); err == nil || out.Len() != 0 {
		t.Errorf("rendered failing notes: %v %q", err, out.String())
	}
	if err := tl.renderCmd(ctx, filepath.Join(t.TempDir(), "none.json"), &out); err == nil {
		t.Error("rendered a missing file")
	}
}

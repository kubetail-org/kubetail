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
	"fmt"
	"io"
	"slices"
	"strings"
)

// render writes the release's Markdown: the notes' opening, a thanks with the
// range's counts, each highlight under a heading of its own, the sections,
// then every change in the range, the new contributors, the compare link and,
// for the CLI, the checksums. Only the prose comes from notes.json; check has
// already kept links out of it.
func render(n notes, c collection) string {
	base := "https://github.com/" + c.Repo
	prLink := func(pr int) string { return fmt.Sprintf("[#%d](%s/pull/%d)", pr, base, pr) }
	prLinks := func(prs []int) string {
		links := make([]string, len(prs))
		for i, pr := range prs {
			links[i] = prLink(pr)
		}
		return strings.Join(links, ", ")
	}
	paragraphs := func(b *strings.Builder, ps []string) {
		for _, p := range ps {
			b.WriteString(strings.TrimSpace(p) + "\n\n")
		}
	}

	var b strings.Builder
	paragraphs(&b, n.Summary)
	if t := thanks(c); t != "" {
		b.WriteString(t + "\n\n")
	}
	if len(n.Highlights) > 0 {
		b.WriteString("## Highlights\n\n")
		for _, h := range n.Highlights {
			label := "PR"
			if len(h.PRs) > 1 {
				label = "PRs"
			}
			fmt.Fprintf(&b, "### %s\n\n%s: %s\n\n", strings.TrimSpace(h.Title), label, prLinks(h.PRs))
			paragraphs(&b, h.Body)
		}
	}
	for _, s := range n.sections() {
		if len(s.items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", s.title)
		for _, it := range s.items {
			fmt.Fprintf(&b, "- %s (%s)\n", strings.TrimSpace(it.Text), prLinks(it.PRs))
		}
		b.WriteString("\n")
	}

	b.WriteString("<details>\n<summary>All changes</summary>\n\n")
	if len(c.Changes) == 0 {
		b.WriteString("No change touches this component.\n")
	}
	for _, ch := range c.Changes {
		if ch.PR != 0 {
			fmt.Fprintf(&b, "- %s (%s) by @%s\n", escape(ch.Subject), prLink(ch.PR), login(ch.Author))
		} else {
			fmt.Fprintf(&b, "- %s ([`%s`](%s/commit/%s))\n", escape(ch.Subject), ch.Commit[:min(7, len(ch.Commit))], base, ch.Commit)
		}
	}
	b.WriteString("\n</details>\n")

	var newcomers []contributor
	for _, nc := range c.NewContributors {
		if !slices.Contains(maintainers, nc.Login) {
			newcomers = append(newcomers, nc)
		}
	}
	if len(newcomers) > 0 {
		b.WriteString("\n## New contributors\n\n")
		for _, nc := range newcomers {
			fmt.Fprintf(&b, "- @%s made their first contribution in %s\n", login(nc.Login), prLink(nc.PR))
		}
	}

	if c.Previous != "" {
		fmt.Fprintf(&b, "\n**Full changelog**: [%s...%s](%s/compare/%s...%s)\n", c.Previous, c.Tag, base, c.Previous, c.Tag)
	} else {
		fmt.Fprintf(&b, "\n**Full changelog**: [%s](%s/commits/%s)\n", c.Tag, base, c.Tag)
	}
	// Only release-cli.yml attaches downloads to a release.
	if strings.HasPrefix(c.Tag, "cli/") {
		b.WriteString("\nEach download's SHA-256 is in `SHA256SUMS`, signed in `SHA256SUMS.asc`.\n")
	}
	return b.String()
}

// maintainers are the GitHub logins the notes never call out by name. They
// still count among thanks' contributors.
var maintainers = []string{"amorey"}

// thanks counts the range's pull requests and the people who wrote them, or
// is "" when no person wrote one. A bot is no contributor.
func thanks(c collection) string {
	prs := 0
	people := map[string]bool{}
	for _, ch := range c.Changes {
		if ch.PR == 0 {
			continue
		}
		prs++
		if !ch.Bot && ch.Author != "" {
			people[ch.Author] = true
		}
	}
	if len(people) == 0 {
		return ""
	}
	return fmt.Sprintf("This release has changes from **%s** over **%s**. Thank you to everyone who helped make it.",
		count(len(people), "contributor"), count(prs, "pull request"))
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// login drops the app/ prefix gh gives a GitHub App's login.
func login(s string) string {
	return strings.TrimPrefix(s, "app/")
}

var markdown = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`)

// escape keeps a commit subject from being read as Markdown, leaving its
// code spans as they are. The text after an unpaired backtick is no code
// span, so it is escaped too.
func escape(s string) string {
	parts := strings.Split(s, "`")
	for i := range parts {
		if i%2 == 0 || i == len(parts)-1 {
			parts[i] = markdown.Replace(parts[i])
		}
	}
	return strings.Join(parts, "`")
}

func (t tool) renderCmd(ctx context.Context, path string, out io.Writer) error {
	n, c, err := t.checked(ctx, path)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, render(n, c))
	return err
}

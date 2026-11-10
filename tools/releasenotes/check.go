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
	"regexp"
	"strings"
)

// check lists what is wrong with the notes against their range: a citation
// outside it, a feat, fix or ! pull request neither cited nor omitted with a
// reason, and prose that is empty, spans lines or builds its own links. A
// paragraph, title or bullet is one line; a body holds several paragraphs.
func check(n notes, c collection) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	inRange := map[int]bool{}
	for _, ch := range c.Changes {
		if ch.PR != 0 {
			inRange[ch.PR] = true
		}
	}

	text := func(where, s string) {
		if p := textProblem(s); p != "" {
			add("%s %s", where, p)
		}
	}
	paragraphs := func(where string, ps []string) {
		if len(ps) == 0 {
			add("%s has no paragraph", where)
		}
		for i, p := range ps {
			text(fmt.Sprintf("%s[%d]", where, i), p)
		}
	}
	cited := map[int]bool{}
	cite := func(where string, prs []int) {
		if len(prs) == 0 {
			add("%s cites no pull request", where)
		}
		for _, pr := range prs {
			if !inRange[pr] {
				add("%s cites #%d, which is not in %s", where, pr, c.Range)
			}
			cited[pr] = true
		}
	}

	paragraphs("summary", n.Summary)
	for i, h := range n.Highlights {
		where := fmt.Sprintf("highlights[%d]", i)
		text(where+".title", h.Title)
		paragraphs(where+".body", h.Body)
		cite(where, h.PRs)
	}
	for _, s := range n.sections() {
		for i, it := range s.items {
			where := fmt.Sprintf("%s[%d]", s.key, i)
			text(where, it.Text)
			cite(where, it.PRs)
		}
	}

	omitted := map[int]bool{}
	for i, o := range n.Omitted {
		where := fmt.Sprintf("omitted[%d]", i)
		switch {
		case !inRange[o.PR]:
			add("%s names #%d, which is not in %s", where, o.PR, c.Range)
		case cited[o.PR]:
			add("%s names #%d, which the notes cite", where, o.PR)
		case strings.TrimSpace(o.Reason) == "":
			add("%s gives no reason for #%d", where, o.PR)
		}
		omitted[o.PR] = true
	}

	for _, ch := range c.Changes {
		if ch.MustCite && !cited[ch.PR] && !omitted[ch.PR] {
			add("#%d (%s) is neither cited nor omitted with a reason", ch.PR, ch.Subject)
		}
	}
	return problems
}

var (
	codeSpan = regexp.MustCompile("`[^`]*`")
	issueRef = regexp.MustCompile(`#\d`)
)

// textProblem says what keeps s out of the notes, or "" when nothing does.
// Outside code spans the prose holds no link, HTML, mention or #N: render
// builds every link from the citations.
func textProblem(s string) string {
	if strings.TrimSpace(s) == "" {
		return "is empty"
	}
	if strings.ContainsAny(s, "\r\n") {
		return "spans lines; write one paragraph"
	}
	plain := codeSpan.ReplaceAllString(s, "")
	for _, bad := range []string{"://", "www.", "](", "]:", "<", "@"} {
		if strings.Contains(plain, bad) {
			return fmt.Sprintf("contains %q; render builds every link, and the notes hold no HTML or mention", bad)
		}
	}
	if issueRef.MatchString(plain) {
		return "names a pull request as #N; cite it in prs instead"
	}
	return ""
}

// checked reads the notes and collects their range, failing on any problem.
func (t tool) checked(ctx context.Context, path string) (notes, collection, error) {
	n, err := readNotes(path)
	if err != nil {
		return notes{}, collection{}, err
	}
	component, _, _ := strings.Cut(n.Tag, "/")
	c, err := t.collect(ctx, component, n.Tag)
	if err != nil {
		return notes{}, collection{}, err
	}
	if problems := check(n, c); len(problems) > 0 {
		return notes{}, collection{}, fmt.Errorf("%s fails the check:\n  - %s", path, strings.Join(problems, "\n  - "))
	}
	return n, c, nil
}

func (t tool) checkCmd(ctx context.Context, path string, out io.Writer) error {
	_, c, err := t.checked(ctx, path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s passes against %s\n", path, c.Range)
	return err
}

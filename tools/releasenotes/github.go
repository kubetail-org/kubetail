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
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// gh runs the gh CLI against the repository, with the releaser's credentials,
// and decodes its JSON output into v unless v is nil.
func (t tool) gh(ctx context.Context, v any, args ...string) error {
	out, err := t.run(ctx, "gh", append(args, "--repo", t.repo)...)
	if err != nil || v == nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

type pullRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
		IsBot bool   `json:"is_bot"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (t tool) pullRequest(ctx context.Context, n int) (pullRequest, error) {
	var pr pullRequest
	err := t.gh(ctx, &pr, "pr", "view", strconv.Itoa(n), "--json", "title,body,labels,author")
	return pr, err
}

// mergedBefore reports whether login had a pull request merged before ref's
// commit.
func (t tool) mergedBefore(ctx context.Context, login, ref string) (bool, error) {
	out, err := t.git(ctx, "log", "-1", "--format=%cI", ref)
	if err != nil {
		return false, err
	}
	at, err := time.Parse(time.RFC3339, out)
	if err != nil {
		return false, err
	}
	var prs []struct{}
	err = t.gh(ctx, &prs, "pr", "list", "--author", login, "--state", "merged",
		"--search", "merged:<"+at.UTC().Format(time.RFC3339), "--limit", "1", "--json", "number")
	return len(prs) > 0, err
}

// maxText bounds each piece of pull request text collect passes on.
const maxText = 4000

func clip(s string) string {
	if len(s) <= maxText {
		return s
	}
	return strings.ToValidUTF8(s[:maxText], "") + " […]"
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// sections splits a pull request body at its "## " headings, keyed by the
// heading in lower case. The template's comments are dropped.
func sections(body string) map[string]string {
	body = htmlComment.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), "")
	out := map[string]string{}
	for _, part := range strings.Split("\n"+body, "\n## ")[1:] {
		heading, text, _ := strings.Cut(part, "\n")
		out[strings.ToLower(strings.TrimSpace(heading))] = clip(strings.TrimSpace(text))
	}
	return out
}

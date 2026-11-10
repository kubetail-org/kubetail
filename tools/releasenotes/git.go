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
	"regexp"
	"strconv"
	"strings"
)

func (t tool) git(ctx context.Context, args ...string) (string, error) {
	out, err := t.run(ctx, "git", append([]string{"-C", t.dir}, args...)...)
	return strings.TrimSpace(string(out)), err
}

// log lists the range's commits that touch paths, newest first.
func (t tool) log(ctx context.Context, rng string, paths []string) ([]change, error) {
	out, err := t.git(ctx, append([]string{"log", "--format=%H %s", rng, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	var changes []change
	for line := range strings.Lines(out) {
		if sha, subject, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			changes = append(changes, parseCommit(sha, subject))
		}
	}
	return changes, nil
}

var (
	prSuffix     = regexp.MustCompile(`\s*\(#(\d+)\)$`)
	conventional = regexp.MustCompile(`^([a-z]+)(?:\(([^)]*)\))?(!)?: `)
)

// parseCommit reads a subject as a conventional commit's. A squash merge's
// subject ends in its pull request, "(#NNN)".
func parseCommit(sha, subject string) change {
	c := change{Commit: sha, Subject: subject}
	if m := prSuffix.FindStringSubmatch(subject); m != nil {
		c.PR, _ = strconv.Atoi(m[1])
		c.Subject = strings.TrimSuffix(subject, m[0])
	}
	if m := conventional.FindStringSubmatch(c.Subject); m != nil {
		c.Type, c.Scope, c.Breaking = m[1], m[2], m[3] == "!"
	}
	return c
}

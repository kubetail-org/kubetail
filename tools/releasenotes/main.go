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

// Command releasenotes does the deterministic half of a release's notes. The
// release-notes skill (.claude/skills/release-notes/SKILL.md) writes the
// prose; this program collects its input, checks its output and renders it.
// Run it from tools/: go run ./releasenotes <command> ....
//
// Pull request text is attacker-controlled. Only collect reads it, only render
// builds links, and only publish writes to GitHub, after a person answers on
// the terminal.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const usage = `usage: releasenotes <command> <args>

  collect <component> <tag>  print the tag's range and its pull requests as JSON
  check <notes.json>         fail on a citation outside the range or a dropped feat, fix or !
  render <notes.json>        print the release's Markdown
  publish <tag> <notes.md>   ask on the terminal, then write the notes to the release

A component is cli, dashboard, cluster-api or cluster-agent.`

// A tool runs the commands against one checkout and one GitHub repository.
type tool struct {
	dir  string // a directory inside the git checkout
	repo string // owner/name
	// run runs a program and returns its standard output.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// terminal opens the controlling terminal, never standard input.
	terminal func() (io.ReadCloser, error)
}

func main() {
	t := tool{dir: ".", repo: "kubetail-org/kubetail", run: execRun, terminal: openTerminal}
	if err := t.command(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes:", err)
		os.Exit(1)
	}
}

func (t tool) command(ctx context.Context, args []string, out io.Writer) error {
	switch {
	case len(args) == 3 && args[0] == "collect":
		return t.collectCmd(ctx, args[1], args[2], out)
	case len(args) == 2 && args[0] == "check":
		return t.checkCmd(ctx, args[1], out)
	case len(args) == 2 && args[0] == "render":
		return t.renderCmd(ctx, args[1], out)
	case len(args) == 3 && args[0] == "publish":
		return t.publishCmd(ctx, args[1], args[2], out)
	}
	return errors.New(usage)
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

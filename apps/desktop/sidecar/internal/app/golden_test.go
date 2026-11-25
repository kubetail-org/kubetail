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

package app

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/chat"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/loop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// promptMachines is each shell the app offers, as a machine of that platform
// describes it, and a machine with none.
var promptMachines = []struct {
	name  string
	shell *bash.Tool
}{
	{"macos", bash.Described("darwin", "bash", "3.2", true)},
	{"linux", bash.Described("linux", "bash", "5.2", true)},
	{"windows", bash.Described("windows", "bash", "5.2", false)},
	{"no_shell", nil},
}

// What the model reads on a turn is one document per kind of run and machine,
// under testdata/prompt: the system prompt as the loop assembles it over the
// app's own box, then each tool's offer. The files are the review of a change
// to any prompt, offer or assembly: `make generate-prompts` rewrites them, and the diff
// is the model's view of the change. The target is Anthropic's, the one
// provider offered a tool of its own.
func TestWhatTheModelReadsMatchesItsGolden(t *testing.T) {
	cat := everyCatalog(t)
	target := anthropicTarget(t, cat)
	names := cat.ToolsFor(target)

	for _, m := range promptMachines {
		box := promptBox(t, m.shell).For(target, names)
		golden(t, "chat_"+m.name+".md", renderTurn(t, chat.SystemPrompt(), box, chat.MaxToolCalls))
	}
	linux := promptBox(t, promptMachines[1].shell).For(target, names)
	golden(t, "subagent_linux.md", renderTurn(t, chat.SubagentSystemPrompt(), chat.SubagentBox(linux), chat.MaxSubagentToolCalls))
	golden(t, "monitor_linux.md", renderTurn(t, chat.MonitorSystemPrompt(), chat.MonitorBox(linux), chat.MaxMonitorToolCalls))
}

// anthropicTarget is the catalog's first Anthropic model.
func anthropicTarget(t *testing.T, cat catalog.Catalog) llm.Target {
	t.Helper()
	for _, p := range cat.Providers() {
		if p.ID == "anthropic" {
			return llm.Target{Provider: p, Model: p.Catalog[0]}
		}
	}
	t.Fatal("the catalog has no anthropic provider")
	return llm.Target{}
}

// promptBox is the app's box over shell, on a clock fixed where the search's
// section names the month.
func promptBox(t *testing.T, shell *bash.Tool) tools.Box {
	t.Helper()
	march := func() time.Time { return time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC) }
	box, err := chatTools(toolDeps{shell: shell, fenced: []string{t.TempDir()}, umask: 0o022, now: march})
	require.NoError(t, err)
	return box
}

// renderTurn is the document: the system prompt, byte for byte, then each
// tool's offer in box order. An offer is trimmed and its schema indented to be
// read; the wire sends them as the files spell them.
func renderTurn(t *testing.T, prompt string, box tools.Box, budget int) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("==== system prompt ====\n\n")
	b.WriteString(loop.SystemPrompt(loop.Turn{SystemPrompt: prompt, Tools: box, MaxToolCalls: budget}))
	b.WriteString("\n")
	defs, native := box.Offer()
	for _, d := range defs {
		var schema bytes.Buffer
		require.NoError(t, json.Indent(&schema, d.InputSchema, "", "  "), d.Name)
		fmt.Fprintf(&b, "\n==== tool %s ====\n\n%s\n\n%s\n", d.Name, strings.TrimSpace(d.Description), schema.String())
	}
	for _, o := range native {
		fmt.Fprintf(&b, "\n==== tool %s ====\n\n", o.Tool.Name())
		if o.Server {
			fmt.Fprintf(&b, "The provider's, run on its side, at most %d calls a turn.\n", o.MaxUses)
		} else {
			b.WriteString("The provider's, run by the sidecar.\n")
		}
	}
	return b.String()
}

// golden compares got to the named file under testdata/prompt, or rewrites the
// file under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompt", name)
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run `make generate-prompts` to write it")
	assert.Equal(t, string(want), got)
}

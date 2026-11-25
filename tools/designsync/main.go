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

// Command designsync does the bookkeeping that keeps the Figma design and the
// desktop webview's code in step (apps/desktop/design/CLAUDE.md). It reads the
// manifest, apps/desktop/design/components.json, and, when FIGMA_TOKEN is set,
// the Desktop file over Figma's REST API. It never writes to Figma.
// Run it from tools/: go run ./designsync <command> ....
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const usage = `usage: designsync <command> <args>

  check           fail on an entry that is malformed or names a missing file or symbol
  status          list the components and screens whose code or design moved since their last sync
  record <id>...  set each entry's fingerprints to the working tree's and, with FIGMA_TOKEN, Figma's
  format          rewrite the manifest in the tool's format

status and record read Figma when FIGMA_TOKEN holds a personal access token that can read file content`

// A tool runs the commands against one checkout.
type tool struct {
	root string // the desktop app's root, apps/desktop/
	// design reads Figma's components and screens. It is nil when there is no
	// token, and the commands then read the code side alone.
	design func() ([]stamped, error)
}

// readDesign reads Figma, or nothing when there is no token.
func (t tool) readDesign() ([]stamped, error) {
	if t.design == nil {
		return nil, nil
	}
	return t.design()
}

func main() {
	t := tool{root: "../apps/desktop"}
	if token := os.Getenv("FIGMA_TOKEN"); token != "" {
		// The file's two pages are about 25 MB.
		client := &http.Client{Timeout: 2 * time.Minute}
		t.design = figmaFile{api: figmaAPI, key: desktopFile, token: token, client: client}.read
	}
	if err := t.command(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "designsync:", err)
		os.Exit(1)
	}
}

func (t tool) command(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	rest := args[1:]
	switch {
	case args[0] == "check" && len(rest) == 0:
		return t.checkCmd(out)
	case args[0] == "status" && len(rest) == 0:
		return t.statusCmd(out)
	case args[0] == "record" && len(rest) > 0:
		return t.recordCmd(rest, out)
	case args[0] == "format" && len(args) == 1:
		return t.formatCmd()
	}
	return errors.New(usage)
}

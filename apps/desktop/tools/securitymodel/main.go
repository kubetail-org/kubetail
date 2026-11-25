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

// Command securitymodel checks the security model, docs/security-model.md and
// the subject files under docs/security-model/, against the code it cites, and
// writes the two parts of it that are derived: the index of subjects and the
// register of what is not protected.
// Run it from tools/: go run ./securitymodel <command>.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const usage = `usage: securitymodel <command>

  check   fail on a row that is malformed, cites a test or file that is not there, or lacks
          what its status needs, and on an index or register that is out of date
  write   rewrite the index in docs/security-model.md and docs/security-model/not-protected.md`

// A tool runs the commands against one checkout.
type tool struct {
	root string // the repository's root
}

func main() {
	t := tool{root: ".."}
	if err := t.command(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "securitymodel:", err)
		os.Exit(1)
	}
}

func (t tool) command(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	switch args[0] {
	case "check":
		return t.checkCmd(out)
	case "write":
		return t.writeCmd()
	}
	return errors.New(usage)
}

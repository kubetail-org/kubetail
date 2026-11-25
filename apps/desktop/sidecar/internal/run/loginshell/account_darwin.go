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

package loginshell

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"os/user"
	"strings"
)

// lookupCommand reads the account record. A variable so a test can stand in
// a fake.
var lookupCommand = "/usr/bin/dscl"

// DefaultPath is the PATH a login shell starts from here, before its
// startup files run.
const DefaultPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// defaultShells is macOS's own login shell.
var defaultShells = []string{"/bin/zsh"}

// recordShell is the UserShell dscl reads, "" when it reads none. dscl
// answers for a network account as well as a local one.
func recordShell(ctx context.Context) string {
	u, err := user.Current()
	if err != nil { // coverage:ignore user.Current caches the process's own user, which no test can take away
		return ""
	}
	out, err := exec.CommandContext(ctx, lookupCommand, "/Search", "-read", "/Users/"+u.Username, "UserShell").Output()
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if shell, ok := strings.CutPrefix(scanner.Text(), "UserShell:"); ok {
			return strings.TrimSpace(shell)
		}
	}
	return ""
}

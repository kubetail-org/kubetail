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

//go:build unix && !darwin

package loginshell

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// lookupCommand reads the account record, and passwdFile is read when it
// cannot. Variables so a test can stand in fakes.
var (
	lookupCommand = "/usr/bin/getent"
	passwdFile    = "/etc/passwd"
)

// DefaultPath is the PATH a login shell starts from here, before its
// startup files run.
const DefaultPath = "/usr/local/bin:/usr/bin:/bin"

// defaultShells is the platform's own login shells, in order.
var defaultShells = []string{"/bin/bash", "/bin/sh"}

// recordShell is the shell field of the user's passwd entry, "" when there is
// none. getent answers for a directory account (LDAP, SSSD) too, so it is
// asked first, and the file is read when getent cannot answer.
func recordShell(ctx context.Context) string {
	u, err := user.Current()
	if err != nil { // coverage:ignore user.Current caches the process's own user, which no test can take away
		return ""
	}
	if out, err := exec.CommandContext(ctx, lookupCommand, "passwd", u.Username).Output(); err == nil {
		return passwdShell(out, u.Username)
	}
	out, err := os.ReadFile(passwdFile)
	if err != nil {
		return ""
	}
	return passwdShell(out, u.Username)
}

// passwdShell is the seventh field of name's line in passwd.
func passwdShell(passwd []byte, name string) string {
	scanner := bufio.NewScanner(bytes.NewReader(passwd))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) == 7 && fields[0] == name {
			return fields[6]
		}
	}
	return ""
}

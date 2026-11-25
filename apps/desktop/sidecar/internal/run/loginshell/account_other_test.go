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
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// useAccountShell points the lookup at a fake getent that answers shell for
// the current user, and for no one else.
func useAccountShell(t *testing.T, shell string) {
	t.Helper()
	u, err := user.Current()
	require.NoError(t, err)
	script := writeScript(t, "getent", "#!/bin/sh\n"+
		"[ \"$1 $2\" = \"passwd "+u.Username+"\" ] || exit 2\n"+
		"echo '"+u.Username+":x:"+u.Uid+":"+u.Gid+"::/home/x:"+shell+"'\n")
	defer func(cmd string) { t.Cleanup(func() { lookupCommand = cmd }) }(lookupCommand)
	lookupCommand = script
}

// getent answers for a directory account /etc/passwd does not list, so it is
// asked before the file.
func TestAccountShellAsksGetent(t *testing.T) {
	shell := writeScript(t, "fish", "#!/bin/sh\nexit 0\n")
	useAccountShell(t, shell)

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, shell, got)
}

// With no getent the user's line in the passwd file answers.
func TestAccountShellReadsThePasswdFileWithoutGetent(t *testing.T) {
	u, err := user.Current()
	require.NoError(t, err)
	shell := writeScript(t, "fish", "#!/bin/sh\nexit 0\n")
	passwd := writeScript(t, "passwd", "other:x:1:1::/home/other:/bin/false\n"+
		u.Username+":x:"+u.Uid+":"+u.Gid+"::/home/x:"+shell+"\n")
	defer func(cmd, file string) { lookupCommand, passwdFile = cmd, file }(lookupCommand, passwdFile)
	lookupCommand, passwdFile = filepath.Join(t.TempDir(), "getent"), passwd

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, shell, got)
}

// A passwd file with no line for the user names no shell, so the platform's
// own is taken.
func TestAccountShellFallsBackWhenThePasswdFileLacksTheUser(t *testing.T) {
	passwd := writeScript(t, "passwd", "other:x:1:1::/home/other:/bin/false\n")
	fallback := writeScript(t, "sh", "#!/bin/sh\nexit 0\n")
	defer func(cmd, file string, shells []string) {
		lookupCommand, passwdFile, defaultShells = cmd, file, shells
	}(lookupCommand, passwdFile, defaultShells)
	lookupCommand, passwdFile, defaultShells = filepath.Join(t.TempDir(), "getent"), passwd, []string{fallback}

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, fallback, got)
}

// With neither getent nor a passwd file there is no record, so the platform's
// own shell is taken.
func TestAccountShellFallsBackWithoutAPasswdFile(t *testing.T) {
	fallback := writeScript(t, "sh", "#!/bin/sh\nexit 0\n")
	defer func(cmd, file string, shells []string) {
		lookupCommand, passwdFile, defaultShells = cmd, file, shells
	}(lookupCommand, passwdFile, defaultShells)
	lookupCommand, passwdFile, defaultShells = filepath.Join(t.TempDir(), "getent"), filepath.Join(t.TempDir(), "passwd"), []string{fallback}

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, fallback, got)
}

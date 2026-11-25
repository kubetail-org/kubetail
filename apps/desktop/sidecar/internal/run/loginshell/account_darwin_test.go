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
	"os/user"
	"testing"

	"github.com/stretchr/testify/require"
)

// useAccountShell points the lookup at a fake dscl that answers shell for the
// current user, and for no one else.
func useAccountShell(t *testing.T, shell string) {
	t.Helper()
	u, err := user.Current()
	require.NoError(t, err)
	useDscl(t, "[ \"$1 $2 $3 $4\" = \"/Search -read /Users/"+u.Username+" UserShell\" ] || exit 56\n"+
		"echo 'UserShell: "+shell+"'\n")
}

// useDscl points the lookup at a fake dscl running body.
func useDscl(t *testing.T, body string) {
	t.Helper()
	script := writeScript(t, "dscl", "#!/bin/sh\n"+body)
	defer func(cmd string) { t.Cleanup(func() { lookupCommand = cmd }) }(lookupCommand)
	lookupCommand = script
}

// dscl answers for a network account as well as a local one, so it is asked.
func TestAccountShellAsksDscl(t *testing.T) {
	shell := writeScript(t, "fish", "#!/bin/sh\nexit 0\n")
	useAccountShell(t, shell)

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, shell, got)
}

// A dscl that fails, or reads no UserShell, is no record.
func TestRecordShellIsEmptyWithoutARecord(t *testing.T) {
	for name, body := range map[string]string{
		"fails":        "exit 56\n",
		"no UserShell": "echo 'NFSHomeDirectory: /Users/x'\n",
	} {
		useDscl(t, body)
		require.Empty(t, recordShell(t.Context()), name)
	}
}

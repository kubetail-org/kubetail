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

package sandbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWindowsHasNoSandbox(t *testing.T) {
	s, v, err := Probe(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, s)
	assert.Equal(t, State{Available: false, Reason: "no sandbox on native Windows; run Kubetail in WSL2"}, v)

	var none *Sandbox
	assert.False(t, none.Confines())
	assert.False(t, none.NeedsResolver())
	assert.Nil(t, none.Never(`C:\Users\x`))
	assert.Equal(t, System{}, none.System(`C:\Users\x`, "bash.exe"))
	_, err = none.Port()
	assert.ErrorIs(t, err, errNone)
	_, err = none.CountedProcesses(false)
	assert.ErrorIs(t, err, errNone)
	network, reason := none.NetworkStatus()
	assert.False(t, network)
	assert.Equal(t, errNone.Error(), reason)
}

// Command answers errNone and no command, so nothing runs unconfined.
func TestCommandAnswersErrNone(t *testing.T) {
	var none *Sandbox

	cmd, err := none.Command(context.Background(), Run{Shell: `C:\Windows\System32\cmd.exe`, Args: []string{"/c", "exit"}})

	assert.Nil(t, cmd)
	assert.ErrorIs(t, err, errNone)
}

func TestFixedMountIsNothing(t *testing.T) {
	assert.False(t, FixedMount(`C:\`))
}

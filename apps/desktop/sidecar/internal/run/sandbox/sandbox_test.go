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
	"testing"

	"github.com/stretchr/testify/assert"
)

// sandbox-init, sandbox-shell or sandbox-pasta as the first argument runs that part of a run,
// whose own arguments come after it; anything else is the caller's.
func TestMainRunsTheSubcommandItNames(t *testing.T) {
	for _, command := range []string{InitCommand, ShellCommand, PastaCommand} {
		code, ok := Main([]string{"kubetail-sidecar", command})
		assert.True(t, ok, command)
		assert.Equal(t, initFailed, code, command)
	}
	for _, argv := range [][]string{
		{"kubetail-sidecar"}, {"kubetail-sidecar", "--socket", "s"}, {"kubetail-sidecar", "--kubeconfig", InitCommand},
	} {
		_, ok := Main(argv)
		assert.False(t, ok, argv)
	}
}

// A run holding a variable no run may hold fails its check, whatever its
// policy; one holding none passes as its policy does.
func TestARunsCheckRefusesAnUnpassableVariable(t *testing.T) {
	for _, kv := range []string{
		"LD_PRELOAD=/x.so", "AWS_SESSION_TOKEN=t", "AZURE_STORAGE_KEY=k", "ARM_CLIENT_SECRET=s", "MSI_SECRET=s",
	} {
		r := Run{Env: []string{"PATH=/usr/bin", kv}}
		assert.ErrorContains(t, checked(r), "may not pass", kv)
	}
	assert.NoError(t, checked(Run{Env: []string{"PATH=/usr/bin", "HOME=/w", "AZURE_CONFIG_DIR=/t/azure"}}))
}

// A clone shares no slice with its run, so changing the run leaves it be.
func TestACloneSharesNoSliceWithItsRun(t *testing.T) {
	full := func() Run {
		return Run{
			Args: []string{"-c"}, Env: []string{"A=1"},
			Policy: Policy{
				Files:   FilePolicy{Read: []string{"/r"}, Write: []string{"/w"}, Deny: []string{"/d"}},
				Always:  AlwaysPolicy{Deny: []string{"/ad"}, Kubetail: []string{"/k"}, Read: []string{"/ar"}, Write: []string{"/aw"}},
				Network: NetworkPolicy{Relays: []Relay{{Port: 1}}},
			},
		}
	}
	r := full()

	c := r.clone()
	r.Args[0], r.Env[0] = "x", "x"
	f, a := r.Policy.Files, r.Policy.Always
	f.Read[0], f.Write[0], f.Deny[0] = "x", "x", "x"
	a.Deny[0], a.Kubetail[0], a.Read[0], a.Write[0] = "x", "x", "x", "x"
	r.Policy.Network.Relays[0].Port = 2

	assert.Equal(t, full(), c)
}

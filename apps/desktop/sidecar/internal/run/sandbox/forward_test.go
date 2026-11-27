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
	"github.com/stretchr/testify/require"
)

func TestInitArgsAreTheSocketThePortAndTheCommand(t *testing.T) {
	t.Parallel()

	got, err := parseInitArgs([]string{"--socket", "/run/p.sock", "--port", "6443", "--", "/bin/sh", "-c", "--port 1"})
	require.NoError(t, err)
	assert.Equal(t, initArgs{socket: "/run/p.sock", port: 6443, lifeline: -1, argv: []string{"/bin/sh", "-c", "--port 1"}}, got)

	got, err = parseInitArgs([]string{"--port=1", "--socket=s", "--", "x"})
	require.NoError(t, err)
	assert.Equal(t, initArgs{socket: "s", port: 1, lifeline: -1, argv: []string{"x"}}, got)
}

// Under sandbox-pasta the forwarder takes the run's stderr from stdin.
func TestInitArgsTakeTheStderrOnStdin(t *testing.T) {
	t.Parallel()

	got, err := parseInitArgs([]string{stderrOnStdinFlag, "--", "/bin/sh"})
	require.NoError(t, err)
	assert.Equal(t, initArgs{stderrOnStdin: true, lifeline: -1, argv: []string{"/bin/sh"}}, got)
}

// A run with no cluster names neither flag, and its forwarder listens on
// nothing.
func TestInitArgsTakeNeitherFlag(t *testing.T) {
	t.Parallel()

	got, err := parseInitArgs([]string{"--", "/bin/sh", "-c", "true"})
	require.NoError(t, err)
	assert.Equal(t, initArgs{lifeline: -1, argv: []string{"/bin/sh", "-c", "true"}}, got)
}

// A run the sidecar's lifeline reaches names its descriptor.
func TestInitArgsTakeTheLifeline(t *testing.T) {
	t.Parallel()

	got, err := parseInitArgs([]string{lifelineFlag, "3", "--", "/bin/sh"})
	require.NoError(t, err)
	assert.Equal(t, initArgs{lifeline: 3, argv: []string{"/bin/sh"}}, got)
}

func TestInitArgsRefuseWhatIsMissing(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"no socket":           {"--port", "1", "--", "x"},
		"no port":             {"--socket", "s", "--", "x"},
		"neither, no command": {"--"},
		"port zero":           {"--socket", "s", "--port", "0", "--", "x"},
		"port too big":        {"--socket", "s", "--port", "65536", "--", "x"},
		"no command":          {"--socket", "s", "--port", "1", "--"},
		"an unknown":          {"--socket", "s", "--port", "1", "--other", "--", "x"},
		"lifeline on stderr":  {lifelineFlag, "2", "--", "x"},
		"lifeline not a fd":   {lifelineFlag, "x", "--", "x"},
	} {
		_, err := parseInitArgs(args)
		assert.Error(t, err, name)
	}
}

// The forwarder's command line parses back to the run it was written for,
// with or without a socket.
func TestForwarderArgsRoundTrip(t *testing.T) {
	t.Parallel()

	relay := Relay{Port: 6443, Socket: "/run/p.sock"}
	args := ForwarderArgs(relay)
	require.Equal(t, InitCommand, args[0])
	got, err := parseInitArgs(append(args[1:], "/bin/sh", "-c", "--port 1"))
	require.NoError(t, err)
	assert.Equal(t, initArgs{socket: relay.Socket, port: 6443, lifeline: -1, argv: []string{"/bin/sh", "-c", "--port 1"}}, got)

	got, err = parseInitArgs(append(ForwarderArgs(Relay{})[1:], "/bin/sh"))
	require.NoError(t, err)
	assert.Equal(t, initArgs{lifeline: -1, argv: []string{"/bin/sh"}}, got)
}

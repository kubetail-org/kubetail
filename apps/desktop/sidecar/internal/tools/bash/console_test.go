// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package bash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// The console runs a command of one line or many, refusing one that is blank,
// too long, not UTF-8 or holding a control character but a tab or a newline.
func TestCheckCommand(t *testing.T) {
	for _, ok := range []string{"ls ~", "echo a\techo b", "for x in 1 2\ndo echo $x\ndone", strings.Repeat("x", MaxConsoleCommand)} {
		assert.NoError(t, checkCommand(ok), ok)
	}
	for bad, why := range map[string]string{
		"":                                       "Type a command to run.",
		" \n\t":                                  "Type a command to run.",
		strings.Repeat("x", MaxConsoleCommand+1): "The command is too long to run.",
		"echo \xff":                              "The command holds a character that cannot be run.",
		"echo \x1b[2J":                           "The command holds a character that cannot be run.",
		"echo a\rb":                              "The command holds a character that cannot be run.",
	} {
		err := checkCommand(bad)
		var refusal *ConsoleRefusal
		require.ErrorAs(t, err, &refusal, bad)
		assert.Equal(t, why, refusal.Error())
	}
}

// Output a line may still continue is sent up to its last newline, which
// never splits a character.
func TestWholeLinesEndAtTheLastNewline(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "",
		"abc":                  "",
		"a\n":                  "a\n",
		"a\nb":                 "a\n",
		"a\nb\n\u20ac\xe2\x82": "a\nb\n",
	} {
		assert.Equal(t, want, string(wholeLines([]byte(in))), "%q", in)
	}
}

// The console runs nothing on a machine whose sandbox does not confine a
// command, or that has none: nothing it runs leaves the sandbox.
func TestTheConsoleRunsNothingOutsideTheSandbox(t *testing.T) {
	tl := tool(t)
	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: "echo hi"})
	assert.ErrorIs(t, err, ErrNoConsole)

	fake := &fakeSandboxer{}
	tl.sandboxer = fake
	_, err = tl.StartConsoleCommand(ConsoleRequest{Command: "echo hi"})
	assert.ErrorIs(t, err, ErrNoConsole)
	assert.Empty(t, fake.seen())
}

// A command asking for the internet where the machine has none is refused,
// saying why, and so is a command checkCommand refuses.
func TestTheConsoleRefusesWhatItCannotRun(t *testing.T) {
	tl := tool(t)
	fake := &fakeSandboxer{confines: true, networkReason: "pasta is missing"}
	tl.sandboxer = fake

	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: "curl -sI https://example.com", Network: true})
	var refusal *ConsoleRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, "This machine cannot give a sandboxed command the internet: pasta is missing.", refusal.Error())

	_, err = tl.StartConsoleCommand(ConsoleRequest{Command: " "})
	require.ErrorAs(t, err, &refusal)
	assert.Empty(t, fake.seen())
}

// Once the tool is closed, the console starts nothing.
func TestAClosedConsoleStartsNothing(t *testing.T) {
	tl := tool(t)
	tl.sandboxer = &fakeSandboxer{confines: true}
	require.NoError(t, tl.Close())

	_, err := tl.StartConsoleCommand(ConsoleRequest{Command: "echo hi"})
	assert.ErrorIs(t, err, ErrConsoleClosed)
}

// A frame holds back a line that may still continue: a running command's
// last, and one the cap cut, which counts as discarded. A command that ended
// within the cap is sent whole.
func TestPublishSendsWholeLines(t *testing.T) {
	for name, c := range map[string]struct {
		run       ConsoleRun
		out       string
		output    string
		discarded int
	}{
		"running":       {ConsoleRun{Running: true}, "a\nb", "a\n", 0},
		"ended":         {ConsoleRun{}, "a\nb", "a\nb", 0},
		"ended, capped": {ConsoleRun{Discarded: 5}, "a\nb", "a\n", 6},
	} {
		t.Run(name, func(t *testing.T) {
			con := newConsole()
			rx := con.hub.Receiver()
			defer rx.Close()
			testutil.Recv(t, rx.Chan(), "the first value")
			con.run, con.out = &c.run, []byte(c.out)

			con.publish()

			got := testutil.Recv(t, rx.Chan(), "the frame")
			assert.Equal(t, c.output, got.Output)
			assert.Equal(t, c.discarded, got.Discarded)
		})
	}
}

// A stop ends the command it names, or whichever runs for 0, and nothing
// else: a stop sent late for an older command never ends a newer one.
func TestAStopEndsOnlyTheCommandItNames(t *testing.T) {
	for number, stops := range map[int]bool{0: true, 3: true, 2: false, 4: false} {
		tl := &Tool{console: newConsole()}
		stopped := false
		tl.console.run = &ConsoleRun{Number: 3, Running: true}
		tl.console.cancel = func() { stopped = true }

		tl.StopConsoleCommand(number)

		assert.Equal(t, stops, stopped, "stop %d", number)
	}
}

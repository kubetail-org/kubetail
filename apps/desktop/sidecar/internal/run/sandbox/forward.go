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

// The forwarder, kubetail-sidecar sandbox-init: a sandboxed run's first process.
// It listens on loopback where the run's kubeconfig points, relays each
// connection to the run's socket, and runs the shell as its child. It reads
// nothing it relays and checks nothing: every check is the proxy's. Relaying
// and running the child are forward_unix.go; Windows has no sandbox and
// refuses in forward_windows.go.
package sandbox

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

// InitCommand is the subcommand that makes this executable a run's forwarder.
const InitCommand = "sandbox-init"

// ForwarderArgs is the forwarder's command line for relay r, after the
// executable: InitCommand, the socket and port where r has a socket, and the
// -- the child's command follows. The zero Relay relays nothing.
func ForwarderArgs(r Relay) []string {
	args := []string{InitCommand}
	if r.Socket != "" {
		args = append(args, "--socket", r.Socket, "--port", strconv.Itoa(r.Port))
	}
	return append(args, "--")
}

// initFailed is the forwarder's or the shell launcher's own failure, clear of
// the shell's 126 and 127. A command can exit 125 itself, so the subcommand's
// line on stderr is what tells the two apart.
const initFailed = 125

// initArgs is sandbox-init's command line.
type initArgs struct {
	socket string // the run's proxy socket, empty for a run with no cluster
	port   int    // the loopback port to listen on, beside socket
	// stderrOnStdin is set under sandbox-pasta, which passes the run's stderr
	// on stdin.
	stderrOnStdin bool
	lifeline      int      // the lifeline's descriptor, -1 for none
	argv          []string // the command to run as the child
}

// lifelineFlag names the descriptor the lifeline (Sandbox.lifeline) arrives
// on: lifelineFD, the first of exec.Cmd.ExtraFiles.
const (
	lifelineFlag = "--lifeline"
	lifelineFD   = 3
)

// stderrOnStdinFlag is the flag that sets initArgs.stderrOnStdin.
const stderrOnStdinFlag = "--stderr-on-stdin"

// runStarted is what sandbox-init writes on the stderr sandbox-pasta gave
// pasta once it holds the run's own: pasta's and bwrap's output before it is
// theirs, and the run has started.
const runStarted = "\x00sandbox-init: started\x00"

// parseInitArgs reads [--stderr-on-stdin] [--lifeline <fd>] [--socket <path>
// --port <n>] -- <argv…>, both of the last two flags or neither; flag stops at
// the --, so argv is never read as flags.
func parseInitArgs(args []string) (initArgs, error) {
	var a initArgs
	fs := flag.NewFlagSet(InitCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.socket, "socket", "", "")
	fs.IntVar(&a.port, "port", 0, "")
	fs.BoolVar(&a.stderrOnStdin, stderrOnStdinFlag[2:], false, "")
	lifelineVar(fs, &a.lifeline)
	if err := fs.Parse(args); err != nil {
		return initArgs{}, err
	}
	if err := checkLifeline(fs, a.lifeline); err != nil {
		return initArgs{}, err
	}
	a.argv = fs.Args()
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	forwards := set["socket"] || set["port"]
	switch {
	case forwards && a.socket == "":
		return initArgs{}, errors.New("no --socket")
	case forwards && (a.port < 1 || a.port > 65535):
		return initArgs{}, errors.New("no --port from 1 to 65535")
	case len(a.argv) == 0:
		return initArgs{}, errors.New("no command")
	}
	return a, nil
}

// lifelineVar binds --lifeline in fs to fd, -1 when absent.
func lifelineVar(fs *flag.FlagSet, fd *int) {
	fs.IntVar(fd, lifelineFlag[2:], -1, "")
}

// checkLifeline is --lifeline's check once fs parsed: given, it names a
// descriptor above the standard streams.
func checkLifeline(fs *flag.FlagSet, fd int) error {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == lifelineFlag[2:] })
	if given && fd < 3 {
		return errors.New("no --lifeline above the standard streams")
	}
	return nil
}

// count reads a limit: a non-negative decimal integer.
func count(v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err == nil && n < 0 {
		err = fmt.Errorf("negative: %d", n)
	}
	return n, err
}

// fail writes the subcommand's one line on stderr, which is the run's output,
// so the model reads why, and answers initFailed.
func fail(stderr io.Writer, command, what string, err error) int {
	fmt.Fprintf(stderr, "%s: %s: %v\n", command, what, err)
	return initFailed
}

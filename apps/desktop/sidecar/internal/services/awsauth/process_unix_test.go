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

//go:build unix

package awsauth

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// A failing helper's diagnostics, which here name a cache path, never reach
// the sidecar's stderr, which the host logs as is. The test points os.Stderr
// at a pipe, the stderr the SDK's own command would hand the helper, so it
// does not run in parallel.
func TestAHelpersStderrNeverReachesTheSidecars(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	s, _ := profile(t, "eu-west-1", true)
	_, err = s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)

	os.Stderr = stderr
	require.NoError(t, w.Close())
	leaked, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	assert.Empty(t, string(leaked))
}

// A role assumed from a source profile's credential_process is no login: the
// SDK runs that process with the sidecar's stderr, so it never runs.
func TestARoleChainedFromAProcessIsNoLogin(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "creds.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+ran+"\n"), 0o700))
	cfg := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(cfg, []byte("[profile kubetail-test]\nrole_arn = arn:aws:iam::123456789012:role/r\n"+
		"source_profile = src\nregion = eu-west-1\n[profile src]\ncredential_process = "+script+"\n"), 0o600))
	s := newService("", func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx, config.WithSharedConfigFiles([]string{cfg}),
			config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "none")}),
			config.WithSharedConfigProfile("kubetail-test"))
	})

	_, err := s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)
	assert.NoFileExists(t, ran)
}

// A role chain runs a process when its deepest source, whose credentials the
// SDK uses, names one and holds no keys; a process on a profile above it, or
// beside keys, never runs.
func TestRunsProcessReadsTheDeepestSource(t *testing.T) {
	keys := aws.Credentials{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "s"}
	assert.False(t, runsProcess(config.SharedConfig{}))
	assert.False(t, runsProcess(config.SharedConfig{RoleARN: "r", Source: &config.SharedConfig{}}))
	assert.True(t, runsProcess(config.SharedConfig{CredentialProcess: "p"}))
	assert.True(t, runsProcess(config.SharedConfig{RoleARN: "r", Source: &config.SharedConfig{
		RoleARN: "s", Source: &config.SharedConfig{CredentialProcess: "p"},
	}}))
	assert.False(t, runsProcess(config.SharedConfig{RoleARN: "r", CredentialProcess: "p", Source: &config.SharedConfig{Credentials: keys}}))
	assert.False(t, runsProcess(config.SharedConfig{RoleARN: "r", Source: &config.SharedConfig{CredentialProcess: "p", Credentials: keys}}))
}

// A helper leads a session of its own, so it has no controlling terminal to
// stop on.
func TestAHelperLeadsASessionOfItsOwn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cmd, err := quietCommand{line: "sleep 600", waitDelay: time.Second}.NewCommand(ctx)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	sid, err := unix.Getsid(cmd.Process.Pid)
	require.NoError(t, err)
	assert.Equal(t, cmd.Process.Pid, sid)
}

// runHelper starts line through c's command, its output piped as the SDK pipes
// it, and waits until the helper has written its child's pid to pidFile. It
// answers the cancel that kills the helper and a channel closed once it is
// waited for. The child is killed when the test ends.
func runHelper(t *testing.T, c quietCommand, pidFile string) (cancel func(), waited <-chan struct{}) {
	ctx, cancel := context.WithCancel(t.Context())
	cmd, err := c.NewCommand(ctx)
	require.NoError(t, err)
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(pidFile)
		return err == nil && strings.HasSuffix(string(b), "\n")
	}, testutil.Timeout, 10*time.Millisecond, "the helper's child")
	b, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return cancel, done
}

// A cancelled helper is killed with the children in its group, though one
// holds its output open: the wait for it ends with no delay to lean on.
func TestACancelledHelperIsKilledWithItsGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cancel, waited := runHelper(t, quietCommand{
		line:      "sleep 600 & echo $! > " + pidFile + "; wait",
		waitDelay: time.Hour,
	}, pidFile)
	cancel()
	testutil.Wait(t, waited, "the helper and its group")
}

// A child that left the helper's group, holding its output open, holds the
// wait for no longer than waitDelay.
func TestAHelpersChildOutsideItsGroupIsWaitedForNoLonger(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl to start a child in a group of its own")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	cancel, waited := runHelper(t, quietCommand{
		line:      `perl -e 'setpgrp(0, 0); exec "sleep", 600' & echo $! > ` + pidFile + "; wait",
		waitDelay: 50 * time.Millisecond,
	}, pidFile)
	cancel()
	testutil.Wait(t, waited, "the helper, past its child")
}

// A helper that exits while a child it left holds its output is waited for
// no longer than waitDelay, with no timeout or cancel to end the wait.
func TestAHelpersChildHoldingItsOutputIsWaitedForNoLonger(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	_, waited := runHelper(t, quietCommand{
		line:      "sleep 600 & echo $! > " + pidFile,
		waitDelay: 50 * time.Millisecond,
	}, pidFile)
	testutil.Wait(t, waited, "the helper, past the child holding its output")
}

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
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/processcreds"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
)

// quiet is cfg's credentials with a credential_process run by quietCommand:
// the SDK's own command hands the helper the sidecar's stderr, which the host
// logs as is, and the helper's diagnostics can name accounts or hold a
// credential. A process the SDK runs as an assumed role's source is built
// inside the role's provider, out of reach, so that chain is none.
func quiet(cfg aws.Config) aws.CredentialsProvider {
	profile := sharedConfig(cfg)
	switch {
	case aws.IsCredentialsProvider(cfg.Credentials, (*processcreds.Provider)(nil)):
		helper := quietCommand{line: profile.CredentialProcess, waitDelay: helperWaitDelay}
		return aws.NewCredentialsCache(processcreds.NewProviderCommand(helper))
	case aws.IsCredentialsProvider(cfg.Credentials, (*stscreds.AssumeRoleProvider)(nil)) && runsProcess(profile):
		return nil
	}
	return cfg.Credentials
}

// sharedConfig is the profile LoadDefaultConfig read, its source profiles
// linked beneath it.
func sharedConfig(cfg aws.Config) config.SharedConfig {
	for _, source := range cfg.ConfigSources {
		if profile, ok := source.(config.SharedConfig); ok {
			return profile
		}
	}
	return config.SharedConfig{}
}

// usesSSO is whether profile, or a source profile it chains from, signs in
// through IAM Identity Center.
func usesSSO(profile config.SharedConfig) bool {
	for p := &profile; p != nil; p = p.Source {
		if p.SSOSessionName != "" || p.SSOStartURL != "" {
			return true
		}
	}
	return false
}

// runsProcess is whether the SDK resolves profile's role chain by a
// credential_process: the deepest source's, whose credentials it uses, when
// that profile holds no keys, which the SDK prefers.
func runsProcess(profile config.SharedConfig) bool {
	p := &profile
	for p.Source != nil {
		p = p.Source
	}
	return p.CredentialProcess != "" && !p.Credentials.HasKeys()
}

// helperWaitDelay is how long a helper's descendants may hold its output open,
// once it has exited or been killed, before the wait for it ends anyway.
const helperWaitDelay = 5 * time.Second

// quietCommand runs a credential_process as the SDK does, through sh, but
// with no stdin and no stderr: the sidecar's stdin is the host's pipe, and
// its stderr the host's log. It leads a session of its own: with no
// controlling terminal, a helper that would prompt fails at once rather than
// stopping for input, and the SDK's timeout kills its process group whole. A
// descendant that left the group keeps its output open no longer than
// waitDelay, so a hung helper never holds the service's turn.
type quietCommand struct {
	line      string
	waitDelay time.Duration
}

func (c quietCommand) NewCommand(ctx context.Context) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.line)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = c.waitDelay
	return cmd, nil
}

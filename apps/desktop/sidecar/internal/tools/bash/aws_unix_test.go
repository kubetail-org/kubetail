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

//go:build !windows

package bash

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The config file is owner-only.
func TestTheAWSConfigFileIsOwnerOnly(t *testing.T) {
	t.Parallel()

	_, run := awsMount(t, "eu-west-1")
	info, err := os.Stat(filepath.Join(run.Dir, "aws", "config"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// A run with AWS starts through the real sandbox, whose check passes the
// mount's placeholders, and holds none of the user's credentials: not in its
// environment, its run directory, its TMPDIR or its tool home.
func TestNoRealCredentialIsInTheRun(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	tl.sandboxer = sandboxed(t)
	tl.cloud = Cloud{Platforms: []CloudPlatform{awsAs(fakeAWSLogin{"eu-west-1"}, nil)}, Switches: switchesOn(permissions.AWS).read}
	rt := tools.Runtime{Dir: testChatDir(t), Session: session.Session{Kind: session.Chat}}

	text, isError := tl.Run(t.Context(), rt, command(`env; find "$ZDOTDIR" "$TMPDIR" -type f -exec cat {} + 2>/dev/null`))
	require.False(t, isError, text)
	assert.Contains(t, text, "AWS_ACCESS_KEY_ID=KUBETAIL")
	assert.Contains(t, text, "disable_request_compression = true")
	for _, secret := range []string{awsTestCreds.AccessKeyID, awsTestCreds.SecretAccessKey, awsTestCreds.SessionToken} {
		assert.NotContains(t, text, secret)
	}
	require.NoError(t, filepath.WalkDir(tools.ToolHomePath(rt.Dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, secret := range []string{awsTestCreds.AccessKeyID, awsTestCreds.SecretAccessKey, awsTestCreds.SessionToken} {
			assert.NotContains(t, string(b), secret, path)
		}
		return nil
	}))
}

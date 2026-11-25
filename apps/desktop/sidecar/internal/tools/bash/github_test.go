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

package bash

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/githubproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// knownTokens answers one known token.
type knownTokens string

func (k knownTokens) Token(context.Context) (string, error) { return string(k), nil }
func (knownTokens) Refused(string)                          {}

// usersToken is the token behind the tests' GitHub cloud platform.
const usersToken = "gho_theUsersOwnGitHubToken0001"

// newTestGitHub is GitHub's cloud platform over usersToken, reaching up.
func newTestGitHub(up githubproxy.Upstreams) CloudPlatform {
	return NewGitHub(knownTokens(usersToken), up)
}

func TestGitHubsMountWritesItsFilesAndVariables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := newTestGitHub(githubproxy.GitHub()).Mount(t.Context(), CloudRun{
		On:   switchedOn,
		Port: 6443, Dir: dir, Session: session.Session{Kind: session.Chat}, Refusal: "kubetail: no",
	})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	assert.Equal(t, permissions.GitHub, m.Grant.Target())
	proxy := "http://kubetail:" + m.Grant.Token() + "@127.0.0.1:6443"

	gitconfig := filepath.Join(dir, "gitconfig")
	hosts := filepath.Join(dir, "gh", "hosts.yml")
	assert.Equal(t, []string{
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=include.path", "GIT_CONFIG_VALUE_0=" + gitconfig, "GIT_TERMINAL_PROMPT=0",
		"GH_CONFIG_DIR=" + filepath.Join(dir, "gh"), "GH_HOST=github.localhost", "GH_PROMPT_DISABLED=1",
		"HTTP_PROXY=" + proxy,
	}, m.Env)
	assertNoUserCanSet(t, m.Env)
	assert.Equal(t, []string{"github.localhost", "api.github.localhost"}, []string{m.Routes[0].Host, m.Routes[1].Host})

	assertFile(t, gitconfig, `[url "http://github.localhost/"]
	insteadOf = https://github.com/
	insteadOf = http://github.com/
	insteadOf = git://github.com/
	insteadOf = git@github.com:
	insteadOf = ssh://git@github.com/
	insteadOf = ssh://github.com/
[http "http://github.localhost/"]
	proxy = `+proxy+`
[credential "http://127.0.0.1:6443"]
	helper =
[credential "http://github.localhost"]
	helper =
`)
	assertFile(t, filepath.Join(dir, "gh", "config.yml"), "version: \"1\"\n")
	assertFile(t, hosts, `github.localhost:
    users:
        kubetail:
            oauth_token: kubetail-placeholder
    git_protocol: https
    oauth_token: kubetail-placeholder
    user: kubetail
`)
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}

// A mount whose files cannot be made fails, and its grant is ended: the run
// never sees it to end it.
func TestAGitHubMountThatCannotWriteFails(t *testing.T) {
	t.Parallel()
	for _, planted := range []string{"gitconfig", "gh"} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, planted), nil, 0o600))
		_, err := newTestGitHub(githubproxy.GitHub()).Mount(t.Context(), CloudRun{On: switchedOn, Port: 6443, Dir: dir, Session: session.Session{Kind: session.Chat}})
		assert.Error(t, err, planted)
	}
}

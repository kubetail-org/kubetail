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
	"strconv"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/githubproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// gitHubPlatform is GitHub's CloudPlatform: a run's git and gh reach github.com
// through its door, which adds the user's token outside the sandbox.
type gitHubPlatform struct {
	tokens githubproxy.TokenSource
	up     githubproxy.Upstreams
}

// NewGitHub is GitHub's cloud platform over the user's token, reaching up.
func NewGitHub(tokens githubproxy.TokenSource, up githubproxy.Upstreams) CloudPlatform {
	return gitHubPlatform{tokens: tokens, up: up}
}

func (gitHubPlatform) Target() permissions.Target { return permissions.GitHub }

// Mount writes the run's gitconfig and gh config under its directory, and
// answers both of GitHub's routes, its variables and its grant. The grant's
// token rides as proxy credentials, which git sends only once challenged.
//
// gh has no proxy setting of its own, so HTTP_PROXY carries it. Every other
// program that reads the variable sends its plain HTTP to the door too, with
// the grant's token, which opens GitHub's grant alone.
func (g gitHubPlatform) Mount(_ context.Context, run CloudRun) (CloudMount, error) {
	limits := cloudLimits
	limits.Challenges = true
	grant := runproxy.NewGrant(permissions.GitHub, run.On, run.Session, run.Asker, run.Refusal, limits)
	door := "127.0.0.1:" + strconv.Itoa(run.Port)
	proxy := "http://kubetail:" + grant.Token() + "@" + door
	gitconfig, ghDir := filepath.Join(run.Dir, "gitconfig"), filepath.Join(run.Dir, "gh")
	if err := writeGitHubFiles(gitconfig, ghDir, door, proxy); err != nil {
		grant.End()
		return CloudMount{}, err
	}
	handler := githubproxy.New(grant, g.tokens, g.up)
	return CloudMount{
		Routes: []Route{{Host: githubproxy.GitHost, Handler: handler}, {Host: githubproxy.APIHost, Handler: handler}},
		Env: []string{
			// Named through an include, so the system's and the user's config
			// still apply.
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=include.path", "GIT_CONFIG_VALUE_0=" + gitconfig,
			"GIT_TERMINAL_PROMPT=0",
			"GH_CONFIG_DIR=" + ghDir, "GH_HOST=" + githubproxy.GitHost, "GH_PROMPT_DISABLED=1",
			"HTTP_PROXY=" + proxy,
		},
		Grant: grant,
	}, nil
}

// writeGitHubFiles writes the run's gitconfig, which sends every github.com
// remote, SSH ones included, to the door, and gh's hosts.yml, whose login is
// a placeholder: gh refuses to run with none, and the proxy never forwards it.
//
// hosts.yml is in gh's multi-account shape and config.yml stamps it version 1,
// so no gh migrates them: a gh from 2.40 to 2.100 rewrites an unstamped config
// on every run, a write the sandbox refuses on macOS.
//
// The gitconfig clears every credential helper for the door and the route, so
// no helper, the keychain's included, is handed the grant's token to store.
func writeGitHubFiles(gitconfig, ghDir, door, proxy string) error {
	if err := writeNew(gitconfig, []byte(`[url "http://`+githubproxy.GitHost+`/"]
	insteadOf = https://github.com/
	insteadOf = http://github.com/
	insteadOf = git://github.com/
	insteadOf = git@github.com:
	insteadOf = ssh://git@github.com/
	insteadOf = ssh://github.com/
[http "http://`+githubproxy.GitHost+`/"]
	proxy = `+proxy+`
[credential "http://`+door+`"]
	helper =
[credential "http://`+githubproxy.GitHost+`"]
	helper =
`)); err != nil {
		return err
	}
	if err := os.Mkdir(ghDir, 0o700); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(ghDir, "config.yml"), []byte("version: \"1\"\n")); err != nil { // coverage:ignore gh was made empty above, so only a failing disk fails this write
		return err
	}
	return writeNew(filepath.Join(ghDir, "hosts.yml"), []byte(githubproxy.GitHost+`:
    users:
        kubetail:
            oauth_token: kubetail-placeholder
    git_protocol: https
    oauth_token: kubetail-placeholder
    user: kubetail
`))
}

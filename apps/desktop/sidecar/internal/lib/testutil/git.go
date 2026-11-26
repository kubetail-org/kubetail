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

package testutil

import (
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitOrigin serves a bare repository o/r over TLS through git http-backend,
// pushes allowed, whose branch main holds one commit, "seeded". It answers
// the server and the repository's path. A machine with no git leaves the
// test to RequireSandbox.
func GitOrigin(t testing.TB) (*httptest.Server, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		RequireSandbox(t, "no git on PATH")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "o", "r.git")
	work := filepath.Join(t.TempDir(), "work")
	Git(t, "", "init", "-q", "--bare", "-b", "main", repo)
	Git(t, "", "-C", repo, "config", "http.receivepack", "true")
	Git(t, "", "init", "-q", "-b", "main", work)
	Git(t, work, "-c", "user.name=k", "-c", "user.email=k@example.com", "commit", "-q", "--allow-empty", "-m", "seeded")
	Git(t, work, "push", "-q", repo, "main")
	// LookPath names the backend's .exe on Windows.
	backend, err := exec.LookPath(filepath.Join(strings.TrimSpace(Git(t, "", "--exec-path")), "git-http-backend"))
	if err != nil { // coverage:ignore git installs its backend beside itself
		t.Fatalf("git-http-backend: %v", err)
	}
	srv := httptest.NewTLSServer(&cgi.Handler{
		Path: backend,
		Env:  append([]string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}, isolated...),
	})
	t.Cleanup(srv.Close)
	return srv, repo
}

// isolated keeps git from reading the machine's and the user's config.
var isolated = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}

// Git runs git with args in dir, outside any sandbox and reading neither the
// machine's nor the user's config, and answers its output.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), isolated...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

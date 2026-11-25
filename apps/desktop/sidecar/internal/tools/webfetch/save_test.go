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

package webfetch

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// chatDir is a chat's directory of the test's own.
type chatDir string

func (d chatDir) Path() string { return string(d) }

func (d chatDir) Root(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(string(d), 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(string(d))
}

var savedTo = regexp.MustCompile(`Full page saved to: ([^;\n]+)`)

// A page past the inline limit is saved to the chat's results, as a command's
// output is, and the result is the header and a preview.
func TestWebFetchSavesALargePage(t *testing.T) {
	text := strings.Repeat("line\n", 8000)
	s := newSite(t, plain(text))
	dir := filepath.Join(t.TempDir(), "c1")

	got, isError := s.tool(s.dialing()).Run(t.Context(), tools.Runtime{Dir: chatDir(dir)}, call(s.url("a.test", "/")))

	assert.False(t, isError)
	m := savedTo.FindStringSubmatch(got)
	require.NotNil(t, m, got)
	assert.Equal(t, filepath.Join(dir, "results"), filepath.Dir(m[1]))
	assert.Equal(t, "Fetched "+s.url("a.test", "/")+" (text/plain, 39.1KB)\n\n<persisted-output>\nPage too large (39.1KB). Full page saved to: "+m[1]+
		"\n\nPreview (first 2KB):\n"+text[:tools.PreviewLen]+"\n</persisted-output>", got)
	saved, err := os.ReadFile(m[1])
	require.NoError(t, err)
	assert.Equal(t, text, string(saved))
}

// With no chat's directory the page is cut to the limit with a note.
func TestWebFetchCutsALargePageWithNowhereToSaveIt(t *testing.T) {
	s := newSite(t, plain(strings.Repeat("x", tools.InlineLimit+100)))
	got, isError := run(t, s.tool(s.dialing()), s.url("a.test", "/"))
	assert.False(t, isError)
	assert.LessOrEqual(t, len(got), tools.InlineLimit)
	assert.Regexp(t, `\n… \[\d+ bytes cut; the page could not be saved\]$`, got)
}

// A page that decodes past the file limit is saved cut at it, so Read, which
// opens a file of at most the limit, opens what was saved.
func TestAPageThatDecodesPastTheLimitIsSavedAtIt(t *testing.T) {
	s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
		_, _ = io.WriteString(w, strings.Repeat("\xe9", tools.FileLimit))
	}))
	data := t.TempDir()
	rt := tools.Runtime{Dir: chatDir(filepath.Join(data, "chats", "c1"))}

	got, isError := s.tool(s.dialing()).Run(t.Context(), rt, call(s.url("a.test", "/")))
	require.False(t, isError, got)
	m := savedTo.FindStringSubmatch(got)
	require.NotNil(t, m, got)
	info, err := os.Stat(m[1])
	require.NoError(t, err)
	assert.Equal(t, int64(tools.FileLimit), info.Size())
}

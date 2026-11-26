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

package read

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

// badInputs are inputs parse refuses.
var badInputs = []string{
	`{}`,
	`{"file_path":""}`,
	`{"file_path":null}`,
	`{"file_path":1}`,
	`{"File_path":"/a"}`,
	`{"file_path":"/a","file_path":"/b"}`,
	`{"file_path":"/a","extra":1}`,
	`{"file_path":"/a","offset":0}`,
	`{"file_path":"/a","offset":2.5}`,
	`{"file_path":"/a","offset":-1}`,
	`{"file_path":"/a","offset":1e2}`,
	`{"file_path":"/a","offset":1.0}`,
	`{"file_path":"/a","offset":"1"}`,
	`{"file_path":"/a","offset":null}`,
	`{"file_path":"/a","limit":0}`,
	`{"file_path":"/a","limit":99999999999999999999999}`,
	`{"file_path":"/a"} 1`,
	`{"file_path":"/a"`,
	`{"file_path"`,
	`{1}`,
	`[]`,
	`not json`,
}

// A Read input is a file_path, an optional offset and an optional limit, each key
// spelled exactly and once, the numbers whole and at least 1.
func TestReadReadsItsInput(t *testing.T) {
	for raw, want := range map[string]input{
		`{"file_path":"/a"}`:                       {Path: "/a", Offset: 1, Limit: defaultLimit},
		`{"file_path":"/a","offset":3}`:            {Path: "/a", Offset: 3, Limit: defaultLimit},
		`{"file_path":"/a","offset":3,"limit":10}`: {Path: "/a", Offset: 3, Limit: 10},
		`{"limit":1,"file_path":"/a"}`:             {Path: "/a", Offset: 1, Limit: 1},
		` {"file_path":"/a"} `:                     {Path: "/a", Offset: 1, Limit: defaultLimit},
	} {
		got, err := parse([]byte(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range badInputs {
		_, err := parse([]byte(raw))
		assert.ErrorIs(t, err, errInput, raw)
	}
}

// shown is what numbered shows, without whether it showed everything.
func shown(s string, offset, limit int) string {
	out, _ := numbered(s, offset, limit)
	return out
}

// A range is numbered as cat -n numbers it, from offset for limit lines; a line
// past 2000 runes is cut, and a final newline starts no line.
func TestReadNumbersTheLines(t *testing.T) {
	text := "one\ntwo\nthree\nfour\n"
	assert.Equal(t, "     1\tone\n     2\ttwo\n     3\tthree\n     4\tfour\n", shown(text, 1, defaultLimit))
	assert.Equal(t, "     2\ttwo\n     3\tthree\n", shown(text, 2, 2))
	assert.Equal(t, "     4\tfour\n", shown(text, 4, 1<<62), "a limit past the end stops at it")
	assert.Equal(t, "     1\tone\n     2\ttwo\n", shown("one\ntwo", 1, 10), "no final newline")
	assert.Equal(t, "The file has 4 lines.", shown(text, 5, 1))
	assert.Equal(t, "The file is empty.", shown("", 1, 1))
	assert.Equal(t, "     1\t\n", shown("\n", 1, 1), "one empty line")

	long := strings.Repeat("é", 2001)
	assert.Equal(t, "     1\t"+strings.Repeat("é", 2000)+"…\n", shown(long, 1, 1))
	fits := strings.Repeat("é", 2000)
	assert.Equal(t, "     1\t"+fits+"\n", shown(fits, 1, 1))
}

// A byte order mark at the start and a \r before each \n are not shown, so a
// CRLF file reads as its lines.
func TestNumberedStripsCRAndABOM(t *testing.T) {
	assert.Equal(t, "     1\ta\n     2\tb\n", shown("\ufeffa\r\nb\r\n", 1, defaultLimit))
	assert.Equal(t, "     1\ta\rb\n", shown("a\rb", 1, defaultLimit), "a lone \\r is the line's own")
}

// numbered says whether it showed every byte of the text as it is: from the
// first line, to the last, nothing cut and nothing stripped.
func TestNumberedSaysWhetherItShowedEveryByte(t *testing.T) {
	long := strings.Repeat("x", maxLineRunes+1)
	// Past InlineLimit in fewer lines than defaultLimit, so the result is cut.
	many := strings.Repeat(strings.Repeat("x", 100)+"\n", tools.InlineLimit/100)
	for name, c := range map[string]struct {
		text          string
		offset, limit int
		whole         bool
	}{
		"all of it":        {"a\nb\n", 1, defaultLimit, true},
		"no final newline": {"a\nb", 1, defaultLimit, true},
		"empty":            {"", 1, defaultLimit, true},
		"past line 1":      {"a\nb\n", 2, defaultLimit, false},
		"short of the end": {"a\nb\n", 1, 1, false},
		"past the end":     {"a\n", 3, defaultLimit, false},
		"a cut line":       {long, 1, defaultLimit, false},
		"a cut result":     {many, 1, defaultLimit, false},
		"a CRLF file":      {"a\r\nb\r\n", 1, defaultLimit, false},
		"a BOM":            {"\ufeffa\n", 1, defaultLimit, false},
	} {
		_, whole := numbered(c.text, c.offset, c.limit)
		assert.Equal(t, c.whole, whole, name)
	}
}

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

// stamps is a chat's file stamps, held by the test.
type stamps map[string]tools.Stamp

func (s stamps) Stamp(path string) (tools.Stamp, bool) { st, ok := s[path]; return st, ok }
func (s stamps) SetStamp(path string, st tools.Stamp)  { s[path] = st }

// tool is Read fenced around dirs, which must exist.
func tool(t *testing.T, dirs ...string) *Tool {
	t.Helper()
	tl, err := New(nil, dirs...)
	require.NoError(t, err)
	return tl
}

// A tool fenced out of no directory could open Kubetail's files, so New needs one.
func TestNewNeedsAFencedDirectory(t *testing.T) {
	_, err := New(nil)
	assert.ErrorIs(t, err, fileguard.ErrNoFence)
}

// chatStamps is a chat under a data directory of the test's own: its results
// directory, the tool, the chat's runtime, and the chat's stamps.
func chatStamps(t *testing.T) (dir string, tl *Tool, rt tools.Runtime, st stamps) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	dir = filepath.Join(data, "chats", "c1")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	st = stamps{}
	return dir, tool(t, data), tools.Runtime{Dir: chatDir(dir), Files: st}, st
}

// chat is a chat under a data directory of the test's own.
func chat(t *testing.T) (dir string, tl *Tool, rt tools.Runtime) {
	t.Helper()
	dir, tl, rt, _ = chatStamps(t)
	return dir, tl, rt
}

// file is a file of the test's own outside any data directory, holding body.
func file(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// ranged is the input that reads limit lines of path from offset.
func ranged(path string, offset, limit int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"file_path": path, "offset": offset, "limit": limit})
	return b
}

// A byte order mark and CRLF endings are stripped on both branches, and a read
// that stripped them is not Whole.
func TestReadStripsCRAndABOM(t *testing.T) {
	dir, tl, rt, st := chatStamps(t)
	body := "\ufeffa\r\nb\r\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "out.txt"), []byte(body), 0o600))
	path := file(t, body)

	for _, p := range []string{filepath.Join(dir, "out.txt"), path} {
		text, isError := tl.Run(t.Context(), rt, call(p))
		assert.False(t, isError, p)
		assert.Equal(t, "     1\ta\n     2\tb\n", text, p)
	}
	assert.Equal(t, stamps{path: {Sum: sha256.Sum256([]byte(body))}}, st, "the results are not stamped")
}

// Only a read that showed every byte as it is stamps Whole.
func TestReadStampsWholeOnlyForEveryByte(t *testing.T) {
	_, tl, rt, st := chatStamps(t)
	// Past InlineLimit in fewer lines than defaultLimit, so the result is cut.
	many := strings.Repeat(strings.Repeat("x", 100)+"\n", tools.InlineLimit/100)
	for name, c := range map[string]struct {
		body          string
		offset, limit int
	}{
		"an offset past 1":         {"a\nb\n", 2, defaultLimit},
		"a limit short of the end": {"a\nb\n", 1, 1},
		"a cut line":               {strings.Repeat("x", maxLineRunes+1), 1, defaultLimit},
		"a cut result":             {many, 1, defaultLimit},
		"a CRLF file":              {"a\r\nb\r\n", 1, defaultLimit},
		"a BOM":                    {"\ufeffa\n", 1, defaultLimit},
	} {
		path := file(t, c.body)
		input := ranged(path, c.offset, c.limit)
		_, isError := tl.Run(t.Context(), rt, input)
		require.False(t, isError, name)
		assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte(c.body))}, st[path], name)
	}
}

// A read never takes back what an earlier one showed: a range of a file seen
// whole leaves it Whole, until the file changes.
func TestARangeReadKeepsAWholeStamp(t *testing.T) {
	_, tl, rt, st := chatStamps(t)
	path := file(t, "a\nb\n")

	_, isError := tl.Run(t.Context(), rt, call(path))
	require.False(t, isError)
	_, isError = tl.Run(t.Context(), rt, ranged(path, 2, 1))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a\nb\n")), Whole: true}, st[path])

	require.NoError(t, os.WriteFile(path, []byte("a\nc\n"), 0o600))
	_, isError = tl.Run(t.Context(), rt, ranged(path, 2, 1))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a\nc\n"))}, st[path])
}

// A stat or open can block in a syscall no context reaches, so the call answers
// its cancel while the file work still blocks; when that work returns late, it
// is dropped, and nothing is stamped.
func TestReadAnswersTheCancelWhileTheFileWorkBlocks(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.MkdirAll(data, 0o700))
	tl := tool(t, data)
	real := tl.fetch
	release := make(chan struct{})
	returned := make(chan struct{})
	tl.fetch = func(path string) ([]byte, error) {
		defer close(returned)
		<-release
		return real(path)
	}
	st := stamps{}
	rt := tools.Runtime{Dir: chatDir(filepath.Join(data, "chats", "c1")), Files: st}
	path := file(t, "a\n")
	ctx, cancel := context.WithCancel(t.Context())

	var text string
	var isError bool
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		text, isError = tl.Run(ctx, rt, call(path))
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.True(t, isError)
	assert.Equal(t, cancelled, text)

	close(release)
	testutil.Wait(t, returned, "the file work")
	assert.Empty(t, st, "a cancelled call stamps nothing")
}

// Outside the data directory the user approved the path, so a refusal says
// what is wrong, and never in a Go error's words.
func TestReadRefusalsSayWhatIsWrong(t *testing.T) {
	_, tl, rt := chat(t)
	dir := t.TempDir()
	big, err := os.Create(filepath.Join(dir, "big.txt"))
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin"), []byte("ELF\x00\x01"), 0o600))
	sep := string(filepath.Separator)

	for path, want := range map[string]string{
		"x.txt":                           "Read takes an absolute path.",
		dir + sep + "." + sep + "big.txt": "Read takes a path in its plain form. Call again with: " + filepath.Join(dir, "big.txt"),
		filepath.Join(dir, "missing.txt"): "The file does not exist.",
		dir:                               "This is a directory, not a file.",
		filepath.Join(dir, "big.txt"):     "The file is larger than 8 MiB, which Read does not open.",
		filepath.Join(dir, "bin"):         "The file looks binary. Read opens text files only.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
}

// An approved file is read numbered, and its stamp is the hash of its bytes,
// Whole when every byte was shown as it is.
func TestReadOpensAnApprovedFile(t *testing.T) {
	_, tl, rt, st := chatStamps(t)
	path := file(t, "a\nb\n")

	text, isError := tl.Run(t.Context(), rt, call(path))
	assert.False(t, isError, text)
	assert.Equal(t, "     1\ta\n     2\tb\n", text)
	assert.Equal(t, stamps{path: {Sum: sha256.Sum256([]byte("a\nb\n")), Whole: true}}, st)
}

// A file outside the results is redacted like one in them, and a read the
// redaction changed is not Whole: writing it back would write the placeholder.
func TestReadRedactsAFileOutsideTheResults(t *testing.T) {
	_, tl, rt, st := chatStamps(t)
	body := "users:\n- name: ana\n  user:\n    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQo=\n"
	path := file(t, body)

	text, isError := tl.Run(t.Context(), rt, call(path))
	assert.False(t, isError, text)
	assert.NotContains(t, text, "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQo=")
	assert.Contains(t, text, "client-key-data:")
	assert.Equal(t, stamps{path: {Sum: sha256.Sum256([]byte(body))}}, st)
}

// call is the input that reads path.
func call(path string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"file_path": path})
	return b
}

// A file the chat's directory holds is read, numbered.
func TestReadReadsASavedFile(t *testing.T) {
	dir, tl, rt := chat(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "out.txt"), []byte("a\nb\n"), 0o600))

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(dir, "out.txt")))
	assert.False(t, isError)
	assert.Equal(t, "     1\ta\n     2\tb\n", text)
}

// Every path in the data directory the chat's directory does not hold as a regular
// file within the limit is refused in the same words, which name nothing about
// the path.
func TestReadRefusesAPathOutsideTheChat(t *testing.T) {
	dir, tl, rt := chat(t)
	data := filepath.Dir(filepath.Dir(dir))
	other := filepath.Join(filepath.Dir(dir), "c2")
	require.NoError(t, os.MkdirAll(other, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(data, "app.db"), []byte("x"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
	big, err := os.Create(filepath.Join(dir, "big.txt"))
	require.NoError(t, err)
	require.NoError(t, big.Truncate(tools.FileLimit+1))
	require.NoError(t, big.Close())

	for _, path := range []string{
		filepath.Join(other, "x.txt"),
		filepath.Join(data, "app.db"),
		data,
		dir,
		filepath.Join(dir, "sub"),
		filepath.Join(dir, "big.txt"),
		filepath.Join(dir, "missing.txt"),
	} {
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, notHere, text, path)
	}

	none := tools.Runtime{Dir: chatDir(filepath.Join(data, "none")), Files: stamps{}}
	text, isError := tl.Run(t.Context(), none, call(filepath.Join(data, "none", "x")))
	assert.True(t, isError)
	assert.Equal(t, notHere, text, "a chat with no chat's directory")
}

// An input parse refuses runs nothing.
func TestReadRefusesABadInput(t *testing.T) {
	_, tl, rt := chat(t)
	for _, raw := range badInputs {
		text, isError := tl.Run(t.Context(), rt, json.RawMessage(raw))
		assert.True(t, isError, raw)
		assert.Equal(t, `{"error":"bad-input"}`, text, raw)
	}
}

// approval is what the tool answers for path in rt's chat.
func approval(t *testing.T, tl *Tool, rt tools.Runtime, path string) tools.Approval {
	t.Helper()
	got, err := tl.Approval(t.Context(), rt, call(path))
	require.NoError(t, err)
	return got
}

// A file of the chat's directory is read without asking.
func TestReadOfTheResultsIsSkipped(t *testing.T) {
	dir, tl, rt := chat(t)
	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, filepath.Join(dir, "out.txt")))
}

// The rest of the data directory is never asked about, since Run refuses it.
func TestReadOfTheDataDirIsSkippedAndRefused(t *testing.T) {
	dir, tl, rt := chat(t)
	data := filepath.Dir(filepath.Dir(dir))
	require.NoError(t, os.WriteFile(filepath.Join(data, "host.json"), []byte("{}"), 0o600))

	for _, path := range []string{filepath.Join(data, "host.json"), filepath.Join(filepath.Dir(dir), "c2", "x.txt"), data} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, path), path)
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, notHere, text, path)
	}
}

// The cache and runtime directories are fenced as the data directory is: never
// asked about, and refused.
func TestReadOfTheCacheAndRuntimeDirsIsSkippedAndRefused(t *testing.T) {
	dir, _, rt := chat(t)
	data := filepath.Dir(filepath.Dir(dir))
	cache, run := t.TempDir(), t.TempDir()
	tl := tool(t, data, cache, run)
	for _, other := range []string{cache, run} {
		path := filepath.Join(other, "x.txt")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, path), path)
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, notHere, text, path)
	}
}

// A path Abs refuses is never asked about, and Run says what is wrong.
func TestReadOfAPathAbsRefusesIsSkippedAndRefused(t *testing.T) {
	_, tl, rt := chat(t)
	plain := filepath.Join(t.TempDir(), "x.txt")
	sep := string(filepath.Separator)
	notPlain := filepath.Dir(plain) + sep + "sub" + sep + ".." + sep + "x.txt"

	for path, want := range map[string]string{
		notPlain: "Read takes a path in its plain form. Call again with: " + plain,
		"x.txt":  "Read takes an absolute path.",
		plain + "\n" + sep + "etc" + sep + "passwd": "Read takes a path of one line, with no control character, of at most 4096 bytes.",
	} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, path), path)
		text, isError := tl.Run(t.Context(), rt, call(path))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
}

// Anywhere else waits for the user, with no directory to show.
func TestReadElsewhereIsAsked(t *testing.T) {
	_, tl, rt := chat(t)
	assert.Equal(t, tools.Approval{}, approval(t, tl, rt, filepath.Join(t.TempDir(), "x.txt")))

	_, err := tl.Approval(t.Context(), rt, json.RawMessage(`{}`))
	assert.ErrorIs(t, err, errInput, "an input Run refuses shows nothing")
}

// Approval reads the results from the runtime it is handed: a path under them
// skips, and the same path is asked about in a runtime whose results are
// elsewhere.
func TestApprovalReadsTheRuntimeItIsHanded(t *testing.T) {
	tl := tool(t, t.TempDir())
	results := t.TempDir()
	path := filepath.Join(results, "out.txt")

	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, tools.Runtime{Dir: chatDir(results)}, path))
	assert.Equal(t, tools.Approval{}, approval(t, tl, tools.Runtime{Dir: chatDir(t.TempDir())}, path))
}

// A file a command wrote into the directory is not known to be redacted, so Read
// redacts it whole, and a key spanning the range asked for is redacted too.
func TestReadRedactsWhatACommandWrote(t *testing.T) {
	dir, tl, rt := chat(t)
	body := "MIIEowIBAAKCAQEA\nMIIEowIBAAKCAQEB\n"
	text := "password: hunter2\n-----BEGIN RSA PRIVATE KEY-----\n" + body + "-----END RSA PRIVATE KEY-----\nafter\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd.txt"), []byte(text), 0o600))

	whole, isError := tl.Run(t.Context(), rt, call(filepath.Join(dir, "cmd.txt")))
	assert.False(t, isError)
	assert.NotContains(t, whole, "hunter2")
	assert.NotContains(t, whole, "MIIEowIBAAKCAQE")

	b, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "cmd.txt"), "offset": 2, "limit": 1})
	part, isError := tl.Run(t.Context(), rt, b)
	assert.False(t, isError)
	assert.Equal(t, "     2\t-----BEGIN RSA PRIVATE KEY-----[redacted]-----END RSA PRIVATE KEY-----\n", part)
}

// A range longer than the inline limit is cut at a line, and says where to go on.
func TestReadCutsToTheInlineLimitWithTheNextOffset(t *testing.T) {
	line := strings.Repeat("x", 100)
	text := strings.Repeat(line+"\n", defaultLimit)

	got := shown(text, 1, defaultLimit)

	assert.LessOrEqual(t, len(got), tools.InlineLimit)
	m := regexp.MustCompile(`\n… \[cut; continue with offset (\d+)\]$`).FindStringSubmatch(got)
	require.NotNil(t, m, got[len(got)-80:])
	next, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	assert.Contains(t, got, fmt.Sprintf("%6d\t%s\n", next-1, line), "the last line kept is whole")
	assert.NotContains(t, got, fmt.Sprintf("%6d\t", next))

	entry := len(fmt.Sprintf("%6d\t%s\n", 1, line))
	exact := strings.Repeat(line+"\n", tools.InlineLimit/entry)
	assert.NotContains(t, shown(exact, 1, defaultLimit), "cut", "a range that fits is not cut")

	lines := tools.InlineLimit/entry + 1
	over := shown(strings.Repeat(line+"\n", lines), 1, defaultLimit)
	assert.True(t, strings.HasSuffix(over, fmt.Sprintf("… [cut; continue with offset %d]", lines)), "a last line that does not fit is still named")
	assert.LessOrEqual(t, len(over), tools.InlineLimit)
}

// The offer is the reference's Read, and the section says what comes back is
// redacted data.
func TestTheDefinitionIsTheReferences(t *testing.T) {
	def := tool(t, t.TempDir()).Definition()
	assert.Equal(t, "Read", def.Name)
	assert.Equal(t, Name, def.Name)
	assert.True(t, strings.HasPrefix(def.Description, "Reads a text file from the user's machine.\n"))
	assert.Contains(t, def.Description, "waits for the user to approve it")
	assert.Contains(t, def.Description, "directories cannot be read")
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.Equal(t, []string{"file_path"}, schema.Required)
	assert.Len(t, schema.Properties, 3)

	section := tool(t, t.TempDir()).Prompt()
	assert.True(t, strings.HasPrefix(section, "## Read\n"))
	assert.Contains(t, section, "waits on the user")
	assert.Contains(t, section, "redacted")
}

// Reading and redacting 8 MiB can pass the turn's default bound, so the tool
// carries its own; it is gated.
func TestReadIsBoundedAndGated(t *testing.T) {
	var tl tools.Tool = tool(t, t.TempDir())
	b, ok := tl.(tools.Bounded)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, b.CallTimeout(nil))
	_, gated := tl.(tools.Gated)
	assert.True(t, gated)
}

// A cancel that lands before the redaction ends the call there.
func TestReadStopsOnACancel(t *testing.T) {
	dir, tl, rt := chat(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "out.txt"), []byte("a\n"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	text, isError := tl.Run(ctx, rt, call(filepath.Join(dir, "out.txt")))
	assert.True(t, isError)
	assert.Equal(t, `{"error":"cancelled"}`, text)
}

// ActionOf is the path the call named, as it named it, and refuses what parse
// refuses.
func TestActionOfReadsThePath(t *testing.T) {
	got, err := ActionOf(json.RawMessage(`{"file_path":"/data/tool-results/c/a \u202e.txt","offset":3}`), "")
	require.NoError(t, err)
	assert.Equal(t, tools.Action{Read: &tools.ReadAction{Path: "/data/tool-results/c/a \u202e.txt"}}, got)

	for _, raw := range badInputs {
		_, err := ActionOf(json.RawMessage(raw), "")
		assert.ErrorIs(t, err, errInput, raw)
	}
}

// Stored arguments read the same in every build: the transcript recomputes a
// call's action from its row on every read. A case here is never edited;
// changing what an input means is a new tool name.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	for raw, want := range map[string]tools.Action{
		`{"file_path":"/data/tool-results/c/out.txt"}`:                         {Read: &tools.ReadAction{Path: "/data/tool-results/c/out.txt"}},
		`{"file_path":"/data/tool-results/c/out.txt","offset":201,"limit":50}`: {Read: &tools.ReadAction{Path: "/data/tool-results/c/out.txt"}},
		`{"file_path":"/c/Users/ana/AppData/out.txt"}`:                         {Read: &tools.ReadAction{Path: "/c/Users/ana/AppData/out.txt"}},
	} {
		got, err := ActionOf(json.RawMessage(raw), "")
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

// The tool names itself and reads a call as ActionOf does, as a read.
func TestReadNamesItsKind(t *testing.T) {
	raw := json.RawMessage(`{"file_path":"/etc/hosts"}`)
	tool := &Tool{}
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, tools.ActionRead, tool.ActionKind())
	got, err := tool.Action(raw, "", false)
	require.NoError(t, err)
	want, err := ActionOf(raw, "")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, tools.ActionRead, got.Kind())
}

// A read in the chat's workspace stamps the file as a read outside the data
// directory does, so Write and Edit can change a file a command made. A saved
// result beside the workspace stays unstamped.
func TestReadStampsAWorkspaceFile(t *testing.T) {
	dir, tl, rt, st := chatStamps(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	whole, part, saved := filepath.Join(ws, "x.txt"), filepath.Join(ws, "y.txt"), filepath.Join(dir, "out.txt")
	for _, p := range []string{whole, part, saved} {
		require.NoError(t, os.WriteFile(p, []byte("a\nb\n"), 0o600))
	}

	_, isError := tl.Run(t.Context(), rt, call(whole))
	require.False(t, isError)
	_, isError = tl.Run(t.Context(), rt, ranged(part, 2, 1))
	require.False(t, isError)
	_, isError = tl.Run(t.Context(), rt, call(saved))
	require.False(t, isError)

	sum := sha256.Sum256([]byte("a\nb\n"))
	assert.Equal(t, stamps{
		whole: {Sum: sum, Whole: true},
		part:  {Sum: sum, Whole: false},
	}, st)
}

// A UNC or device path, which only Windows has, is worded on every machine.
func TestReadWordsAPathOffThisMachine(t *testing.T) {
	assert.Equal(t, "Read opens files on this machine only.", refusal(fileguard.ErrNotLocal))
}

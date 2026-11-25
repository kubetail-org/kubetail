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

package write

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

// stamps is a chat's file stamps, held by the test.
type stamps map[string]tools.Stamp

func (s stamps) Stamp(path string) (tools.Stamp, bool) { st, ok := s[path]; return st, ok }
func (s stamps) SetStamp(path string, st tools.Stamp)  { s[path] = st }

// tool is Write fenced around a data directory of the test's own, under a
// umask of 022, and that directory.
func tool(t *testing.T) (*Tool, string) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Mkdir(data, 0o700))
	tl, err := New(0o022, nil, data)
	require.NoError(t, err)
	return tl, data
}

// A tool fenced out of no directory could change Kubetail's files, so New needs
// one.
func TestNewNeedsAFencedDirectory(t *testing.T) {
	_, err := New(0o022, nil)
	assert.ErrorIs(t, err, fileguard.ErrNoFence)
}

// chat is Write, a runtime of a chat of the test's own, the chat's stamps, and
// the data directory.
func chat(t *testing.T) (*Tool, tools.Runtime, stamps, string) {
	t.Helper()
	tl, data := tool(t)
	st := stamps{}
	return tl, tools.Runtime{Files: st, Dir: chatDirIn(data)}, st, data
}

// chatDir is a chat's directory, not yet made.
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

// chatDirIn is a chat's directory under the data directory, where chat
// keeps them.
func chatDirIn(data string) chatDir {
	return chatDir(filepath.Join(data, "chats", "c1"))
}

// call is the input that writes content at path.
func call(path, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"file_path": path, "content": content})
	return b
}

// approval is what the tool answers for a call in rt's chat.
func approval(t *testing.T, tl *Tool, rt tools.Runtime, raw json.RawMessage) tools.Approval {
	t.Helper()
	got, err := tl.Approval(t.Context(), rt, raw)
	require.NoError(t, err)
	return got
}

// The gate reads the path by name alone: a path Abs refuses, one under the data
// directory by name, and content holding a NUL are never asked about, since Run
// refuses each before touching anything; every other path is asked alike,
// whatever is on disk, and nothing on disk is touched.
func TestWriteAsksOnTheNameAlone(t *testing.T) {
	tl, data := tool(t)
	tl.write = func(context.Context, tools.Runtime, string, *session.Folder, []byte) (bool, error) {
		t.Fatal("the gate touched the disk")
		return false, nil
	}
	rt := tools.Runtime{Files: stamps{}, Dir: chatDirIn(data)}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o600))
	sep := string(filepath.Separator)

	for _, raw := range []json.RawMessage{
		call("x.txt", "a"),
		call(dir+sep+"."+sep+"x.txt", "a"),
		call(filepath.Join(data, "app.db"), "a"),
		call(filepath.Join(dir, "x.txt"), "a\x00b"),
	} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, raw), string(raw))
	}
	for _, path := range []string{filepath.Join(dir, "missing", "x.txt"), dir, filepath.Join(dir, "x.txt")} {
		assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(path, "a")), path)
	}

	_, err := tl.Approval(t.Context(), rt, json.RawMessage(`{}`))
	assert.ErrorIs(t, err, errInput, "an input Run refuses shows nothing")
}

// A path Abs refuses says what is wrong, in Write's words.
func TestWriteRefusesAPathAbsRefuses(t *testing.T) {
	tl, rt, _, _ := chat(t)
	plain := filepath.Join(t.TempDir(), "x.txt")
	sep := string(filepath.Separator)
	notPlain := filepath.Dir(plain) + sep + "sub" + sep + ".." + sep + "x.txt"

	for path, want := range map[string]string{
		notPlain: "Write takes a path in its plain form. Call again with: " + plain,
		"x.txt":  "Write takes an absolute path.",
		plain + "\n" + sep + "etc" + sep + "passwd": "Write takes a path of one line, with no control character, of at most 4096 bytes.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "a"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
	assert.NoFileExists(t, plain)
}

// Nothing under the data directory is written, and a path there by name is
// refused before anything is touched.
func TestWriteRefusesTheDataDirByName(t *testing.T) {
	tl, rt, _, data := chat(t)
	for _, path := range []string{data, filepath.Join(data, "host.json"), filepath.Join(data, "new", "x.txt")} {
		text, isError := tl.Run(t.Context(), rt, call(path, "{}"))
		assert.True(t, isError, path)
		assert.Equal(t, "Write cannot change Kubetail's directories.", text, path)
	}
	assert.Empty(t, names(t, data))
}

// The cache directory is fenced as the data directory is.
func TestWriteRefusesTheCacheDir(t *testing.T) {
	_, rt, _, data := chat(t)
	cache := t.TempDir()
	tl, err := New(0o022, nil, data, cache)
	require.NoError(t, err)
	path := filepath.Join(cache, "x.txt")

	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "{}")))
	text, isError := tl.Run(t.Context(), rt, call(path, "{}"))
	assert.True(t, isError)
	assert.Equal(t, "Write cannot change Kubetail's directories.", text)
	assert.NoFileExists(t, path)
}

// A NUL anywhere in the content, past Binary's first 8 KiB included, is
// refused: the file would read as binary, which no file tool changes again.
func TestWriteRefusesANul(t *testing.T) {
	tl, rt, _, _ := chat(t)
	path := filepath.Join(t.TempDir(), "x.txt")
	for _, content := range []string{"\x00", "a\x00b", strings.Repeat("a", 9<<10) + "\x00"} {
		text, isError := tl.Run(t.Context(), rt, call(path, content))
		assert.True(t, isError)
		assert.Equal(t, "Write writes text only, and the content holds a NUL byte.", text)
	}
	assert.NoFileExists(t, path)
}

// An input parse refuses runs nothing.
func TestWriteRefusesABadInput(t *testing.T) {
	tl, rt, _, _ := chat(t)
	for _, raw := range badInputs {
		text, isError := tl.Run(t.Context(), rt, json.RawMessage(raw))
		assert.True(t, isError, raw)
		assert.Equal(t, `{"error":"bad-input"}`, text, raw)
	}
}

// file is a file of the test's own outside any data directory, holding body,
// and stamped in st as a read of every byte would stamp it.
func file(t *testing.T, st stamps, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	st[path] = tools.Stamp{Sum: sha256.Sum256([]byte(body)), Whole: true}
	return path
}

// created and updated are Write's two answers, as the reference words them.
func created(path string) string {
	return "File created successfully at: " + path + " (file state is current in your context — no need to Read it back)"
}

func updated(path string) string {
	return "The file " + path + " has been updated successfully. (file state is current in your context — no need to Read it back)"
}

// content is what path holds.
func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// A new file is made with the directories missing on its way, and needs no
// stamp; its bytes are the content exactly, with no newline added.
func TestWriteCreatesAFile(t *testing.T) {
	tl, rt, _, _ := chat(t)
	path := filepath.Join(t.TempDir(), "a", "b", "x.yaml")

	text, isError := tl.Run(t.Context(), rt, call(path, "a: 1\r\nb: 2"))
	assert.False(t, isError, text)
	assert.Equal(t, created(path), text)
	assert.Equal(t, "a: 1\r\nb: 2", content(t, path))
}

// A file read whole and unchanged since is replaced by the content exactly: no
// line endings kept from the old file, no newline added.
func TestWriteReplacesAFileReadWhole(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "a\r\nb\r\n")

	text, isError := tl.Run(t.Context(), rt, call(path, "c\nd"))
	assert.False(t, isError, text)
	assert.Equal(t, updated(path), text)
	assert.Equal(t, "c\nd", content(t, path))
}

// A replace leaves no temporary file.
func TestWriteLeavesNoTemporaryFile(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "old\n")

	_, isError := tl.Run(t.Context(), rt, call(path, "new\n"))
	require.False(t, isError)
	assert.Equal(t, "new\n", content(t, path))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// An existing file is replaced only over a stamp from this chat that matches
// it and saw every byte; each refusal says how to get one, and changes nothing.
func TestWriteNeedsAMatchingWholeStamp(t *testing.T) {
	tl, rt, st, _ := chat(t)
	unread := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(unread, []byte("a\n"), 0o600))
	changed := file(t, st, "a\n")
	require.NoError(t, os.WriteFile(changed, []byte("b\n"), 0o600))
	part := file(t, st, "a\n")
	st[part] = tools.Stamp{Sum: st[part].Sum}

	for path, want := range map[string]string{
		unread:  "Read the file before replacing it.",
		changed: "The file changed since you last read it. Read it again before replacing it.",
		part:    "You were shown only part of this file, or it was redacted or had its line endings or byte order mark changed when read. Read it whole, with no offset or limit, or change it with Edit.",
	} {
		before := content(t, path)
		text, isError := tl.Run(t.Context(), rt, call(path, "new\n"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
		assert.Equal(t, before, content(t, path), path)
	}
}

// A binary and a file past the limit are refused in their own words: Read
// refuses both, so no stamp could match.
func TestWriteRefusesABinaryAndALargeFile(t *testing.T) {
	tl, rt, st, _ := chat(t)
	bin := file(t, st, "ELF\x00\x01")
	big := file(t, st, "")
	require.NoError(t, os.Truncate(big, tools.FileLimit+1))

	for path, want := range map[string]string{
		bin: "The file looks binary. Write replaces text files only.",
		big: "The file is larger than 8 MiB, which Write does not replace.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "x"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
}

// A directory, and a path with a file where a directory must be, are refused
// before anything on disk changes.
func TestWriteRefusesADirectoryAndAFileInThePath(t *testing.T) {
	tl, rt, _, _ := chat(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(f, nil, 0o600))

	for path, want := range map[string]string{
		dir:                            "This is a directory, not a file.",
		filepath.Join(f, "a", "x.txt"): "Part of this path is a file, not a directory: " + f,
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "x"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
	assert.Equal(t, []string{"f"}, names(t, dir))
}

// A write the call answers as done stamps the file Whole with the content's
// hash, since the model wrote every byte: a Write straight after passes the
// stamp check without a Read. A write answered as cancelled stamps nothing.
func TestWriteSetsTheStamp(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := filepath.Join(t.TempDir(), "x.txt")

	_, isError := tl.Run(t.Context(), rt, call(path, "a\r\n"))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a\r\n")), Whole: true}, st[path])
	text, isError := tl.Run(t.Context(), rt, call(path, "b\n"))
	require.False(t, isError, text)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("b\n")), Whole: true}, st[path])

	// The cancel answers before the write returns, so the test waits for it: a
	// write still statting the temp tree keeps Windows from removing it.
	real := tl.write
	returned := make(chan struct{})
	tl.write = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
		defer close(returned)
		return real(ctx, rt, path, folder, content)
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	other := filepath.Join(t.TempDir(), "y.txt")
	text, isError = tl.Run(ended, rt, call(other, "c\n"))
	assert.True(t, isError)
	assert.Equal(t, cancelled, text)
	testutil.Wait(t, returned, "the write")
	assert.NotContains(t, st, other)
	assert.NoFileExists(t, other)
}

// A stat on a dead mount can block in a syscall no context reaches, so the call
// answers its cancel while the write still blocks; when the write wakes, it
// changes nothing on disk, and nothing is stamped.
func TestAStuckWriteHonoursTheCancel(t *testing.T) {
	tl, data := tool(t)
	real := tl.write
	release := make(chan struct{})
	returned := make(chan struct{})
	tl.write = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
		defer close(returned)
		<-release
		return real(ctx, rt, path, folder, content)
	}
	st := stamps{}
	rt := tools.Runtime{Files: st, Dir: chatDirIn(data)}
	root := t.TempDir()
	path := filepath.Join(root, "a", "x.txt")
	ctx, cancel := context.WithCancel(t.Context())

	var text string
	var isError bool
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		text, isError = tl.Run(ctx, rt, call(path, "x\n"))
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.True(t, isError)
	assert.Equal(t, cancelled, text)

	close(release)
	testutil.Wait(t, returned, "the write")
	assert.Empty(t, names(t, root), "nothing is made")
	assert.Empty(t, st, "a cancelled call stamps nothing")
}

// What fileguard refuses is worded by the tool, never with a Go error's text.
func TestWriteWordsWhatFileguardRefuses(t *testing.T) {
	tl, data := tool(t)
	rt := tools.Runtime{Files: stamps{}, Dir: chatDirIn(data)}
	for err, want := range map[error]string{
		fileguard.ErrOtherUser:                  "This file belongs to another user. Kubetail does not replace it.",
		fileguard.ErrOtherGroup:                 "This file belongs to a group you are not in. Kubetail does not replace it.",
		fileguard.ErrReadOnly:                   "Kubetail cannot write this file.",
		fileguard.ErrDirReadOnly{Prefix: "/d"}:  "Kubetail cannot replace files in this directory.",
		cannotWriteIn("/d"):                     "Kubetail cannot write in /d.",
		fileguard.ErrUnresolved:                 "Kubetail cannot write this file.",
		fileguard.ErrExists:                     "A file was created at this path since the request. Read it before replacing it.",
		fileguard.ErrReplace:                    "Kubetail could not replace the file.",
		fileguard.ErrWrite:                      "Kubetail could not write the file.",
		fileguard.ErrLink{Target: "/etc/hosts"}: "This is a symbolic link. Write to its target instead: /etc/hosts",
		fileguard.ErrNotRegular:                 "This is not a regular file.",
		fileguard.ErrNotLocal:                   "Write changes files on this machine only.",
		errUnreadable:                           "Kubetail cannot read this file.",
		errCannotWrite:                          "Kubetail cannot write this file.",
		errors.New("disk full"):                 "Kubetail could not write the file.",
	} {
		tl.write = func(context.Context, tools.Runtime, string, *session.Folder, []byte) (bool, error) { return false, err }
		text, isError := tl.Run(t.Context(), rt, call(filepath.Join(t.TempDir(), "x.txt"), "x"))
		assert.True(t, isError, err.Error())
		assert.Equal(t, want, text, err.Error())
	}
}

// names is what dir holds, by name.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// The offer is the reference's Write, and the description and the section tell
// the model to Read first and that the user approves every write.
func TestTheDefinitionIsTheReferences(t *testing.T) {
	tl, _ := tool(t)
	def := tl.Definition()
	assert.Equal(t, Name, def.Name)
	assert.Equal(t, "Write", def.Name)
	assert.True(t, strings.HasPrefix(def.Description, "Writes a file on the user's machine, replacing it whole if one is there.\n"))
	assert.Contains(t, def.Description, "needs a Read of the whole file")
	assert.Contains(t, def.Description, "waits for the user to approve the path and the content")
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.Equal(t, []string{"file_path", "content"}, schema.Required)
	assert.Len(t, schema.Properties, 2)

	section := tl.Prompt()
	assert.True(t, strings.HasPrefix(section, "## Write\n"))
	assert.Contains(t, section, "only when it is part of what the user asked for")
}

// Reading, hashing and writing 8 MiB can pass the turn's default bound, so the
// tool carries its own; it is gated.
func TestWriteIsBoundedAndGated(t *testing.T) {
	tl, _ := tool(t)
	var got tools.Tool = tl
	b, ok := got.(tools.Bounded)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, b.CallTimeout(nil))
	_, gated := got.(tools.Gated)
	assert.True(t, gated)
}

// badInputs are inputs parse refuses.
var badInputs = []string{
	`{}`,
	`{"file_path":"/a"}`,
	`{"content":"x"}`,
	`{"file_path":"","content":"x"}`,
	`{"file_path":null,"content":"x"}`,
	`{"file_path":"/a","content":null}`,
	`{"file_path":"/a","content":1}`,
	`{"File_path":"/a","content":"x"}`,
	`{"file_path":"/a","file_path":"/b","content":"x"}`,
	`{"file_path":"/a","content":"x","content":"y"}`,
	`{"file_path":"/a","content":"x","extra":1}`,
	`{"file_path":"/a","content":"x"} 1`,
	`{"file_path":"/a","content":"x"`,
	`{"file_path":`,
	`{"file_path":"/a",`,
	`[]`,
	`not json`,
}

// A Write input is a file_path and a content, each key spelled exactly and
// once, both strings; the content may be empty, and at most FileLimit bytes.
func TestWriteReadsItsInput(t *testing.T) {
	for raw, want := range map[string]input{
		`{"file_path":"/a","content":"x"}`:     {Path: "/a", Content: "x"},
		`{"content":"","file_path":"/a"}`:      {Path: "/a", Content: ""},
		` {"file_path":"/a","content":"\r\n"}`: {Path: "/a", Content: "\r\n"},
	} {
		got, err := parse([]byte(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	for _, raw := range badInputs {
		_, err := parse([]byte(raw))
		assert.ErrorIs(t, err, errInput, raw)
	}

	const limit = 16
	full, _ := json.Marshal(map[string]string{"file_path": "/a", "content": strings.Repeat("x", limit)})
	_, err := parseWithin(full, limit)
	assert.NoError(t, err, "limit bytes")
	over, _ := json.Marshal(map[string]string{"file_path": "/a", "content": strings.Repeat("x", limit+1)})
	_, err = parseWithin(over, limit)
	assert.ErrorIs(t, err, errInput, "past the limit")
}

// ActionOf is the path and the content as the call named them, and refuses
// what parse refuses.
func TestActionOfReadsThePathAndContent(t *testing.T) {
	got, err := ActionOf(json.RawMessage(`{"file_path":"/Users/ana/a \u202e.yaml","content":"a: 1\r\n"}`), "")
	require.NoError(t, err)
	assert.Equal(t, tools.Action{Write: &tools.WriteAction{Path: "/Users/ana/a \u202e.yaml", Content: "a: 1\r\n"}}, got)

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
		`{"file_path":"/Users/ana/values.yaml","content":"replicas: 2\n"}`: {Write: &tools.WriteAction{Path: "/Users/ana/values.yaml", Content: "replicas: 2\n"}},
		`{"file_path":"/tmp/empty","content":""}`:                          {Write: &tools.WriteAction{Path: "/tmp/empty", Content: ""}},
		`{"content":"a\r\nb","file_path":"/c/Users/ana/x.txt"}`:            {Write: &tools.WriteAction{Path: "/c/Users/ana/x.txt", Content: "a\r\nb"}},
	} {
		got, err := ActionOf(json.RawMessage(raw), "")
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

// The tool names itself and reads a call as ActionOf does, as a write.
func TestWriteNamesItsKind(t *testing.T) {
	raw := json.RawMessage(`{"file_path":"/tmp/a","content":"a"}`)
	tool := &Tool{}
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, tools.ActionWrite, tool.ActionKind())
	got, err := tool.Action(raw, "", false)
	require.NoError(t, err)
	want, err := ActionOf(raw, "")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, tools.ActionWrite, got.Kind())
}

// A file in the chat's workspace is written, though the workspace is under the
// data directory.
func TestWriteReachesTheWorkspace(t *testing.T) {
	tl, rt, _, _ := chat(t)
	path := filepath.Join(tools.WorkspacePath(rt.Dir), "notes", "x.txt")

	text, isError := tl.Run(t.Context(), rt, call(path, "a\n"))
	assert.False(t, isError, text)
	assert.Equal(t, created(path), text)
	assert.Equal(t, "a\n", content(t, path))

	text, isError = tl.Run(t.Context(), rt, call(path, "b\n"))
	assert.False(t, isError, text)
	assert.Equal(t, updated(path), text)
	assert.Equal(t, "b\n", content(t, path))
}

// A write in the chat's workspace runs unasked, whether or not the workspace
// exists yet, and touches nothing on disk to decide.
func TestAWriteInTheWorkspaceAsksNoOne(t *testing.T) {
	tl, rt, _, _ := chat(t)
	tl.write = func(context.Context, tools.Runtime, string, *session.Folder, []byte) (bool, error) {
		t.Fatal("the gate touched the disk")
		return false, nil
	}
	ws := tools.WorkspacePath(rt.Dir)
	for _, path := range []string{filepath.Join(ws, "x.txt"), filepath.Join(ws, "notes", "deep", "x.txt")} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "a")), path)
	}
	assert.NoDirExists(t, ws)
}

// A write outside Kubetail's directories asks as before, a directory named
// workspace included: only the chat's own is written unasked.
func TestAWriteElsewhereStillAsks(t *testing.T) {
	tl, rt, _, data := chat(t)
	for _, path := range []string{
		filepath.Join(t.TempDir(), "x.txt"),
		filepath.Join(filepath.Dir(data), "workspace", "x.txt"),
	} {
		assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(path, "a")), path)
	}
}

// The rest of the data directory stays fenced: the chat's results beside its
// workspace, another chat's workspace, and the app's own files.
func TestTheRestOfTheDataDirectoryStaysFenced(t *testing.T) {
	tl, rt, _, data := chat(t)
	results := rt.Dir.Path()
	for _, path := range []string{
		filepath.Join(results, "out.txt"),
		filepath.Join(filepath.Dir(results), "c2", "work", "x.txt"),
		filepath.Join(data, "app.db"),
	} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "x")), path)
		text, isError := tl.Run(t.Context(), rt, call(path, "x"))
		assert.True(t, isError, path)
		assert.Equal(t, "Write cannot change Kubetail's directories.", text, path)
	}
	assert.NoDirExists(t, filepath.Join(data, "chats"))
}

// The workspace itself is a directory, not a file.
func TestTheWorkspaceItselfIsADirectory(t *testing.T) {
	tl, rt, _, _ := chat(t)

	text, isError := tl.Run(t.Context(), rt, call(tools.WorkspacePath(rt.Dir), "x"))
	assert.True(t, isError)
	assert.Equal(t, "This is a directory, not a file.", text)
}

// The description and the section name the workspace, the one place under the
// data directory Write reaches.
func TestTheOfferNamesTheWorkspace(t *testing.T) {
	tl, _ := tool(t)
	assert.Contains(t, tl.Definition().Description, "Kubetail's own directories cannot be written, but for the chat's workspace.")
	assert.Contains(t, tl.Prompt(), "Write a file outside the chat's workspace only when it is part of what the user asked for in this conversation, never for a file you need only for a moment")
}

// The description says which writes wait for the user: every one but the
// chat's workspace.
func TestTheOfferSaysAWorkspaceWriteDoesNotWait(t *testing.T) {
	tl, _ := tool(t)
	got := tl.Definition().Description
	assert.Contains(t, got, "A write in the chat's workspace does not wait for the user; any other file waits for the user to approve the path and the content.")
	assert.Contains(t, got, "For a file outside the workspace, Read it before you ask")
	assert.NotContains(t, got, "Every write waits")
}

// A workspace that is not a directory is refused, and nothing is written.
func TestAWorkspaceThatIsAFileIsRefused(t *testing.T) {
	tl, rt, _, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(filepath.Dir(ws), 0o700))
	require.NoError(t, os.WriteFile(ws, nil, 0o600))

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(ws, "x.txt"), "x"))
	assert.True(t, isError)
	assert.Equal(t, "Kubetail could not write the file.", text)
}

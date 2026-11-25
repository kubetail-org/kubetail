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

package edit

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
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
)

// tool is Edit fenced around a data directory of the test's own, and that
// directory.
func tool(t *testing.T) (*Tool, string) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Mkdir(data, 0o700))
	tl, err := New(nil, data)
	require.NoError(t, err)
	return tl, data
}

// A tool fenced out of no directory could change Kubetail's files, so New needs
// one.
func TestNewNeedsAFencedDirectory(t *testing.T) {
	_, err := New(nil)
	assert.ErrorIs(t, err, fileguard.ErrNoFence)
}

// The offer is the reference's Edit, and the description and the section tell
// the model to Read first, that the user approves both strings, and to use
// Write for a new file.
func TestTheDefinitionIsTheReferences(t *testing.T) {
	tl, _ := tool(t)
	def := tl.Definition()
	assert.Equal(t, "Edit", def.Name)
	assert.True(t, strings.HasPrefix(def.Description, "Replaces exact text in a file on the user's machine.\n"))
	assert.Contains(t, def.Description, "Needs a Read of the file")
	assert.Contains(t, def.Description, "waits for the user to approve the path and both strings")
	assert.Contains(t, def.Description, "Does not create files: use Write.")
	assert.Contains(t, def.Description, "`[redacted]`")
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.Equal(t, []string{"file_path", "old_string", "new_string"}, schema.Required)
	assert.Len(t, schema.Properties, 4)

	section := tl.Prompt()
	assert.True(t, strings.HasPrefix(section, "## Edit\n"))
	assert.Contains(t, section, "only when the change is part of what the user asked for")
}

// Reading, hashing and writing 8 MiB can pass the turn's default bound, so the
// tool carries its own; it is gated.
func TestEditIsBoundedAndGated(t *testing.T) {
	tl, _ := tool(t)
	var got tools.Tool = tl
	b, ok := got.(tools.Bounded)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, b.CallTimeout(nil))
	_, gated := got.(tools.Gated)
	assert.True(t, gated)
}

// stamps is a chat's file stamps, held by the test.
type stamps map[string]tools.Stamp

func (s stamps) Stamp(path string) (tools.Stamp, bool) { st, ok := s[path]; return st, ok }
func (s stamps) SetStamp(path string, st tools.Stamp)  { s[path] = st }

// chat is Edit, a runtime of a chat of the test's own, the chat's stamps, and
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

// call is the input that replaces old with new in path, once.
func call(path, old, new string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"file_path": path, "old_string": old, "new_string": new})
	return b
}

// callAll is the input that replaces every old with new in path.
func callAll(path, old, new string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"file_path": path, "old_string": old, "new_string": new, "replace_all": true})
	return b
}

// approval is what the tool answers for a call in rt's chat.
func approval(t *testing.T, tl *Tool, rt tools.Runtime, raw json.RawMessage) tools.Approval {
	t.Helper()
	got, err := tl.Approval(t.Context(), rt, raw)
	require.NoError(t, err)
	return got
}

// The gate reads the call by its input alone: a path Abs refuses, one under the
// data directory by name, and the four strings check refuses are never asked
// about, since Run refuses each before touching anything; every other path is
// asked alike, whatever is on disk, and nothing on disk is touched.
func TestEditAsksOnTheNameAlone(t *testing.T) {
	tl, data := tool(t)
	tl.edit = func(context.Context, tools.Runtime, string, *session.Folder, input) (edited, error) {
		t.Fatal("the gate touched the disk")
		return edited{}, nil
	}
	rt := tools.Runtime{Files: stamps{}, Dir: chatDirIn(data)}
	dir := t.TempDir()
	x := filepath.Join(dir, "x.txt")
	require.NoError(t, os.WriteFile(x, []byte("a"), 0o600))
	sep := string(filepath.Separator)

	for _, raw := range []json.RawMessage{
		call("x.txt", "a", "b"),
		call(dir+sep+"."+sep+"x.txt", "a", "b"),
		call(filepath.Join(data, "app.db"), "a", "b"),
		call(x, "", "b"),
		call(x, "a", "a"),
		call(x, "a", "b\x00"),
		call(x, "a", "token: [redacted]"),
	} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, raw), string(raw))
	}
	for _, path := range []string{filepath.Join(dir, "missing.txt"), dir, x} {
		assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(path, "a", "b")), path)
	}

	_, err := tl.Approval(t.Context(), rt, json.RawMessage(`{}`))
	assert.ErrorIs(t, err, errInput, "an input Run refuses shows nothing")
}

// What the gate skips on the input alone, Run refuses in words that say what
// is wrong, and the file is left as it was.
func TestEditRefusesByInputUnasked(t *testing.T) {
	tl, rt, _, data := chat(t)
	x := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(x, []byte("a"), 0o600))
	plain := filepath.Join(t.TempDir(), "y.txt")
	sep := string(filepath.Separator)
	notPlain := filepath.Dir(plain) + sep + "sub" + sep + ".." + sep + "y.txt"

	for raw, want := range map[string]string{
		string(call(notPlain, "a", "b")):                         "Edit takes a path in its plain form. Call again with: " + plain,
		string(call("x.txt", "a", "b")):                          "Edit takes an absolute path.",
		string(call(plain+"\n"+sep+"etc", "a", "b")):             "Edit takes a path of one line, with no control character, of at most 4096 bytes.",
		string(call(filepath.Join(data, "host.json"), "a", "b")): "Edit cannot change Kubetail's directories.",
		string(call(x, "", "b")):                                 "old_string is empty. Edit does not create files: use Write.",
		string(call(x, "a", "a")):                                "old_string and new_string are the same, so there is nothing to change.",
		string(call(x, "a", "b\x00")):                            "Edit writes text only, and new_string holds a NUL byte.",
		string(call(x, "a", "token: [redacted]")):                "new_string holds [redacted], the mark Read shows in place of a secret. Edit would write the mark as text. Leave the redacted text out of the edit.",
	} {
		text, isError := tl.Run(t.Context(), rt, json.RawMessage(raw))
		assert.True(t, isError, raw)
		assert.Equal(t, want, text, raw)
	}
	assert.Equal(t, "a", content(t, x))
}

// An input parse refuses runs nothing.
func TestEditRefusesABadInput(t *testing.T) {
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

// updated is Edit's answer for one replacement, as the reference words it.
func updated(path string) string {
	return "The file " + path + " has been updated successfully. (file state is current in your context — no need to Read it back)"
}

// The one match is replaced and nothing else in the file moves.
func TestEditReplacesOneMatch(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "replicas: 1\nimage: app:1\n")

	text, isError := tl.Run(t.Context(), rt, call(path, "replicas: 1", "replicas: 3"))
	assert.False(t, isError, text)
	assert.Equal(t, updated(path), text)
	assert.Equal(t, "replicas: 3\nimage: app:1\n", content(t, path))
}

// An edit leaves no temporary file.
func TestEditLeavesNoTemporaryFile(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "old\n")

	_, isError := tl.Run(t.Context(), rt, call(path, "old", "new"))
	require.False(t, isError)
	assert.Equal(t, "new\n", content(t, path))
	assert.Equal(t, []string{"x.txt"}, names(t, filepath.Dir(path)))
}

// An edit needs a stamp from this chat that matches the file, Whole or not;
// each refusal says how to get one, and changes nothing.
func TestEditNeedsAMatchingStamp(t *testing.T) {
	tl, rt, st, _ := chat(t)
	unread := filepath.Join(t.TempDir(), "x.txt")
	require.NoError(t, os.WriteFile(unread, []byte("a\n"), 0o600))
	changed := file(t, st, "a\n")
	require.NoError(t, os.WriteFile(changed, []byte("a\nb\n"), 0o600))

	for path, want := range map[string]string{
		unread:  "Read the file before editing it.",
		changed: "The file changed since you last read it. Read it again before editing it.",
	} {
		before := content(t, path)
		text, isError := tl.Run(t.Context(), rt, call(path, "a", "c"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
		assert.Equal(t, before, content(t, path), path)
	}

	part := file(t, st, "a\n")
	st[part] = tools.Stamp{Sum: st[part].Sum}
	text, isError := tl.Run(t.Context(), rt, call(part, "a", "c"))
	assert.False(t, isError, text)
	assert.Equal(t, "c\n", content(t, part))
}

// Without replace_all a string that matches twice is refused with the count,
// and one whose single match another overlaps is refused as not unique; the
// file is left as it was.
func TestEditRefusesAMatchThatIsNotUnique(t *testing.T) {
	tl, rt, st, _ := chat(t)
	for _, c := range []struct{ body, old, want string }{
		{"a: 1\na: 1\na: 1\n", "a: 1", "Found 3 matches of the string to replace, but replace_all is false. To replace all occurrences, set replace_all to true. To replace only one occurrence, please provide more context to uniquely identify the instance.\nString: a: 1"},
		{"x     y\n", "    ", "The string to replace matches in overlapping places, so it is not unique. Please provide more context to uniquely identify the instance.\nString:     "},
	} {
		path := file(t, st, c.body)
		text, isError := tl.Run(t.Context(), rt, call(path, c.old, "b"))
		assert.True(t, isError, c.body)
		assert.Equal(t, c.want, text, c.body)
		assert.Equal(t, c.body, content(t, path), c.body)
	}
}

// A string that is not in the file is refused and echoed, cut as Read cuts a
// line, and the file is left as it was.
func TestEditRefusesNoMatch(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "a: 1\n")
	for old, echoed := range map[string]string{
		"b: 2":                    "b: 2",
		strings.Repeat("é", 2001): strings.Repeat("é", 2000) + "…",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, old, "c"))
		assert.True(t, isError)
		assert.Equal(t, "String to replace not found in file.\nString: "+echoed, text)
	}
	assert.Equal(t, "a: 1\n", content(t, path))
}

// A miss says why when the cause can be named: text Read redacted, whether
// copied with the mark or with the mark trimmed off what a rule rewrote beside
// it; line endings the file mixes; a line Read cut.
func TestEditNoMatchNamesTheCause(t *testing.T) {
	tl, rt, st, _ := chat(t)
	const (
		redaction = " Read redacts secrets, so the text it replaced cannot be matched. Choose a string that avoids it."
		mixed     = " The file mixes line endings, which Edit matches exactly. Choose a string within one line."
		cut       = " Read cuts lines longer than 2000 characters, so the text after the cut cannot be matched. Choose a string before it."
	)
	long := strings.Repeat("x", 2001)
	for _, c := range []struct{ body, old, hint string }{
		{"token: abc\n", "token: [redacted]", redaction},
		{"Authorization=abc\n", "Authorization: ", redaction},
		{"a: 1\r\nb: 2\n", "a: 1\nb: 2", mixed},
		{long + "y\n", strings.Repeat("x", 2000) + "…", cut},
		{"a: 1\n", "a: 2…", ""},
		{"a: 1\r\nb: 2\n", "a: 3", ""},
	} {
		path := file(t, st, c.body)
		text, isError := tl.Run(t.Context(), rt, call(path, c.old, "z"))
		assert.True(t, isError, c.old)
		assert.Equal(t, "String to replace not found in file."+c.hint+"\nString: "+echo(c.old), text, c.old)
	}
}

// In a file whose every line break is CRLF, Read showed LF, so the model's LF
// strings match and are written as CRLF, an added line included.
func TestEditMatchesLFInACRLFFile(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "a: 1\r\nb: 2\r\n")
	text, isError := tl.Run(t.Context(), rt, call(path, "a: 1\nb: 2", "a: 1\nc: 3\nb: 2"))
	assert.False(t, isError, text)
	assert.Equal(t, "a: 1\r\nc: 3\r\nb: 2\r\n", content(t, path))

	// A \n already after a \r is left as it is.
	text, isError = tl.Run(t.Context(), rt, call(path, "c: 3\r\n", "d: 4\n"))
	assert.False(t, isError, text)
	assert.Equal(t, "a: 1\r\nd: 4\r\nb: 2\r\n", content(t, path))
}

// Two strings the conversion makes equal would change nothing, so the call is
// refused as a no-op is.
func TestEditRefusesANoOpAfterTheFold(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "x\r\ny\r\n")
	text, isError := tl.Run(t.Context(), rt, call(path, "x\n", "x\r\n"))
	assert.True(t, isError)
	assert.Equal(t, "old_string and new_string are the same, so there is nothing to change.", text)
	assert.Equal(t, "x\r\ny\r\n", content(t, path))
}

// A file that mixes line endings is matched exactly, and so is whitespace: a
// tab never matches spaces.
func TestEditFoldsNothingElse(t *testing.T) {
	tl, rt, st, _ := chat(t)
	mixed := file(t, st, "a: 1\r\nb: 2\nc: 3\r\n")
	text, isError := tl.Run(t.Context(), rt, call(mixed, "b: 2\n", "b: 5\n"))
	assert.False(t, isError, text)
	assert.Equal(t, "a: 1\r\nb: 5\nc: 3\r\n", content(t, mixed))

	tabs := file(t, st, "\tx: 1\n")
	text, isError = tl.Run(t.Context(), rt, call(tabs, "    x: 1", "    x: 2"))
	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(text, "String to replace not found in file."), text)
	assert.Equal(t, "\tx: 1\n", content(t, tabs))
}

// replace_all replaces every non-overlapping occurrence and names the count;
// one occurrence answers as a single edit does.
func TestEditReplacesAll(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, "image: app:1\nsidecar: app:1\n")
	text, isError := tl.Run(t.Context(), rt, callAll(path, "app:1", "app:2"))
	assert.False(t, isError, text)
	assert.Equal(t, "The file "+path+" has been updated. All 2 occurrences were successfully replaced. (file state is current in your context — no need to Read it back)", text)
	assert.Equal(t, "image: app:2\nsidecar: app:2\n", content(t, path))

	one := file(t, st, "image: app:1\n")
	text, isError = tl.Run(t.Context(), rt, callAll(one, "app:1", "app:2"))
	assert.False(t, isError, text)
	assert.Equal(t, updated(one), text)
	assert.Equal(t, "image: app:2\n", content(t, one))
}

// An edit whose result would pass FileLimit is refused from the count, before
// the result is built: here the product is terabytes, so a build before the
// check would not return. An edit that lands at FileLimit exactly is made.
func TestEditRefusesAnOversizedResultBeforeBuildingIt(t *testing.T) {
	tl, rt, st, _ := chat(t)
	path := file(t, st, strings.Repeat("a", 8<<20))
	text, isError := tl.Run(t.Context(), rt, callAll(path, "a", strings.Repeat("b", 1<<20)))
	assert.True(t, isError)
	assert.Equal(t, "The edited file would be larger than 8 MiB.", text)

	exact := file(t, st, strings.Repeat("a", tools.FileLimit-2)+"xy")
	text, isError = tl.Run(t.Context(), rt, call(exact, "xy", "xyz"))
	assert.True(t, isError)
	assert.Equal(t, "The edited file would be larger than 8 MiB.", text)
	text, isError = tl.Run(t.Context(), rt, call(exact, "x", "z"))
	assert.False(t, isError, text)
}

// The uniqueness check is linear: past the count, one scan finds the match
// and one probes for an overlap, however many places old overlaps itself.
// Here it overlaps at every one of 2¹⁹ offsets.
func TestEditUniquenessIsLinear(t *testing.T) {
	text, old := strings.Repeat("a", 1<<20-1), strings.Repeat("a", 1<<19)
	scans := 0
	index := func(s, substr string) int {
		scans++
		if scans > 2 {
			t.Fatalf("scan %d: the overlap check walks the matches", scans)
		}
		return strings.Index(s, substr)
	}

	n, err := matches(text, old, false, index)
	assert.ErrorIs(t, err, errOverlap)
	assert.Equal(t, 1, n)
}

// An edit the call answers as done stamps the new bytes, keeping the matched
// stamp's Whole when the strings went in as given, since the model then knows
// every byte that changed; after the CRLF conversion it is not Whole, since
// the model sent LF where the file now holds CRLF. A call answered as
// cancelled stamps nothing.
func TestEditSetsTheStamp(t *testing.T) {
	tl, rt, st, _ := chat(t)
	whole := file(t, st, "a: 1\n")
	_, isError := tl.Run(t.Context(), rt, call(whole, "a: 1", "a: 2"))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a: 2\n")), Whole: true}, st[whole])

	part := file(t, st, "a: 1\n")
	st[part] = tools.Stamp{Sum: st[part].Sum}
	_, isError = tl.Run(t.Context(), rt, call(part, "a: 1", "a: 2"))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a: 2\n"))}, st[part])

	folded := file(t, st, "a: 1\r\nb: 2\r\n")
	_, isError = tl.Run(t.Context(), rt, call(folded, "a: 1\nb: 2", "a: 1\nb: 3"))
	require.False(t, isError)
	assert.Equal(t, tools.Stamp{Sum: sha256.Sum256([]byte("a: 1\r\nb: 3\r\n"))}, st[folded])

	oneLine := file(t, st, "a: 1\r\nb: 2\r\n")
	_, isError = tl.Run(t.Context(), rt, call(oneLine, "b: 2", "b: 3"))
	require.False(t, isError)
	assert.True(t, st[oneLine].Whole, "a string with no newline is not changed by the conversion")

	// The cancel answers before the edit returns, so the test waits for it: an
	// edit still statting the temp tree keeps Windows from removing it.
	real := tl.edit
	returned := make(chan struct{})
	tl.edit = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error) {
		defer close(returned)
		return real(ctx, rt, path, folder, in)
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	other := file(t, st, "c\n")
	before := st[other]
	text, isError := tl.Run(ended, rt, call(other, "c", "d"))
	assert.True(t, isError)
	assert.Equal(t, cancelled, text)
	testutil.Wait(t, returned, "the edit")
	assert.Equal(t, before, st[other])
}

// A missing file, a directory, a binary and a file past the limit are refused
// in their own words, and nothing is made.
func TestEditRefusesWhatItDoesNotChange(t *testing.T) {
	tl, rt, st, _ := chat(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	bin := file(t, st, "ELF\x00\x01")
	big := file(t, st, "")
	require.NoError(t, os.Truncate(big, tools.FileLimit+1))

	for path, want := range map[string]string{
		missing: "The file does not exist. Use Write to create it.",
		dir:     "This is a directory, not a file.",
		bin:     "The file looks binary. Edit changes text files only.",
		big:     "The file is larger than 8 MiB, which Edit does not change.",
	} {
		text, isError := tl.Run(t.Context(), rt, call(path, "ELF", "x"))
		assert.True(t, isError, path)
		assert.Equal(t, want, text, path)
	}
	assert.NoFileExists(t, missing)
}

// What fileguard refuses is worded by the tool, never with a Go error's text.
func TestEditWordsWhatFileguardRefuses(t *testing.T) {
	tl, data := tool(t)
	rt := tools.Runtime{Files: stamps{}, Dir: chatDirIn(data)}
	for err, want := range map[error]string{
		fileguard.ErrOtherUser:                  "This file belongs to another user. Kubetail does not replace it.",
		fileguard.ErrOtherGroup:                 "This file belongs to a group you are not in. Kubetail does not replace it.",
		fileguard.ErrReadOnly:                   "Kubetail cannot write this file.",
		fileguard.ErrDirReadOnly{Prefix: "/d"}:  "Kubetail cannot replace files in this directory.",
		fileguard.ErrUnresolved:                 "Kubetail cannot write this file.",
		fileguard.ErrMissing:                    "The file does not exist. Use Write to create it.",
		fileguard.ErrLink{Target: "/etc/hosts"}: "This is a symbolic link. Edit its target instead: /etc/hosts",
		fileguard.ErrDirectory:                  "This is a directory, not a file.",
		fileguard.ErrNotRegular:                 "This is not a regular file.",
		fileguard.ErrNotLocal:                   "Edit changes files on this machine only.",
		fileguard.ErrTooLarge:                   "The file is larger than 8 MiB, which Edit does not change.",
		fileguard.ErrCancelled:                  cancelled,
		fileguard.ErrReplace:                    "Kubetail could not replace the file.",
		fileguard.ErrWrite:                      "Kubetail could not write the file.",
		errUnreadable:                           "Kubetail cannot read this file.",
		errCannotWrite:                          "Kubetail cannot write this file.",
		errors.New("disk full"):                 "Kubetail could not write the file.",
	} {
		tl.edit = func(context.Context, tools.Runtime, string, *session.Folder, input) (edited, error) {
			return edited{}, err
		}
		text, isError := tl.Run(t.Context(), rt, call(filepath.Join(t.TempDir(), "x.txt"), "a", "b"))
		assert.True(t, isError, err.Error())
		assert.Equal(t, want, text, err.Error())
	}
}

// A stat on a dead mount can block in a syscall no context reaches, so the call
// answers its cancel while the edit still blocks; when the edit wakes, it
// changes nothing on disk, and nothing is stamped.
func TestAStuckEditHonoursTheCancel(t *testing.T) {
	tl, data := tool(t)
	real := tl.edit
	release := make(chan struct{})
	returned := make(chan struct{})
	tl.edit = func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error) {
		defer close(returned)
		<-release
		return real(ctx, rt, path, folder, in)
	}
	st := stamps{}
	rt := tools.Runtime{Files: st, Dir: chatDirIn(data)}
	path := file(t, st, "a\n")
	before := st[path]
	ctx, cancel := context.WithCancel(t.Context())

	var text string
	var isError bool
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		text, isError = tl.Run(ctx, rt, call(path, "a", "b"))
	}()
	cancel()
	testutil.Wait(t, ran, "the cancel's answer")
	assert.True(t, isError)
	assert.Equal(t, cancelled, text)

	close(release)
	testutil.Wait(t, returned, "the edit")
	assert.Equal(t, "a\n", content(t, path), "nothing is changed")
	assert.Equal(t, before, st[path], "a cancelled call stamps nothing")
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

// content is what path holds.
func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// badInputs are inputs parse refuses.
var badInputs = []string{
	`{}`,
	`{"file_path":"/a","old_string":"x"}`,
	`{"file_path":"/a","new_string":"y"}`,
	`{"old_string":"x","new_string":"y"}`,
	`{"file_path":"","old_string":"x","new_string":"y"}`,
	`{"file_path":null,"old_string":"x","new_string":"y"}`,
	`{"file_path":"/a","old_string":null,"new_string":"y"}`,
	`{"file_path":"/a","old_string":"x","new_string":1}`,
	`{"file_path":"/a","old_string":"x","new_string":"y","replace_all":null}`,
	`{"file_path":"/a","old_string":"x","new_string":"y","replace_all":"true"}`,
	`{"File_path":"/a","old_string":"x","new_string":"y"}`,
	`{"file_path":"/a","file_path":"/b","old_string":"x","new_string":"y"}`,
	`{"file_path":"/a","old_string":"x","old_string":"z","new_string":"y"}`,
	`{"file_path":"/a","old_string":"x","new_string":"y","replace_all":true,"replace_all":false}`,
	`{"file_path":"/a","old_string":"x","new_string":"y","extra":1}`,
	`{"file_path":"/a","old_string":"x","new_string":"y"} 1`,
	`{"file_path":"/a","old_string":"x","new_string":"y"`,
	`{"file_path":`,
	`{1:2}`,
	`[]`,
	`not json`,
}

// An Edit input is a file_path, an old_string and a new_string, each key
// spelled exactly and once, and an optional boolean replace_all. The strings
// may be empty or equal here, since check refuses those in words; each is at
// most FileLimit bytes.
func TestEditReadsItsInput(t *testing.T) {
	for raw, want := range map[string]input{
		`{"file_path":"/a","old_string":"x","new_string":"y"}`:                        {Path: "/a", Old: "x", New: "y"},
		`{"new_string":"","old_string":"","file_path":"/a"}`:                          {Path: "/a"},
		`{"file_path":"/a","old_string":"x","new_string":"x","replace_all":true}`:     {Path: "/a", Old: "x", New: "x", All: true},
		` {"file_path":"/a","old_string":"\r\n","new_string":"","replace_all":false}`: {Path: "/a", Old: "\r\n"},
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
	full := strings.Repeat("x", limit)
	over := full + "x"
	for _, c := range []struct {
		old, new string
		ok       bool
	}{{full, "y", true}, {"y", full, true}, {over, "y", false}, {"y", over, false}} {
		raw, _ := json.Marshal(map[string]string{"file_path": "/a", "old_string": c.old, "new_string": c.new})
		_, err := parseWithin(raw, limit)
		if c.ok {
			assert.NoError(t, err, "limit bytes")
		} else {
			assert.ErrorIs(t, err, errInput, "past the limit")
		}
	}
}

// ActionOf is the path and both strings as the call named them, and refuses
// what parse refuses.
func TestActionOfReadsTheCall(t *testing.T) {
	got, err := ActionOf(json.RawMessage(`{"file_path":"/Users/ana/a \u202e.yaml","old_string":"a: 1\r\n","new_string":"a: 2\r\n","replace_all":true}`), "")
	require.NoError(t, err)
	assert.Equal(t, tools.Action{Edit: &tools.EditAction{Path: "/Users/ana/a \u202e.yaml", OldString: "a: 1\r\n", NewString: "a: 2\r\n", ReplaceAll: true}}, got)

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
		`{"file_path":"/Users/ana/values.yaml","old_string":"replicas: 1","new_string":"replicas: 2"}`: {Edit: &tools.EditAction{Path: "/Users/ana/values.yaml", OldString: "replicas: 1", NewString: "replicas: 2"}},
		`{"file_path":"/tmp/x","old_string":"a\n","new_string":"","replace_all":true}`:                 {Edit: &tools.EditAction{Path: "/tmp/x", OldString: "a\n", ReplaceAll: true}},
		`{"replace_all":false,"new_string":"b","old_string":"a","file_path":"/c/Users/ana/x.txt"}`:     {Edit: &tools.EditAction{Path: "/c/Users/ana/x.txt", OldString: "a", NewString: "b"}},
	} {
		got, err := ActionOf(json.RawMessage(raw), "")
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

// The tool names itself and reads a call as ActionOf does, as an edit.
func TestEditNamesItsKind(t *testing.T) {
	raw := json.RawMessage(`{"file_path":"/tmp/a","old_string":"a","new_string":"b"}`)
	tool := &Tool{}
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, tools.ActionEdit, tool.ActionKind())
	got, err := tool.Action(raw, "", false)
	require.NoError(t, err)
	want, err := ActionOf(raw, "")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, tools.ActionEdit, got.Kind())
}

// A file in the chat's workspace is edited, though the workspace is under the
// data directory.
func TestEditReachesTheWorkspace(t *testing.T) {
	tl, rt, st, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "notes"), 0o700))
	path := filepath.Join(ws, "notes", "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\n"), 0o600))
	st[path] = tools.Stamp{Sum: sha256.Sum256([]byte("a\n")), Whole: true}

	text, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
	assert.False(t, isError, text)
	assert.Equal(t, updated(path), text)
	assert.Equal(t, "b\n", content(t, path))
}

// An edit in the chat's workspace runs unasked, and touches nothing on disk
// to decide.
func TestAWriteInTheWorkspaceAsksNoOne(t *testing.T) {
	tl, rt, _, _ := chat(t)
	tl.edit = func(context.Context, tools.Runtime, string, *session.Folder, input) (edited, error) {
		t.Fatal("the gate touched the disk")
		return edited{}, nil
	}
	ws := tools.WorkspacePath(rt.Dir)
	for _, path := range []string{filepath.Join(ws, "x.txt"), filepath.Join(ws, "notes", "x.txt")} {
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "a", "b")), path)
	}
	assert.NoDirExists(t, ws)
}

// An edit outside Kubetail's directories asks as before, a directory named
// workspace included: only the chat's own is changed unasked.
func TestAWriteElsewhereStillAsks(t *testing.T) {
	tl, rt, _, data := chat(t)
	for _, path := range []string{
		filepath.Join(t.TempDir(), "x.txt"),
		filepath.Join(filepath.Dir(data), "workspace", "x.txt"),
	} {
		assert.Equal(t, tools.Approval{}, approval(t, tl, rt, call(path, "a", "b")), path)
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
		assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "a", "b")), path)
		text, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
		assert.True(t, isError, path)
		assert.Equal(t, "Edit cannot change Kubetail's directories.", text, path)
	}
}

// The cache directory is fenced as the data directory is.
func TestEditRefusesTheCacheDir(t *testing.T) {
	_, rt, _, data := chat(t)
	cache := t.TempDir()
	tl, err := New(nil, data, cache)
	require.NoError(t, err)
	path := filepath.Join(cache, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\n"), 0o600))

	assert.Equal(t, tools.Approval{Skip: true}, approval(t, tl, rt, call(path, "a", "b")))
	text, isError := tl.Run(t.Context(), rt, call(path, "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "Edit cannot change Kubetail's directories.", text)
	assert.Equal(t, "a\n", content(t, path))
}

// The workspace itself is a directory, and an edit before it exists finds no
// file rather than failing a write.
func TestTheWorkspaceItselfIsADirectory(t *testing.T) {
	tl, rt, _, _ := chat(t)
	ws := tools.WorkspacePath(rt.Dir)

	text, isError := tl.Run(t.Context(), rt, call(filepath.Join(ws, "x.txt"), "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "The file does not exist. Use Write to create it.", text)
	assert.NoDirExists(t, ws, "an edit makes no workspace")

	require.NoError(t, os.MkdirAll(ws, 0o700))
	text, isError = tl.Run(t.Context(), rt, call(ws, "a", "b"))
	assert.True(t, isError)
	assert.Equal(t, "This is a directory, not a file.", text)
}

// A file a command left in the workspace is the model's to Read, then Edit.
func TestEditChangesAFileACommandMade(t *testing.T) {
	tl, rt, _, data := chat(t)
	reader, err := read.New(nil, data)
	require.NoError(t, err)
	ws := tools.WorkspacePath(rt.Dir)
	require.NoError(t, os.MkdirAll(ws, 0o700))
	path := filepath.Join(ws, "values.yaml")
	require.NoError(t, os.WriteFile(path, []byte("replicas: 1\n"), 0o600))

	b, _ := json.Marshal(map[string]string{"file_path": path})
	text, isError := reader.Run(t.Context(), rt, b)
	require.False(t, isError, text)
	text, isError = tl.Run(t.Context(), rt, call(path, "replicas: 1", "replicas: 3"))
	assert.False(t, isError, text)
	assert.Equal(t, "replicas: 3\n", content(t, path))
}

// The description names the workspace, the one place under the data directory
// Edit reaches.
func TestTheOfferNamesTheWorkspace(t *testing.T) {
	tl, _ := tool(t)
	assert.Contains(t, tl.Definition().Description, "Kubetail's own directories cannot be changed, but for the chat's workspace.")
}

// The description and the section say which edits wait for the user: every
// one but the chat's workspace.
func TestTheOfferSaysAWorkspaceEditDoesNotWait(t *testing.T) {
	tl, _ := tool(t)
	got := tl.Definition().Description
	assert.Contains(t, got, "An edit in the chat's workspace does not wait for the user; any other file waits for the user to approve the path and both strings.")
	assert.Contains(t, got, "For a file outside the workspace, Read it before you ask")
	assert.NotContains(t, got, "Every edit waits")
	assert.Contains(t, tl.Prompt(), "Edit a file outside the chat's workspace only when")
}

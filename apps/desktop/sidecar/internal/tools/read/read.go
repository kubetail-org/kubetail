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

// Package read is the tool that reads a text file: one the sidecar saved for a
// chat, such as a command's full output, without asking, and any other the user
// approves. It opens nothing else under Kubetail's directories, and what it
// returns is redacted, whoever wrote the file.
package read

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

//go:embed prompts/read.md
var prompt string

// description is the tool description the model reads with the schema: the
// reference's Read, narrowed to plain absolute paths and text.
//
//go:embed prompts/description.md
var description string

// inputSchema is the reference's Read's file_path, offset and limit.
//
//go:embed prompts/schema.json
var inputSchema []byte

const (
	// defaultLimit is how many lines a call reads when it names no limit.
	defaultLimit = 2000
	// callTimeout is the loop's bound on one call: above the turn's default, which
	// reading and redacting a file of tools.FileLimit bytes can pass.
	callTimeout = 30 * time.Second
)

// notHere answers every path in Kubetail's directories the chat's directory does
// not hold as a readable file, in the same words whatever the reason, so a refusal
// says nothing about a path there.
const notHere = "Read cannot open Kubetail's directories, except the files saved for this conversation."

//lint:ignore ST1005 the error's text is notHere, the refusal's own sentence.
var errNotHere = errors.New(notHere)

// cancelled answers a call whose context ended; the loop writes its own refusal
// in its place.
const cancelled = `{"error":"cancelled"}`

// Name is the name Read is offered under.
const Name = "Read"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "read/v1"

var _ interface {
	tools.Custom
	tools.ApprovedRunner
	tools.Bounded
} = (*Tool)(nil)

// Tool reads the files of the chat a call runs in, any in a folder the
// session was granted, and any other file the user approves outside fence.
type Tool struct {
	fence fileguard.Fence
	// fetch is the file work of a read outside the chat's directory: the fence's check
	// on disk, the open and the read. A test swaps in one that blocks.
	fetch func(path string) ([]byte, error)
	// fetchGranted is the file work of a read in a granted folder: the walk,
	// the open and the read. A test swaps in one that blocks.
	fetchGranted func(folder session.Folder, path string) ([]byte, error)
}

var (
	_ tools.Gated   = (*Tool)(nil)
	_ tools.Bounded = (*Tool)(nil)
)

// New is the tool, which opens nothing under fenced, Kubetail's directories, but
// a chat's directory, and reads a granted folder unasked but for what hidden
// answers the sandbox keeps shut there. None is fileguard.ErrNoFence.
func New(hidden func() (never, closed []string), fenced ...string) (*Tool, error) {
	fence, err := fileguard.NewFence(fenced...)
	if err != nil {
		return nil, err
	}
	fence = fence.WithHidden(hidden)
	return &Tool{
		fence:        fence,
		fetch:        func(path string) ([]byte, error) { return readFile(fence, path) },
		fetchGranted: func(folder session.Folder, path string) ([]byte, error) { return readGranted(fence, folder, path) },
	}, nil
}

// Definition is the offer: a function named Read, run by the sidecar.
func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

// Prompt is the tool's section of the system prompt.
func (t *Tool) Prompt() string { return prompt }

// CallTimeout is the loop's bound on one call, whatever it asks for.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionRead }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

// ActionOf is the file a call reads, through the parse Run uses, as the call
// named it. Read runs nowhere, so cwd is never set.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{Read: &tools.ReadAction{Path: in.Path}}, nil
}

// Approval classifies the path: the user is asked about any path but one Run
// refuses by its name, one in the chat's directory, and one in a folder the
// session was granted that the sandbox does not keep shut there, which names
// the folder Run walks. Read runs nowhere, so no Cwd.
func (t *Tool) Approval(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (tools.Approval, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Approval{}, err
	}
	path, err := fileguard.Abs(in.Path)
	if err != nil || t.fence.Named(path) {
		return tools.Approval{Skip: true}, nil
	}
	if _, ok := inChatDir(rt.Dir, path); ok {
		return tools.Approval{Skip: true}, nil
	}
	granted, err := rt.Session.GrantedFolders(ctx)
	if err != nil {
		return tools.Approval{}, tools.FoldersUnread(Name, err)
	}
	if folder, ok := t.fence.Granted(ctx, granted, path); ok {
		return tools.Approval{Skip: true, Folder: &folder}, nil
	}
	return tools.Approval{}, nil
}

// inChatDir is path, as Abs spells it, relative to results, when it is under
// them.
func inChatDir(dir tools.ChatDir, path string) (string, bool) {
	return fileguard.Under(dir.Path(), path)
}

// Run reads the range a call asks for, once approved.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	return t.RunApproved(ctx, rt, raw, tools.Approval{})
}

// RunApproved reads the range a call asks for: for a call its gate skipped for
// a granted folder, through the walk of that folder alone, whatever the
// session's folders say now; else of a file in the chat's directory, or of one
// outside Kubetail's directories.
func (t *Tool) RunApproved(ctx context.Context, rt tools.Runtime, raw json.RawMessage, a tools.Approval) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return `{"error":"bad-input"}`, true
	}
	path, err := fileguard.Abs(in.Path)
	if err != nil {
		return refusal(err), true
	}
	if a.Folder != nil {
		folder := *a.Folder
		return t.readOutside(ctx, rt, in, path, func() ([]byte, error) { return t.fetchGranted(folder, path) })
	}
	if rel, ok := inChatDir(rt.Dir, path); ok {
		b, err := load(rt.Dir, rel)
		if err != nil {
			return notHere, true
		}
		out, whole, ok := present(ctx, b, in)
		// Write and Edit change a workspace file only once it has been read.
		if _, inWorkspace := fileguard.Under(tools.WorkspacePath(rt.Dir), path); ok && inWorkspace {
			stamp(rt.Files, path, b, whole)
		}
		return out, !ok
	}
	if t.fence.Named(path) {
		return notHere, true
	}
	return t.readOutside(ctx, rt, in, path, func() ([]byte, error) { return t.fetch(path) })
}

// readOutside is the range in asks for of the file fetch reads, outside the
// chat's directory, stamped for Write and Edit.
func (t *Tool) readOutside(ctx context.Context, rt tools.Runtime, in input, path string, fetch func() ([]byte, error)) (string, bool) {
	b, err := fetchWithin(ctx, fetch)
	switch {
	case ctx.Err() != nil:
		return cancelled, true
	case errors.Is(err, errNotHere):
		return notHere, true
	case err != nil:
		return refusal(err), true
	case fileguard.Binary(b):
		return "The file looks binary. Read opens text files only.", true
	}
	out, whole, ok := present(ctx, b, in)
	if ok {
		stamp(rt.Files, path, b, whole)
	}
	return out, !ok
}

// stamp records what this read saw of path. A read never takes back what an
// earlier one showed: the same bytes seen whole before stay Whole.
func stamp(files tools.FileStamps, path string, b []byte, whole bool) {
	sum := sha256.Sum256(b)
	if prev, ok := files.Stamp(path); ok && prev.Sum == sum && prev.Whole {
		whole = true
	}
	files.SetStamp(path, tools.Stamp{Sum: sum, Whole: whole})
}

// present is the range a call asks for of the file's bytes, redacted and
// numbered, and whether that showed every byte as it is. ok is false when the
// context ended, and out is then cancelled.
func present(ctx context.Context, b []byte, in input) (out string, whole, ok bool) {
	// The redaction runs to its end, so the cancel is checked on either side.
	if ctx.Err() != nil {
		return cancelled, false, false
	}
	// Whole, before the range: a key or a multi-line field must not be cut in
	// half before the rules see it.
	text := safe.Redact(string(b))
	if ctx.Err() != nil { // coverage:ignore only a cancel landing during the redaction reaches it
		return cancelled, false, false
	}
	out, whole = numbered(text, in.Offset, in.Limit)
	return out, whole && text == string(b), true
}

// fetchWithin runs fetch where the call can leave it: a stat or open on a stale
// network mount, or on a cloud file being fetched, blocks in a syscall no
// context reaches. On the context's end it answers at once and the goroutine
// is left to return whenever the filesystem does, closing what it opened; what
// it hands back then is dropped.
func fetchWithin(ctx context.Context, fetch func() ([]byte, error)) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := fetch()
		done <- result{b, err}
	}()
	select {
	case r := <-done:
		return r.b, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readFile is the file at path, outside fence by name, when it is outside it
// on disk too and Read opens it.
func readFile(fence fileguard.Fence, path string) ([]byte, error) {
	held, err := fence.Holds(path)
	if err != nil {
		return nil, err
	}
	if held {
		return nil, errNotHere
	}
	f, err := fileguard.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return fileguard.ReadAll(f)
}

// readGranted is the file at path in folder, reached by the walk.
func readGranted(fence fileguard.Fence, folder session.Folder, path string) ([]byte, error) {
	file, err := fence.Walk(folder, path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return fileguard.ReadAll(f)
}

// load is the file at rel under the chat's directory when it holds it as a
// regular file of at most tools.FileLimit bytes. Every step goes through the
// chat's root,
// which refuses a link that leads out of it.
func load(dir tools.ChatDir, rel string) ([]byte, error) {
	root, err := dir.Root(false)
	if err != nil {
		return nil, errNotHere
	}
	defer root.Close()
	f, err := root.OpenFile(rel, fileguard.OpenFlags, 0)
	if err != nil {
		return nil, errNotHere
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || fileguard.Check(info) != nil {
		return nil, errNotHere
	}
	b, err := fileguard.ReadAll(f)
	if err != nil { // coverage:ignore a regular file just opened fails to read only on an I/O error
		return nil, errNotHere
	}
	return b, nil
}

// refusal is what the model reads for a path Read will not open, outside
// Kubetail's directories: the user approved it, or it was never a path.
func refusal(err error) string {
	var (
		notPlain fileguard.ErrNotPlain
		link     fileguard.ErrLink
	)
	switch {
	case errors.Is(err, fileguard.ErrNotOneLine):
		return "Read takes a path of one line, with no control character, of at most 4096 bytes."
	case errors.As(err, &notPlain):
		return "Read takes a path in its plain form. Call again with: " + notPlain.Plain
	case errors.Is(err, fileguard.ErrNotAbs):
		return "Read takes an absolute path."
	case errors.Is(err, fileguard.ErrNotLocal):
		return "Read opens files on this machine only."
	case errors.Is(err, fileguard.ErrMissing):
		return "The file does not exist."
	case errors.Is(err, fileguard.ErrDirectory):
		return "This is a directory, not a file."
	case errors.As(err, &link):
		return "This is a symbolic link. Read its target instead: " + link.Target
	case errors.Is(err, fileguard.ErrNotRegular):
		return "This is not a regular file."
	case errors.Is(err, fileguard.ErrTooLarge):
		return "The file is larger than 8 MiB, which Read does not open."
	}
	if text, ok := fileguard.WalkRefusal(err); ok {
		return text
	}
	return "Kubetail cannot read this file."
}

var errInput = errors.New(`read input is not {"file_path": <non-empty string>, "offset"?: <integer ≥ 1>, "limit"?: <integer ≥ 1>}`)

// input is one call's arguments.
type input struct {
	Path   string
	Offset int // 1-based; 1 when the call names none
	Limit  int // defaultLimit when the call names none
}

// wholeRE is a JSON number written as a whole number of at least 1.
var wholeRE = regexp.MustCompile(`^[1-9][0-9]*$`)

// parse reads an object whose keys are file_path, offset and limit, each spelled
// exactly and at most once, with nothing after it, as bash's parse does. Each
// value's type is checked off its token, since a typed decode takes null as the
// zero value.
func parse(raw json.RawMessage) (input, error) {
	in := input{Offset: 1, Limit: defaultLimit}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return input{}, errInput
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return input{}, errInput
		}
		name, _ := key.(string)
		if seen[name] {
			return input{}, errInput
		}
		seen[name] = true
		val, err := dec.Token()
		if err != nil {
			return input{}, errInput
		}
		var ok bool
		switch name {
		case "file_path":
			in.Path, ok = val.(string)
		case "offset":
			in.Offset, ok = whole(val)
		case "limit":
			in.Limit, ok = whole(val)
		}
		if !ok {
			return input{}, errInput
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return input{}, errInput
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input{}, errInput
	}
	if in.Path == "" {
		return input{}, errInput
	}
	return in, nil
}

// whole is a number token written as an integer of at least 1, with no fraction
// or exponent, that fits an int.
func whole(val json.Token) (int, bool) {
	n, ok := val.(json.Number)
	if !ok || !wholeRE.MatchString(string(n)) {
		return 0, false
	}
	v, err := strconv.Atoi(string(n))
	return v, err == nil
}

// maxLineRunes is the longest line a result carries whole.
const maxLineRunes = 2000

// numbered is limit lines of text from offset, each numbered as cat -n numbers
// it, cut at a line to tools.InlineLimit with the offset to go on from. A final
// newline ends the last line and starts none. A byte order mark at the start
// and a \r before each \n are not shown. whole reports that every byte of text
// was shown as it is: every line from the first, none cut, nothing stripped.
func numbered(text string, offset, limit int) (out string, whole bool) {
	stripped := strings.ReplaceAll(strings.TrimPrefix(text, bom), "\r\n", "\n")
	whole = stripped == text
	if stripped == "" {
		return "The file is empty.", whole
	}
	lines := strings.Split(strings.TrimSuffix(stripped, "\n"), "\n")
	if offset > len(lines) {
		return "The file has " + strconv.Itoa(len(lines)) + " lines.", false
	}
	first := offset - 1
	end := len(lines)
	if limit < end-first {
		end = first + limit
	}
	whole = whole && first == 0 && end == len(lines)
	var b strings.Builder
	for i := first; i < end; i++ {
		line := cutLine(lines[i])
		whole = whole && line == lines[i]
		entry := fmt.Sprintf("%6d\t%s\n", i+1, line)
		// A line is kept only with room left for the note naming the next, so the
		// note always fits; the last line needs none.
		room := 0
		if i < end-1 {
			room = len(cutNote(i + 2))
		}
		if b.Len()+len(entry)+room > tools.InlineLimit {
			return b.String() + cutNote(i+1), false
		}
		b.WriteString(entry)
	}
	return b.String(), whole
}

// bom is UTF-8's byte order mark.
const bom = "\ufeff"

// cutNote ends a cut range with the line to go on from.
func cutNote(next int) string { return "… [cut; continue with offset " + strconv.Itoa(next) + "]" }

// cutLine is line up to maxLineRunes runes, ended with … when it was longer.
func cutLine(line string) string {
	n := 0
	for i := range line {
		if n == maxLineRunes {
			return line[:i] + "…"
		}
		n++
	}
	return line
}

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

// Package write is the tool that writes a file whole: a new one, with any
// missing directories, or one the chat has read whole and that has not changed
// since, once the user approves the path and the content. It changes nothing
// under Kubetail's directories but the chat's workspace.
package write

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

//go:embed prompts/write.md
var prompt string

// description is the tool description the model reads with the schema: the
// reference's Write, narrowed to plain absolute paths and a read first.
//
//go:embed prompts/description.md
var description string

// inputSchema is the reference's Write's file_path and content.
//
//go:embed prompts/schema.json
var inputSchema []byte

// callTimeout is the loop's bound on one call: above the turn's default, which
// reading, hashing and writing a file of tools.FileLimit bytes can pass.
const callTimeout = 30 * time.Second

// Name is the name Write is offered under.
const Name = "Write"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "write/v1"

var _ interface {
	tools.Custom
	tools.ApprovedRunner
	tools.Bounded
} = (*Tool)(nil)

// Tool writes files for the chat a call runs in, outside fence but for the
// chat's workspace, and in a folder granted read-write through a walk.
type Tool struct {
	fence fileguard.Fence
	umask fs.FileMode
	// write is the file work of a call, from the fence's check on disk, or
	// with a folder the walk through it, to the rename, answering whether it
	// made a new file. A test swaps in one that blocks.
	write func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (created bool, err error)
}

var (
	_ tools.Gated   = (*Tool)(nil)
	_ tools.Bounded = (*Tool)(nil)
)

// New is the tool, which changes nothing under fenced, Kubetail's directories,
// but a chat's workspace, writes a folder granted read-write unasked but for
// what hidden answers the sandbox keeps shut there, and makes new files and
// directories under umask, the one the sidecar started with. None is
// fileguard.ErrNoFence.
func New(umask fs.FileMode, hidden func() (never, closed []string), fenced ...string) (*Tool, error) {
	fence, err := fileguard.NewFence(fenced...)
	if err != nil {
		return nil, err
	}
	t := &Tool{fence: fence.WithHidden(hidden), umask: umask}
	t.write = t.writeFile
	return t, nil
}

// Definition is the offer: a function named Write, run by the sidecar.
func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

// Prompt is the tool's section of the system prompt.
func (t *Tool) Prompt() string { return prompt }

// CallTimeout is the loop's bound on one call, whatever it asks for.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

// Approval reads the path: the user is asked about any call but one Run
// refuses before touching anything, one in the chat's workspace, Kubetail's own
// directory, and one in a folder the session was granted read-write that the
// sandbox does not keep shut there, which names the folder Run walks. Write
// runs nowhere, so no Cwd.
func (t *Tool) Approval(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (tools.Approval, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Approval{}, err
	}
	path, err := t.check(in, rt)
	if err != nil {
		return tools.Approval{Skip: true}, nil
	}
	// Fence.File opens a path under the workspace by name through its root, so
	// an unasked write cannot leave it.
	if _, ok := fileguard.Under(tools.WorkspacePath(rt.Dir), path); ok {
		return tools.Approval{Skip: true}, nil
	}
	granted, err := rt.Session.GrantedFolders(ctx)
	if err != nil {
		return tools.Approval{}, tools.FoldersUnread(Name, err)
	}
	if folder, ok := t.fence.Granted(ctx, granted, path); ok && folder.Write {
		return tools.Approval{Skip: true, Folder: &folder}, nil
	}
	return tools.Approval{}, nil
}

// check is what a call is refused for by its input alone, touching nothing on
// disk, and otherwise the path as Abs spells it.
func (t *Tool) check(in input, rt tools.Runtime) (string, error) {
	path, err := fileguard.Abs(in.Path)
	switch {
	case err != nil:
		return "", err
	case t.fence.NamedOutsideWorkspace(rt.Dir, path):
		return "", errFenced
	case strings.ContainsRune(in.Content, 0):
		return "", errNul
	}
	return path, nil
}

// Run writes the file a call names, once approved.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	return t.RunApproved(ctx, rt, raw, tools.Approval{})
}

// RunApproved writes the file a call names: for a call its gate skipped for a
// granted folder, through the walk of that folder alone, whatever the
// session's folders say now. The file work runs where the call can leave it:
// a stat on a dead network mount blocks in a syscall no context reaches.
func (t *Tool) RunApproved(ctx context.Context, rt tools.Runtime, raw json.RawMessage, a tools.Approval) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return `{"error":"bad-input"}`, true
	}
	path, err := t.check(in, rt)
	if err != nil {
		return refusal(err), true
	}
	content := []byte(in.Content)
	made, err := t.writeWithin(ctx, rt, path, a.Folder, content)
	switch {
	case err != nil && ctx.Err() != nil:
		return cancelled, true
	case err != nil:
		return refusal(err), true
	}
	// The model wrote every byte.
	rt.Files.SetStamp(path, tools.Stamp{Sum: sha256.Sum256(content), Whole: true})
	if made {
		return "File created successfully at: " + path + " (file state is current in your context — no need to Read it back)", false
	}
	return "The file " + path + " has been updated successfully. (file state is current in your context — no need to Read it back)", false
}

// writeWithin runs the tool's write where the call can leave it. On the
// context's end it answers at once, and the goroutine is left to return
// whenever the filesystem does; fileguard checks the context before each change
// on disk, so a write that wakes late changes nothing unless its rename was
// already under way.
func (t *Tool) writeWithin(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
	type result struct {
		made bool
		err  error
	}
	done := make(chan result, 1)
	go func() {
		made, err := t.write(ctx, rt, path, folder, content)
		done <- result{made, err}
	}()
	select {
	case r := <-done:
		return r.made, r.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// writeFile is the file work of a call: the file, through the walk of a
// granted folder, the workspace's root or past the fence's check on disk,
// then a new file, or an existing one checked against the chat's stamp and
// replaced.
func (t *Tool) writeFile(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, content []byte) (bool, error) {
	var file fileguard.File
	var err error
	if folder != nil {
		file, err = t.fence.Walk(*folder, path)
	} else {
		file, err = t.fence.File(rt.Dir, path, true)
	}
	if errors.Is(err, fileguard.ErrFenced) {
		return false, errFenced
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Lstat()
	var link fileguard.ErrLink
	switch {
	case errors.Is(err, fileguard.ErrMissing):
		return true, t.create(ctx, file, content)
	case errors.As(err, &link), errors.Is(err, fileguard.ErrDirectory), errors.Is(err, fileguard.ErrNotRegular):
		return false, err
	case err != nil:
		return false, errCannotWrite
	}
	if err := file.Replaceable(info); err != nil {
		return false, err
	}
	old, err := readOld(file)
	if err != nil {
		return false, err
	}
	if err := checkStamp(rt.Files, path, old); err != nil {
		return false, err
	}
	return false, file.Replace(ctx, content, info)
}

// create makes file new.
func (t *Tool) create(ctx context.Context, file fileguard.File, content []byte) error {
	var ro fileguard.ErrDirReadOnly
	if err := file.Creatable(); errors.As(err, &ro) {
		return cannotWriteIn(ro.Prefix)
	} else if err != nil {
		return err
	}
	return file.Create(ctx, content, t.umask)
}

// readOld is the whole of the file a write would replace, when it is a text
// file Read would open.
func readOld(file fileguard.File) ([]byte, error) {
	f, err := file.Open()
	if err != nil {
		return nil, unreadable(err)
	}
	defer f.Close()
	b, err := fileguard.ReadAll(f)
	if err != nil { // coverage:ignore a regular file just opened fails to read only on an I/O error
		return nil, unreadable(err)
	}
	if fileguard.Binary(b) {
		return nil, errBinary
	}
	return b, nil
}

// unreadable is a failed open or read as Write words it: a file past the limit
// keeps its own refusal, anything else is one Kubetail cannot read.
func unreadable(err error) error {
	if errors.Is(err, fileguard.ErrTooLarge) {
		return err
	}
	return errUnreadable
}

// checkStamp refuses a replace unless the chat's stamp of path matches old and
// saw every byte: the model cannot write back what it was never shown.
func checkStamp(files tools.FileStamps, path string, old []byte) error {
	st, ok := files.Stamp(path)
	switch {
	case !ok:
		return errNoStamp
	case st.Sum != sha256.Sum256(old):
		return errChanged
	case !st.Whole:
		return errNotWhole
	}
	return nil
}

// cancelled answers a call whose context ended; the loop writes its own
// refusal in its place.
const cancelled = `{"error":"cancelled"}`

// couldNotWrite answers any failure no other refusal names.
const couldNotWrite = "Kubetail could not write the file."

// refused is a refusal the tool words itself: its text is what the model reads.
type refused string

func (r refused) Error() string { return string(r) }

const (
	errFenced      refused = "Write cannot change Kubetail's directories."
	errNul         refused = "Write writes text only, and the content holds a NUL byte."
	errCannotWrite refused = "Kubetail cannot write this file."
	errUnreadable  refused = "Kubetail cannot read this file."
	errBinary      refused = "The file looks binary. Write replaces text files only."
	errNoStamp     refused = "Read the file before replacing it."
	errChanged     refused = "The file changed since you last read it. Read it again before replacing it."
	errNotWhole    refused = "You were shown only part of this file, or it was redacted or had its line endings or byte order mark changed when read. Read it whole, with no offset or limit, or change it with Edit."
)

// cannotWriteIn refuses a new file under prefix, a directory the user cannot
// write in.
func cannotWriteIn(prefix string) error { return refused("Kubetail cannot write in " + prefix + ".") }

// refusal is what the model reads for a call Write will not carry out, in
// words that say what is wrong and never a Go error's text.
func refusal(err error) string {
	var (
		own      refused
		notPlain fileguard.ErrNotPlain
		link     fileguard.ErrLink
		notDir   fileguard.ErrNotDir
		ro       fileguard.ErrDirReadOnly
	)
	switch {
	case errors.As(err, &own):
		return string(own)
	case errors.Is(err, fileguard.ErrNotOneLine):
		return "Write takes a path of one line, with no control character, of at most 4096 bytes."
	case errors.As(err, &notPlain):
		return "Write takes a path in its plain form. Call again with: " + notPlain.Plain
	case errors.Is(err, fileguard.ErrNotAbs):
		return "Write takes an absolute path."
	case errors.Is(err, fileguard.ErrNotLocal):
		return "Write changes files on this machine only."
	case errors.As(err, &link):
		return "This is a symbolic link. Write to its target instead: " + link.Target
	case errors.Is(err, fileguard.ErrDirectory):
		return "This is a directory, not a file."
	case errors.Is(err, fileguard.ErrNotRegular):
		return "This is not a regular file."
	case errors.As(err, &notDir):
		return "Part of this path is a file, not a directory: " + notDir.Prefix
	case errors.Is(err, fileguard.ErrOtherUser):
		return "This file belongs to another user. Kubetail does not replace it."
	case errors.Is(err, fileguard.ErrOtherGroup):
		return "This file belongs to a group you are not in. Kubetail does not replace it."
	case errors.Is(err, fileguard.ErrReadOnly), errors.Is(err, fileguard.ErrUnresolved):
		return string(errCannotWrite)
	case errors.As(err, &ro):
		return "Kubetail cannot replace files in this directory."
	case errors.Is(err, fileguard.ErrTooLarge):
		return "The file is larger than 8 MiB, which Write does not replace."
	case errors.Is(err, fileguard.ErrExists):
		return "A file was created at this path since the request. Read it before replacing it."
	case errors.Is(err, fileguard.ErrReplace):
		return "Kubetail could not replace the file."
	}
	if text, ok := fileguard.WalkRefusal(err); ok {
		return text
	}
	return couldNotWrite
}

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionWrite }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

// ActionOf is the file a call writes and what it puts there, through the parse
// Run uses, as the call named them. Write runs nowhere, so cwd is never set.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{Write: &tools.WriteAction{Path: in.Path, Content: in.Content}}, nil
}

var errInput = errors.New(`write input is not {"file_path": <non-empty string>, "content": <string of at most 8 MiB>}`)

// input is one call's arguments.
type input struct {
	Path    string
	Content string
}

// parse is parseWithin tools.FileLimit, the bound a call's content takes.
func parse(raw json.RawMessage) (input, error) { return parseWithin(raw, tools.FileLimit) }

// parseWithin reads an object whose keys are file_path and content, each
// spelled exactly and once, with nothing after it, as read's parse does, and
// whose content is at most limit bytes. Each value's type is checked off its
// token, since a typed decode takes null as the zero value.
func parseWithin(raw json.RawMessage, limit int) (input, error) {
	var in input
	dec := json.NewDecoder(bytes.NewReader(raw))
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
		case "content":
			in.Content, ok = val.(string)
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
	if in.Path == "" || !seen["content"] || len(in.Content) > limit {
		return input{}, errInput
	}
	return in, nil
}

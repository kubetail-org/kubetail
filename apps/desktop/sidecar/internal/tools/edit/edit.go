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

// Package edit is the tool that replaces exact text in a file the chat has
// read and that has not changed since, once the user approves the path and
// both strings. It changes nothing under Kubetail's directories but the
// chat's workspace, and never creates a file.
package edit

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

//go:embed prompts/edit.md
var prompt string

// description is the tool description the model reads with the schema: the
// reference's Edit, narrowed to plain absolute paths and a read first.
//
//go:embed prompts/description.md
var description string

// inputSchema is the reference's Edit's file_path, old_string, new_string and
// replace_all.
//
//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name Edit is offered under.
const Name = "Edit"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "edit/v1"

// callTimeout is the loop's bound on one call: above the turn's default, which
// reading, hashing and writing a file of tools.FileLimit bytes can pass.
const callTimeout = 30 * time.Second

var _ interface {
	tools.Custom
	tools.ApprovedRunner
	tools.Bounded
} = (*Tool)(nil)

// Tool replaces text in files for the chat a call runs in, outside fence but
// for the chat's workspace, and in a folder granted read-write through a walk.
type Tool struct {
	fence fileguard.Fence
	// edit is the file work of a call, from the fence's check on disk, or with
	// a folder the walk through it, to the rename. A test swaps in one that
	// blocks.
	edit func(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error)
}

// New is the tool, which changes nothing under fenced, Kubetail's directories,
// but a chat's workspace, and edits a folder granted read-write unasked but
// for what hidden answers the sandbox keeps shut there. None is
// fileguard.ErrNoFence.
func New(hidden func() (never, closed []string), fenced ...string) (*Tool, error) {
	fence, err := fileguard.NewFence(fenced...)
	if err != nil {
		return nil, err
	}
	t := &Tool{fence: fence.WithHidden(hidden)}
	t.edit = t.editFile
	return t, nil
}

// Definition is the offer: a function named Edit, run by the sidecar.
func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

// Prompt is the tool's section of the system prompt.
func (t *Tool) Prompt() string { return prompt }

// CallTimeout is the loop's bound on one call, whatever it asks for.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

// Approval reads the call: the user is asked about any call but one Run
// refuses before touching anything, one in the chat's workspace, Kubetail's own
// directory, and one in a folder the session was granted read-write that the
// sandbox does not keep shut there, which names the folder Run walks. Edit
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
	// an unasked edit cannot leave it.
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
	case in.Old == "":
		return "", errEmptyOld
	case in.Old == in.New:
		return "", errSame
	case strings.ContainsRune(in.New, 0):
		return "", errNul
	case strings.Contains(in.New, safe.Redacted):
		return "", errRedactedNew
	}
	return path, nil
}

// Run replaces the text a call names, once approved.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	return t.RunApproved(ctx, rt, raw, tools.Approval{})
}

// RunApproved replaces the text a call names: for a call its gate skipped
// for a granted folder, through the walk of that folder alone, whatever the
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
	e, err := t.editWithin(ctx, rt, path, a.Folder, in)
	switch {
	case err != nil && ctx.Err() != nil:
		return cancelled, true
	case err != nil:
		return refusal(err), true
	}
	rt.Files.SetStamp(path, e.stamp)
	if e.n > 1 {
		return "The file " + path + " has been updated. All " + strconv.Itoa(e.n) + " occurrences were successfully replaced. (file state is current in your context — no need to Read it back)", false
	}
	return "The file " + path + " has been updated successfully. (file state is current in your context — no need to Read it back)", false
}

// editWithin runs the tool's edit where the call can leave it. On the
// context's end it answers at once, and the goroutine is left to return
// whenever the filesystem does; fileguard checks the context before the
// rename, so an edit that wakes late changes nothing unless its rename was
// already under way.
func (t *Tool) editWithin(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error) {
	type result struct {
		e   edited
		err error
	}
	done := make(chan result, 1)
	go func() {
		e, err := t.edit(ctx, rt, path, folder, in)
		done <- result{e, err}
	}()
	select {
	case r := <-done:
		return r.e, r.err
	case <-ctx.Done():
		return edited{}, ctx.Err()
	}
}

// edited is what an edit did: how many occurrences it replaced, and the stamp
// the new bytes get.
type edited struct {
	n     int
	stamp tools.Stamp
}

// editFile is the file work of a call: the file, through the walk of a
// granted folder, the workspace's root or past the fence's check on disk,
// checked against the chat's stamp, the text replaced, and the file replaced.
func (t *Tool) editFile(ctx context.Context, rt tools.Runtime, path string, folder *session.Folder, in input) (edited, error) {
	var file fileguard.File
	var err error
	if folder != nil {
		file, err = t.fence.Walk(*folder, path)
	} else {
		file, err = t.fence.File(rt.Dir, path, false)
	}
	if errors.Is(err, fileguard.ErrFenced) {
		return edited{}, errFenced
	}
	if err != nil {
		return edited{}, err
	}
	defer file.Close()
	info, err := file.Lstat()
	var link fileguard.ErrLink
	switch {
	case errors.Is(err, fileguard.ErrMissing), errors.As(err, &link),
		errors.Is(err, fileguard.ErrDirectory), errors.Is(err, fileguard.ErrNotRegular):
		return edited{}, err
	case err != nil:
		return edited{}, errCannotWrite
	}
	if err := file.Replaceable(info); err != nil {
		return edited{}, err
	}
	old, err := readOld(file)
	if err != nil {
		return edited{}, err
	}
	st, err := checkStamp(rt.Files, path, old)
	if err != nil {
		return edited{}, err
	}
	text := string(old)
	from, to := in.Old, in.New
	if crlf(text) {
		from, to = toCRLF(from), toCRLF(to)
		if from == to {
			return edited{}, errSame
		}
	}
	n, err := matches(text, from, in.All, strings.Index)
	if err != nil {
		return edited{}, matchRefusal(err, text, n, from, in.Old)
	}
	if len(text)+n*(len(to)-len(from)) > tools.FileLimit {
		return edited{}, errTooLargeAfter
	}
	text = strings.Replace(text, from, to, n)
	if err := file.Replace(ctx, []byte(text), info); err != nil {
		return edited{}, err
	}
	// The model knows every byte that changed only when its strings went in
	// as it sent them.
	asSent := from == in.Old && to == in.New
	return edited{n: n, stamp: tools.Stamp{Sum: sha256.Sum256([]byte(text)), Whole: st.Whole && asSent}}, nil
}

// Why matches refuses a count.
var (
	errNone    = errors.New("edit: no match")
	errSeveral = errors.New("edit: several matches")
	errOverlap = errors.New("edit: overlapping matches")
)

// matches is how many occurrences of old in text the edit replaces: every
// non-overlapping one with all, else one, which must be the only match and
// have no other overlapping it. Past the count, it finds the match and probes
// for an overlap with two scans, so its work is linear in len(text). old is
// never empty, which check refuses. index is strings.Index.
func matches(text, old string, all bool, index func(s, substr string) int) (int, error) {
	n := strings.Count(text, old)
	switch {
	case n == 0:
		return 0, errNone
	case all:
		return n, nil
	case n > 1:
		return n, errSeveral
	case index(text[index(text, old)+1:], old) >= 0:
		return 1, errOverlap
	}
	return 1, nil
}

// matchRefusal is err from matching old in text as the model reads it,
// echoing old_string as the call gave it.
func matchRefusal(err error, text string, n int, old, given string) error {
	switch {
	case errors.Is(err, errNone):
		return refused("String to replace not found in file." + hint(text, old) + "\nString: " + echo(given))
	case errors.Is(err, errSeveral):
		return refused("Found " + strconv.Itoa(n) + " matches of the string to replace, but replace_all is false. To replace all occurrences, set replace_all to true. To replace only one occurrence, please provide more context to uniquely identify the instance.\nString: " + echo(given))
	case errors.Is(err, errOverlap):
		return refused("The string to replace matches in overlapping places, so it is not unique. Please provide more context to uniquely identify the instance.\nString: " + echo(given))
	}
	return err // coverage:ignore matches returns only the three errors above
}

// toCRLF is s with every \n not already after a \r made \r\n.
func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// hint is the sentence a miss of old in text carries when its cause can be
// named, the first that applies, or nothing. Read redacts before it shows a
// file, and some rules rewrite the text beside the mark, so a string found in
// the redacted file but not the raw one was copied from what Read showed.
func hint(text, old string) string {
	switch {
	case strings.Contains(old, safe.Redacted) || strings.Contains(safe.Redact(text), old):
		return " Read redacts secrets, so the text it replaced cannot be matched. Choose a string that avoids it."
	case strings.Contains(text, "\r\n") && !crlf(text) && strings.Contains(old, "\n"):
		return " The file mixes line endings, which Edit matches exactly. Choose a string within one line."
	case strings.Contains(old, "…") && hasLongLine(text):
		return " Read cuts lines longer than 2000 characters, so the text after the cut cannot be matched. Choose a string before it."
	}
	return ""
}

// crlf reports a file whose every line break is \r\n, with at least one.
func crlf(text string) bool {
	n := strings.Count(text, "\r\n")
	return n > 0 && n == strings.Count(text, "\n")
}

// hasLongLine reports a line Read would cut.
func hasLongLine(text string) bool {
	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if utf8.RuneCountInString(line) > maxLineRunes {
			return true
		}
	}
	return false
}

// maxLineRunes is the longest line Read shows whole, and the most of
// old_string a refusal carries.
const maxLineRunes = 2000

// echo is old_string as a refusal carries it: up to maxLineRunes runes, ended
// with … when it was longer.
func echo(old string) string {
	n := 0
	for i := range old {
		if n == maxLineRunes {
			return old[:i] + "…"
		}
		n++
	}
	return old
}

// readOld is the whole of the file an edit would change, when it is a text
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

// unreadable is a failed open or read as Edit words it: a file past the limit
// keeps its own refusal, anything else is one Kubetail cannot read.
func unreadable(err error) error {
	if errors.Is(err, fileguard.ErrTooLarge) {
		return err
	}
	return errUnreadable
}

// checkStamp is the chat's stamp of path when it matches old: the model must
// have read the bytes it is changing.
func checkStamp(files tools.FileStamps, path string, old []byte) (tools.Stamp, error) {
	st, ok := files.Stamp(path)
	switch {
	case !ok:
		return tools.Stamp{}, errNoStamp
	case st.Sum != sha256.Sum256(old):
		return tools.Stamp{}, errChanged
	}
	return st, nil
}

// cancelled answers a call whose context ended; the loop writes its own
// refusal in its place.
const cancelled = `{"error":"cancelled"}`

// refused is a refusal the tool words itself: its text is what the model reads.
type refused string

func (r refused) Error() string { return string(r) }

const (
	errFenced        refused = "Edit cannot change Kubetail's directories."
	errEmptyOld      refused = "old_string is empty. Edit does not create files: use Write."
	errSame          refused = "old_string and new_string are the same, so there is nothing to change."
	errNul           refused = "Edit writes text only, and new_string holds a NUL byte."
	errRedactedNew   refused = "new_string holds " + safe.Redacted + ", the mark Read shows in place of a secret. Edit would write the mark as text. Leave the redacted text out of the edit."
	errCannotWrite   refused = "Kubetail cannot write this file."
	errUnreadable    refused = "Kubetail cannot read this file."
	errBinary        refused = "The file looks binary. Edit changes text files only."
	errNoStamp       refused = "Read the file before editing it."
	errChanged       refused = "The file changed since you last read it. Read it again before editing it."
	errTooLargeAfter refused = "The edited file would be larger than 8 MiB."
)

// refusal is what the model reads for a call Edit will not carry out, in words
// that say what is wrong and never a Go error's text.
func refusal(err error) string {
	var (
		own      refused
		notPlain fileguard.ErrNotPlain
		link     fileguard.ErrLink
		ro       fileguard.ErrDirReadOnly
	)
	switch {
	case errors.As(err, &own):
		return string(own)
	case errors.Is(err, fileguard.ErrNotOneLine):
		return "Edit takes a path of one line, with no control character, of at most 4096 bytes."
	case errors.As(err, &notPlain):
		return "Edit takes a path in its plain form. Call again with: " + notPlain.Plain
	case errors.Is(err, fileguard.ErrNotAbs):
		return "Edit takes an absolute path."
	case errors.Is(err, fileguard.ErrNotLocal):
		return "Edit changes files on this machine only."
	case errors.Is(err, fileguard.ErrMissing):
		return "The file does not exist. Use Write to create it."
	case errors.As(err, &link):
		return "This is a symbolic link. Edit its target instead: " + link.Target
	case errors.Is(err, fileguard.ErrDirectory):
		return "This is a directory, not a file."
	case errors.Is(err, fileguard.ErrNotRegular):
		return "This is not a regular file."
	case errors.Is(err, fileguard.ErrOtherUser):
		return "This file belongs to another user. Kubetail does not replace it."
	case errors.Is(err, fileguard.ErrOtherGroup):
		return "This file belongs to a group you are not in. Kubetail does not replace it."
	case errors.Is(err, fileguard.ErrReadOnly), errors.Is(err, fileguard.ErrUnresolved):
		return string(errCannotWrite)
	case errors.As(err, &ro):
		return "Kubetail cannot replace files in this directory."
	case errors.Is(err, fileguard.ErrTooLarge):
		return "The file is larger than 8 MiB, which Edit does not change."
	case errors.Is(err, fileguard.ErrCancelled):
		return cancelled
	case errors.Is(err, fileguard.ErrReplace):
		return "Kubetail could not replace the file."
	}
	if text, ok := fileguard.WalkRefusal(err); ok {
		return text
	}
	return "Kubetail could not write the file."
}

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionEdit }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

// ActionOf is the file a call edits and both strings, through the parse Run
// uses, as the call named them. Edit runs nowhere, so cwd is never set.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{Edit: &tools.EditAction{Path: in.Path, OldString: in.Old, NewString: in.New, ReplaceAll: in.All}}, nil
}

var errInput = errors.New(`edit input is not {"file_path": <non-empty string>, "old_string": <string of at most 8 MiB>, "new_string": <string of at most 8 MiB>, "replace_all"?: <boolean>}`)

// input is one call's arguments.
type input struct {
	Path string
	Old  string
	New  string
	All  bool
}

// parse is parseWithin tools.FileLimit, the bound a call's strings take.
func parse(raw json.RawMessage) (input, error) { return parseWithin(raw, tools.FileLimit) }

// parseWithin reads an object whose keys are file_path, old_string, new_string
// and replace_all, each spelled exactly and at most once, with nothing after
// it, as write's does, and whose strings are each at most limit bytes. Each
// value's type is checked off its token, since a typed decode takes null as
// the zero value.
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
		case "old_string":
			in.Old, ok = val.(string)
		case "new_string":
			in.New, ok = val.(string)
		case "replace_all":
			in.All, ok = val.(bool)
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
	if in.Path == "" || !seen["old_string"] || !seen["new_string"] ||
		len(in.Old) > limit || len(in.New) > limit {
		return input{}, errInput
	}
	return in, nil
}

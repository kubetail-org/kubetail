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

package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
)

// A PathRefusal is a PATH the user asked for and cannot have, in their
// words.
type PathRefusal string

func (r PathRefusal) Error() string { return string(r) }

// pathElement is one element of the PATH setting: the shell's PATH, or a
// folder the user typed, as typed.
type pathElement struct {
	shell bool
	typed string
}

// parsePath splits value into its elements, refusing it whole on the first
// bad one. It reads no disk.
func parsePath(value string) ([]pathElement, error) {
	var elements []pathElement
	for e := range strings.SplitSeq(value, string(filepath.ListSeparator)) {
		if e == "$PATH" || e == "${PATH}" {
			elements = append(elements, pathElement{shell: true})
			continue
		}
		if reason := typedRefusal(e); reason != "" {
			return nil, PathRefusal(reason)
		}
		elements = append(elements, pathElement{typed: e})
	}
	return elements, nil
}

// typedRefusal is why e cannot be a typed folder, "" when it can.
func typedRefusal(e string) string {
	rest, underHome := homeRelative(e)
	switch {
	case e == "":
		return "The PATH has an empty entry, which a shell searches as the current folder."
	case strings.ContainsRune(e, 0):
		return fmt.Sprintf("%q holds a NUL character.", e)
	case slices.Contains([]string{"~", "$HOME", "${HOME}"}, e) || underHome && filepath.Clean(rest) == ".":
		return fmt.Sprintf("%q is your home, which sandboxed commands never search.", e)
	case strings.Contains(e, "$PATH") || strings.Contains(e, "${PATH}"):
		return fmt.Sprintf("%q mixes $PATH with other text; give $PATH an entry of its own.", e)
	case strings.Contains(rest, "$"):
		return fmt.Sprintf("%q names a variable; Kubetail expands only $PATH, $HOME and ~.", e)
	case !underHome && !filepath.IsAbs(e):
		return fmt.Sprintf("%q is not a full path.", e)
	case slices.Contains(strings.Split(rest, "/"), ".."):
		// A shell resolves .. through links, and Kubetail would resolve it as
		// text, so the two would search different folders.
		return fmt.Sprintf("%q holds \"..\"; name the folder without it.", e)
	}
	return ""
}

// homeRelative is e with a leading ~/, $HOME/ or ${HOME}/ taken off, and
// whether it had one; else e as it is.
func homeRelative(e string) (string, bool) {
	for _, prefix := range []string{"~/", "$HOME/", "${HOME}/"} {
		if rest, ok := strings.CutPrefix(e, prefix); ok {
			return rest, true
		}
	}
	return e, false
}

// CheckPath refuses a PATH setting that does not parse, in the user's words,
// naming the element.
func CheckPath(value string) error {
	_, err := parsePath(value)
	return err
}

// PathOrigin is where a folder of the PATH setting came from.
type PathOrigin string

const (
	// OriginShell is a folder of the login shell's PATH, spliced in for
	// $PATH. A run searches it only when every run reads it already.
	OriginShell PathOrigin = "shell"
	// OriginTyped is a folder the user typed: their read grant.
	OriginTyped PathOrigin = "typed"
)

// PathFolder is one folder of the PATH setting, expanded, and its origin.
type PathFolder struct {
	Path   string
	Origin PathOrigin
}

// ExpandPath is value's folders in order: each $PATH is shell, already split
// and never expanded again, and each typed folder is expanded against home,
// canonical, and cleaned. A value that does not parse is its refusal.
func ExpandPath(value string, shell []string, home string) ([]PathFolder, error) {
	elements, err := parsePath(value)
	if err != nil {
		return nil, err
	}
	var folders []PathFolder
	for _, e := range elements {
		if e.shell {
			for _, dir := range shell {
				folders = append(folders, PathFolder{Path: dir, Origin: OriginShell})
			}
			continue
		}
		path := e.typed
		if rest, ok := homeRelative(path); ok {
			path = filepath.Join(home, rest)
		}
		folders = append(folders, PathFolder{Path: filepath.Clean(path), Origin: OriginTyped})
	}
	return folders, nil
}

// PathSetting is the PATH setting as stored, and why Kubetail cannot use it,
// "" when it can.
type PathSetting struct {
	Value   string
	Refusal string
}

// pathEntry is the PATH setting. It has no strictest value: a stored value
// Kubetail cannot use is read with its refusal, which starts no run.
var pathEntry = Entry[string]{Key: "sandbox.path", Default: "$PATH", Check: CheckPath}

// selectPath is the PATH setting, $PATH with no row, a refused one with its
// refusal. A read that fails is an error, never $PATH.
func selectPath(ctx context.Context, st stmts) (PathSetting, error) {
	scopes, err := selectScopedSetting(ctx, st, pathEntry.Key, "")
	if err != nil {
		return PathSetting{}, fmt.Errorf("select path setting: %w", err)
	}
	switch value := scopes[""].(type) {
	case nil:
		return pathSettingOf(pathEntry.Default), nil
	case string:
		return pathSettingOf(value), nil
	}
	return PathSetting{Refusal: "The stored PATH is not text."}, nil
}

// pathSettingOf is value with the grammar's refusal of it.
func pathSettingOf(value string) PathSetting {
	setting := PathSetting{Value: value}
	if err := CheckPath(value); err != nil {
		setting.Refusal = err.Error()
	}
	return setting
}

// SetPath saves the PATH setting, once the grammar passes it: its refusal is
// a PathRefusal, which the wire maps.
func (s *Store) SetPath(ctx context.Context, value string) error {
	if err := CheckPath(value); err != nil {
		return err
	}
	return s.write(ctx, appdb.KeySandboxPath, func(st stmts) error {
		return writeSetting(ctx, st, pathEntry, "", value, s.now().UnixMilli())
	})
}

// ListenForSandboxPathChanges notifies after each save of the setting and each read of
// the login shell. Listen before the first read.
func (s *Store) ListenForSandboxPathChanges() ChangeNotifications {
	return s.db.ListenForChanges(appdb.KeySandboxPath)
}

// ErrEmptyShellPath is a login shell that answered no PATH: a broken startup
// file, never a choice, so it keeps the last one read.
var ErrEmptyShellPath = PathRefusal("Your shell's PATH is empty.")

// selectShellPath is the login shell's PATH as last read, split, and whether
// a read has ever answered.
func selectShellPath(ctx context.Context, st stmts) ([]string, bool, error) {
	var joined string
	err := st.QueryRow(ctx, stmtSelectShellPath).Scan(&joined)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select shell path: %w", err)
	}
	return filepath.SplitList(joined), true, nil
}

// writeShellPath stores dirs as the login shell's PATH, and answers the one it
// replaced and whether it changed. A folder whose name holds the separator
// splits differently on read, but no run may search such a folder anyway.
func (s *Store) writeShellPath(ctx context.Context, dirs []string) (before []string, changed bool, err error) {
	if len(dirs) == 0 {
		return nil, false, ErrEmptyShellPath
	}
	err = s.stmts.InTx(ctx, func(st stmts) error {
		before, _, err = selectShellPath(ctx, st)
		if err != nil {
			return err
		}
		if changed = !slices.Equal(before, dirs); !changed {
			return nil
		}
		if _, err := st.Exec(ctx, stmtUpsertShellPath, strings.Join(dirs, string(filepath.ListSeparator))); err != nil {
			return fmt.Errorf("write shell path: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return before, changed, nil
}

// readPath is the PATH setting and the shell's last PATH, from one read
// transaction, so a run never pairs a new setting with an old shell PATH.
// found is false before a read of the shell ever answered.
func (s *Store) readPath(ctx context.Context) (setting PathSetting, shell []string, found bool, err error) {
	err = s.stmts.InReadTx(ctx, func(st stmts) error {
		if setting, err = selectPath(ctx, st); err != nil {
			return err
		}
		shell, found, err = selectShellPath(ctx, st)
		return err
	})
	return setting, shell, found, err
}

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
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
)

// Executable is a program the probe checks.
type Executable struct {
	Name       string
	Invocation string // "<name> --version" when the user gave none
	// Refusal is why CheckExecutable refuses a registered one, which is never
	// probed; "" for one Kubetail probes.
	Refusal string
}

// CuratedExecutables is what the probe checks on every machine, in this order.
// A kubeconfig's exec credential plugins run in the sidecar, never in the
// sandbox, so none is listed.
var CuratedExecutables = []Executable{
	{Name: "kubectl", Invocation: "kubectl version --client"},
	{Name: "helm", Invocation: "helm version"},
	{Name: "kustomize", Invocation: "kustomize version"},
	{Name: "git", Invocation: "git --version"},
	{Name: "jq", Invocation: "jq --version"},
	{Name: "yq", Invocation: "yq --version"},
}

const (
	maxInvocationFields = 8
	maxInvocationField  = 255
)

// executableName is a plain program name: 1 to 64 bytes of [A-Za-z0-9._+-], not
// starting with -.
var executableName = regexp.MustCompile(`^[A-Za-z0-9._+][A-Za-z0-9._+-]{0,63}$`)

// CheckExecutable is why an executable named name, probed by invocation, cannot be
// registered, a ExecutableRefusal, or nil. The invocation is split on whitespace
// and never run through a shell, so its first field must be the name.
func CheckExecutable(name, invocation string) error {
	fields := strings.Fields(invocation)
	switch {
	case !executableName.MatchString(name) || name == "." || name == "..":
		return ExecutableRefusal("An executable's name is 1 to 64 letters, digits, ., _, + or -, and does not start with -.")
	case slices.ContainsFunc(CuratedExecutables, func(curated Executable) bool { return curated.Name == name }):
		return ExecutableRefusal("Kubetail probes " + name + " already.")
	case len(fields) == 0 || len(fields) > maxInvocationFields:
		return ExecutableRefusal("An invocation is 1 to 8 words.")
	case fields[0] != name:
		return ExecutableRefusal("An invocation starts with the executable's name.")
	}
	for _, field := range fields {
		if len(field) > maxInvocationField || strings.ContainsFunc(field, unicode.IsControl) {
			return ExecutableRefusal("Each word of an invocation is under 256 bytes, with no control character.")
		}
	}
	return nil
}

// A ExecutableRefusal is a register or a remove the user cannot have, in their words.
type ExecutableRefusal string

func (r ExecutableRefusal) Error() string { return string(r) }

const (
	ErrExecutableListed        ExecutableRefusal = "That executable is already listed."
	ErrExecutableNotRegistered ExecutableRefusal = "That executable is not registered."
)

// Executables is the registered executables in the order added, one
// CheckExecutable refuses with its Refusal. A read that fails partway answers
// none.
func (s *Store) Executables(ctx context.Context) ([]Executable, error) {
	rows, err := s.stmts.Stmts().Query(ctx, stmtSelectExecutables)
	if err != nil {
		return nil, fmt.Errorf("select executables: %w", err)
	}
	return scanAll(rows, func() (Executable, error) {
		var e Executable
		if err := rows.Scan(&e.Name, &e.Invocation); err != nil {
			return Executable{}, fmt.Errorf("scan executable: %w", err)
		}
		if err := CheckExecutable(e.Name, e.Invocation); err != nil {
			e.Refusal = err.Error()
		}
		return e, nil
	})
}

// RegisterExecutable adds an executable for the probe to check, with
// invocation, or "<name> --version" when it is "". A name already stored is
// ErrExecutableListed before it is checked, so a refused row's name stays
// listed until the user removes it.
func (s *Store) RegisterExecutable(ctx context.Context, name, invocation string) error {
	if invocation == "" {
		invocation = name + " --version"
	}
	return s.write(ctx, appdb.KeyExecutables, func(st stmts) error {
		var listed bool
		if err := st.QueryRow(ctx, stmtSelectExecutableListed, name).Scan(&listed); err != nil {
			return fmt.Errorf("select executable: %w", err)
		}
		if listed {
			return ErrExecutableListed
		}
		if err := CheckExecutable(name, invocation); err != nil {
			return err
		}
		if _, err := st.Exec(ctx, stmtInsertExecutable, name, invocation, s.now().UnixMilli()); err != nil { // coverage:ignore the lookup in this transaction rules out the key's conflict, and every column is given
			return fmt.Errorf("insert executable: %w", err)
		}
		return nil
	})
}

// RemoveExecutable removes a registered executable by name, a refused one too.
func (s *Store) RemoveExecutable(ctx context.Context, name string) error {
	return s.write(ctx, appdb.KeyExecutables, func(st stmts) error {
		return execOne(ctx, st, stmtDeleteExecutable, ErrExecutableNotRegistered, name)
	})
}

// ListenForExecutableChanges notifies after each register or remove commits. Listen
// before the first read.
func (s *Store) ListenForExecutableChanges() ChangeNotifications {
	return s.db.ListenForChanges(appdb.KeyExecutables)
}

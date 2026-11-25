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
	"strings"
	"testing"

	"github.com/amorey/gobus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

func TestCheckExecutableRefusesEachBadShape(t *testing.T) {
	assert.NoError(t, CheckExecutable("kubectl-foo", "kubectl-foo --version"))
	assert.NoError(t, CheckExecutable("k9s", "k9s version --short"))

	for name, c := range map[string]struct{ name, invocation string }{
		"a name with a slash":          {"bin/tool", "bin/tool --version"},
		"a name starting with -":       {"-tool", "-tool --version"},
		"a name past 64 bytes":         {strings.Repeat("a", 65), strings.Repeat("a", 65)},
		"an empty name":                {"", "--version"},
		"a name with a space":          {"my tool", "my tool"},
		"a name that is a dot":         {".", ". --version"},
		"a name that is two dots":      {"..", ".. --version"},
		"a curated name":               {"kubectl", "kubectl version"},
		"an empty invocation":          {"tool", ""},
		"an invocation of nine fields": {"tool", "tool 1 2 3 4 5 6 7 8"},
		"a first field not the name":   {"tool", "other --version"},
		"a field past 255 bytes":       {"tool", "tool " + strings.Repeat("a", 256)},
		"a field with a control char":  {"tool", "tool --ver\x1bsion"},
		"a field with a DEL":           {"tool", "tool \x7f"},
	} {
		assert.Error(t, CheckExecutable(c.name, c.invocation), name)
	}
}

// openWithDB is a store with no file over a fresh app.db, and the db.
func openWithDB(t *testing.T) (*Store, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	return openStore(t, db), db
}

func executables(t *testing.T, s *Store) []Executable {
	t.Helper()
	list, err := s.Executables(t.Context())
	require.NoError(t, err)
	return list
}

// An executable is registered with its invocation, or "<name> --version" with
// none, listed in the order added, and removed by name; each refusal is in the
// user's words and writes nothing.
func TestRegisterAndRemoveAnExecutable(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := t.Context()

	require.NoError(t, s.RegisterExecutable(ctx, "k9s", ""))
	require.NoError(t, s.RegisterExecutable(ctx, "kubectl-foo", "kubectl-foo info"))
	assert.Equal(t, []Executable{{Name: "k9s", Invocation: "k9s --version"}, {Name: "kubectl-foo", Invocation: "kubectl-foo info"}}, executables(t, s))

	var refusal ExecutableRefusal
	assert.ErrorIs(t, s.RegisterExecutable(ctx, "k9s", "k9s info"), ErrExecutableListed, "a name already listed")
	assert.ErrorAs(t, s.RegisterExecutable(ctx, "kubectl", ""), &refusal, "a curated name")
	assert.ErrorAs(t, s.RegisterExecutable(ctx, "bad/name", ""), &refusal, "a bad shape")
	assert.ErrorIs(t, s.RemoveExecutable(ctx, "helm"), ErrExecutableNotRegistered, "a name not registered")
	assert.Len(t, executables(t, s), 2)

	require.NoError(t, s.RemoveExecutable(ctx, "k9s"))
	assert.Equal(t, []Executable{{Name: "kubectl-foo", Invocation: "kubectl-foo info"}}, executables(t, s))
}

// Two executables added in the same millisecond keep the order they were added.
func TestExecutablesAreReadInTheOrderAdded(t *testing.T) {
	t.Parallel()
	s, db := openWithDB(t)
	exec(t, db, `INSERT INTO executables (name, invocation, created_at) VALUES ('stern', 'stern --version', 5), ('k9s', 'k9s version', 5), ('argo', 'argo version', 4)`)

	var names []string
	for _, e := range executables(t, s) {
		names = append(names, e.Name)
	}
	assert.Equal(t, []string{"argo", "stern", "k9s"}, names)
}

// A row CheckExecutable refuses is listed in its place with its reason, and
// Remove takes it.
func TestARefusedExecutableIsListedWithItsReason(t *testing.T) {
	t.Parallel()
	s, db := openWithDB(t)
	exec(t, db, `INSERT INTO executables (name, invocation, created_at) VALUES ('k9s', 'k9s version', 1), ('bin/tool', 'bin/tool', 2), ('stern', 'other --version', 3)`)

	assert.Equal(t, []Executable{
		{Name: "k9s", Invocation: "k9s version"},
		{Name: "bin/tool", Invocation: "bin/tool", Refusal: "An executable's name is 1 to 64 letters, digits, ., _, + or -, and does not start with -."},
		{Name: "stern", Invocation: "other --version", Refusal: "An invocation starts with the executable's name."},
	}, executables(t, s))

	require.NoError(t, s.RemoveExecutable(t.Context(), "bin/tool"))
	require.NoError(t, s.RemoveExecutable(t.Context(), "stern"))
	assert.Equal(t, []Executable{{Name: "k9s", Invocation: "k9s version"}}, executables(t, s))
}

// A stored name wins over the shape check: a registered name a release made
// curated answers ErrExecutableListed until the user removes it, and the
// curated-name refusal after.
func TestAStoredNameIsListedBeforeItIsChecked(t *testing.T) {
	t.Parallel()
	s, db := openWithDB(t)
	exec(t, db, `INSERT INTO executables (name, invocation, created_at) VALUES ('kubectl', 'kubectl version', 1)`)
	ctx := t.Context()

	assert.Equal(t, []Executable{{Name: "kubectl", Invocation: "kubectl version", Refusal: "Kubetail probes kubectl already."}}, executables(t, s))
	assert.ErrorIs(t, s.RegisterExecutable(ctx, "kubectl", ""), ErrExecutableListed)

	require.NoError(t, s.RemoveExecutable(ctx, "kubectl"))
	assert.Equal(t, ExecutableRefusal("Kubetail probes kubectl already."), s.RegisterExecutable(ctx, "kubectl", ""))
	assert.Empty(t, executables(t, s))
}

// A read that fails is an error, never an empty list, and so is every write.
func TestAnExecutablesReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	s, db := openWithDB(t)
	exec(t, db, `DROP TABLE executables`)
	ctx := t.Context()

	list, err := s.Executables(ctx)
	assert.Error(t, err)
	assert.Nil(t, list)
	assert.Error(t, s.RegisterExecutable(ctx, "k9s", ""))
	assert.Error(t, s.RemoveExecutable(ctx, "k9s"))
}

// A read that fails after some rows were read answers none of them.
func TestAnExecutablesReadThatFailsPartwayAnswersNone(t *testing.T) {
	t.Parallel()
	s, db := openWithDB(t)
	looseTable(t, db, "executables", `name, invocation, created_at`)
	exec(t, db, `INSERT INTO executables VALUES ('k9s', 'k9s version', 1), ('stern', NULL, 2)`)

	list, err := s.Executables(t.Context())
	assert.Error(t, err)
	assert.Nil(t, list)
}

// A register or a remove pings once it commits; a refused one pings nothing.
func TestEachExecutableWritePublishes(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := t.Context()
	rx := s.ListenForExecutableChanges()
	t.Cleanup(rx.Close)
	published := func(what string, err error) {
		t.Helper()
		require.NoError(t, err, what)
		_, err = rx.TryRecv()
		require.NoError(t, err, what)
	}

	published("a register", s.RegisterExecutable(ctx, "k9s", ""))
	published("a remove", s.RemoveExecutable(ctx, "k9s"))

	assert.Error(t, s.RegisterExecutable(ctx, "kubectl", ""))
	assert.Error(t, s.RemoveExecutable(ctx, "k9s"))
	_, err := rx.TryRecv()
	assert.ErrorIs(t, err, gobus.ErrEmpty, "a refused write pinged")
}

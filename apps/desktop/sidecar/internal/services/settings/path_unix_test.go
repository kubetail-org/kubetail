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

//go:build !windows

package settings

import (
	"context"
	"testing"

	"github.com/amorey/gobus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// Each bad element refuses the whole value, naming the element; a good value
// passes.
func TestCheckPathRefusesEachBadElement(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{
		"$PATH", "${PATH}", "/opt/bin", "~/bin:$PATH:/usr/local/bin", "$HOME/go/bin", "${HOME}/go/bin", "/opt/bin/", "//opt/./bin",
	} {
		assert.NoError(t, CheckPath(ok), ok)
	}
	for value, says := range map[string]string{
		"":                "an empty entry",
		"/opt/bin::$PATH": "an empty entry",
		"$PATH:":          "an empty entry",
		"bin":             `"bin" is not a full path`,
		"~bob/bin":        `"~bob/bin" is not a full path`,
		"~":               `"~" is your home`,
		"$HOME":           `"$HOME" is your home`,
		"${HOME}":         `"${HOME}" is your home`,
		"~/":              `"~/" is your home`,
		"$HOME/":          `"$HOME/" is your home`,
		"~/.":             `"~/." is your home`,
		"$PATH/bin":       `"$PATH/bin" mixes $PATH`,
		"/x${PATH}":       `"/x${PATH}" mixes $PATH`,
		"/opt/$TOOLS/bin": `"/opt/$TOOLS/bin" names a variable`,
		"~/$X":            `"~/$X" names a variable`,
		"/opt/x/../bin":   `"/opt/x/../bin" holds ".."`,
		"~/..":            `"~/.." holds ".."`,
		"/opt/b\x00in":    `holds a NUL`,
	} {
		err := CheckPath(value)
		if assert.Error(t, err, value) {
			assert.Contains(t, err.Error(), says, value)
			assert.IsType(t, PathRefusal(""), err, "a refusal in the user's words")
		}
	}
}

// The shell's PATH is spliced in already split: nothing it lists is ever
// typed, and nothing in it is expanded again.
func TestTheShellsPathIsNeverTyped(t *testing.T) {
	t.Parallel()
	shell := []string{"/x", "~/y", "$HOME", "$PATH", "/z/../w"}
	folders, err := ExpandPath("$PATH", shell, "/home/me")
	assert.NoError(t, err)
	assert.Equal(t, []PathFolder{
		{Path: "/x", Origin: OriginShell},
		{Path: "~/y", Origin: OriginShell},
		{Path: "$HOME", Origin: OriginShell},
		{Path: "$PATH", Origin: OriginShell},
		{Path: "/z/../w", Origin: OriginShell},
	}, folders)
}

// A typed folder is expanded against the home and cleaned, in its place
// among the shell's.
func TestATypedFolderIsCleaned(t *testing.T) {
	t.Parallel()
	folders, err := ExpandPath("~/bin/:${PATH}:$HOME/go/bin://opt/./bin/:${HOME}/.cargo/bin", []string{"/usr/bin"}, "/home/me")
	assert.NoError(t, err)
	assert.Equal(t, []PathFolder{
		{Path: "/home/me/bin", Origin: OriginTyped},
		{Path: "/usr/bin", Origin: OriginShell},
		{Path: "/home/me/go/bin", Origin: OriginTyped},
		{Path: "/opt/bin", Origin: OriginTyped},
		{Path: "/home/me/.cargo/bin", Origin: OriginTyped},
	}, folders)

	_, err = ExpandPath("bin", nil, "/home/me")
	assert.Error(t, err, "a value that does not parse expands to nothing")
}

// pathSetting is the setting as a run reads it.
func pathSetting(ctx context.Context, s *Store) (PathSetting, error) {
	setting, _, _, err := s.readPath(ctx)
	return setting, err
}

// The setting starts as $PATH, is saved as written and read back; a value the
// grammar refuses writes nothing.
func TestThePathSettingRoundTrips(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := t.Context()
	got, err := pathSetting(ctx, s)
	require.NoError(t, err)
	assert.Equal(t, PathSetting{Value: "$PATH"}, got)

	require.NoError(t, s.SetPath(ctx, "~/bin:$PATH"), "the first write inserts the row")
	got, err = pathSetting(ctx, s)
	require.NoError(t, err)
	assert.Equal(t, PathSetting{Value: "~/bin:$PATH"}, got)

	assert.IsType(t, PathRefusal(""), s.SetPath(ctx, "bin:$PATH"))
	got, err = pathSetting(ctx, s)
	require.NoError(t, err)
	assert.Equal(t, "~/bin:$PATH", got.Value, "a refused value wrote nothing")
}

// A stored value the grammar refuses is read with its refusal, never as
// $PATH.
func TestARefusedPathSettingIsReadWithItsRefusal(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.path', 'bin:$PATH', 0, 0)`)

	got, err := pathSetting(t.Context(), s)
	require.NoError(t, err)
	assert.Equal(t, "bin:$PATH", got.Value)
	assert.Contains(t, got.Refusal, `"bin" is not a full path`)
}

// A read that fails is an error, never $PATH, and so is a write.
func TestAPathSettingReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `DROP TABLE settings`)
	_, err := pathSetting(t.Context(), s)
	assert.Error(t, err)
	assert.Error(t, s.SetPath(t.Context(), "$PATH"))
}

// A save pings once it commits; a refused one pings nothing.
func TestASavedPathSettingPublishes(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	rx := s.ListenForSandboxPathChanges()
	t.Cleanup(rx.Close)

	require.NoError(t, s.SetPath(t.Context(), "/opt/bin:$PATH"))
	_, err := rx.TryRecv()
	require.NoError(t, err)

	assert.Error(t, s.SetPath(t.Context(), ""))
	_, err = rx.TryRecv()
	assert.ErrorIs(t, err, gobus.ErrEmpty, "a refused save pinged")
}

// The shell's PATH is kept joined and read back split; a write answers
// whether it changed the row, and an empty PATH is never stored.
func TestTheShellPathRow(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := t.Context()
	read := func() ([]string, bool) {
		t.Helper()
		var dirs []string
		var found bool
		require.NoError(t, s.stmts.InTx(ctx, func(st stmts) error {
			var err error
			dirs, found, err = selectShellPath(ctx, st)
			return err
		}))
		return dirs, found
	}
	_, found := read()
	assert.False(t, found, "no row before a first read answers")

	before, changed, err := s.writeShellPath(ctx, []string{"/usr/bin", "/bin"})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Nil(t, before)
	dirs, found := read()
	assert.True(t, found)
	assert.Equal(t, []string{"/usr/bin", "/bin"}, dirs)

	before, changed, err = s.writeShellPath(ctx, []string{"/usr/bin", "/bin"})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, []string{"/usr/bin", "/bin"}, before)

	_, _, err = s.writeShellPath(ctx, nil)
	assert.ErrorIs(t, err, ErrEmptyShellPath)
	dirs, _ = read()
	assert.Equal(t, []string{"/usr/bin", "/bin"}, dirs, "an empty PATH wrote nothing")
}

// A shell PATH whose row cannot be read, or written, is an error, and
// nothing changes.
func TestAShellPathWriteThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	ctx := t.Context()
	looseTable(t, db, "shell_path", `id INTEGER PRIMARY KEY, path TEXT NOT NULL CHECK (path = '/usr/bin')`)
	_, changed, err := s.writeShellPath(ctx, []string{"/bin"})
	assert.Error(t, err, "a write the table refuses")
	assert.False(t, changed)

	exec(t, db, `DROP TABLE shell_path`)
	_, _, err = s.writeShellPath(ctx, []string{"/bin"})
	assert.Error(t, err, "a read of the row it replaces")
}

// A stored value that is not text is read as an empty value with its
// refusal, never as $PATH.
func TestAPathSettingThatIsNotTextIsReadWithItsRefusal(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.path', 7, 0, 0)`)
	got, err := pathSetting(t.Context(), s)
	require.NoError(t, err)
	assert.Equal(t, PathSetting{Refusal: "The stored PATH is not text."}, got)
}

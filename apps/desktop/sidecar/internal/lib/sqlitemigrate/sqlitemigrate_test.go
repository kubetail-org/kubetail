package sqlitemigrate

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	// Registers the "sqlite" driver openDB opens through.
	_ "modernc.org/sqlite"
)

// openDB opens a fresh on-disk SQLite database in a temp dir. A file (not
// :memory:) so the single *sql.DB can span multiple connections without losing
// the schema, matching how the runner is used in production.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// fsWith writes name->sql pairs into a temp "migrations" dir and returns an
// fs.FS rooted at the temp dir, so Apply(..., fsys, "migrations") reads them.
func fsWith(t *testing.T, files map[string]string) fs.FS {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "migrations")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return os.DirFS(root)
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	require.NoError(t, err)
	return n > 0
}

func TestApplyRunsInOrderAndRecords(t *testing.T) {
	db := openDB(t)
	fsys := fsWith(t, map[string]string{
		"0001_first.sql":  `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
		"0002_second.sql": `CREATE TABLE beta (id INTEGER PRIMARY KEY);`,
	})

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.NoError(t, err)
	require.Equal(t, 2, v)
	require.True(t, tableExists(t, db, "alpha"))
	require.True(t, tableExists(t, db, "beta"))

	var versions string
	require.NoError(t, db.QueryRow(
		`SELECT group_concat(version, ',') FROM (SELECT version FROM schema_migrations ORDER BY version)`,
	).Scan(&versions))
	require.Equal(t, "1,2", versions)
}

func TestApplyIsIdempotent(t *testing.T) {
	db := openDB(t)
	fsys := fsWith(t, map[string]string{
		"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
	})

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.NoError(t, err)
	require.Equal(t, 1, v)

	// A second call must be a no-op: it would error ("table alpha already
	// exists") if it re-ran the migration instead of skipping it.
	v, err = Apply(context.Background(), db, fsys, "migrations")
	require.NoError(t, err)
	require.Equal(t, 1, v)
}

func TestApplyResumesAfterPartial(t *testing.T) {
	db := openDB(t)

	v, err := Apply(context.Background(), db, fsWith(t, map[string]string{
		"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	require.NoError(t, err)
	require.Equal(t, 1, v)

	// Now a newer binary ships an additional migration; only v2 should run.
	v, err = Apply(context.Background(), db, fsWith(t, map[string]string{
		"0001_first.sql":  `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
		"0002_second.sql": `CREATE TABLE beta (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	require.NoError(t, err)
	require.Equal(t, 2, v)
	require.True(t, tableExists(t, db, "beta"))
}

func TestApplyRejectsNewerThanBinary(t *testing.T) {
	db := openDB(t)

	// Simulate a DB written by a newer binary: schema at v2 already.
	_, err := Apply(context.Background(), db, fsWith(t, map[string]string{
		"0001_first.sql":  `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
		"0002_second.sql": `CREATE TABLE beta (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	require.NoError(t, err)

	// This (older) binary only knows v1 — refuse rather than truncate.
	_, err = Apply(context.Background(), db, fsWith(t, map[string]string{
		"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	var newer *NewerSchemaError
	require.ErrorAs(t, err, &newer)
	require.Equal(t, 2, newer.Recorded)
	require.Equal(t, 1, newer.Supported)
	require.EqualError(t, err, "schema version 2 is newer than this build supports (1)")
}

func TestApplyRejectsVersionGap(t *testing.T) {
	db := openDB(t)
	_, err := Apply(context.Background(), db, fsWith(t, map[string]string{
		"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
		"0003_third.sql": `CREATE TABLE gamma (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	require.Error(t, err, "a gap in version numbers must be caught at startup")
}

func TestApplyRejectsBadFilename(t *testing.T) {
	db := openDB(t)
	_, err := Apply(context.Background(), db, fsWith(t, map[string]string{
		"init.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
	}), "migrations")
	require.Error(t, err, "a file without a numeric version prefix is a packaging bug")
}

// A migrations dir that isn't there is a build mistake, not an empty upgrade.
func TestApplyRejectsAMissingDir(t *testing.T) {
	_, err := Apply(context.Background(), openDB(t), fsWith(t, nil), "nowhere")
	require.Error(t, err)
}

// Everything that isn't a `*.sql` file is ignored, so a dir carrying a README or
// a subdirectory still migrates.
func TestApplySkipsNonSQLEntries(t *testing.T) {
	db := openDB(t)
	root := t.TempDir()
	dir := filepath.Join(root, "migrations")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notes"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "0001_first.sql"),
		[]byte(`CREATE TABLE alpha (id INTEGER PRIMARY KEY);`), 0o600))

	v, err := Apply(context.Background(), db, os.DirFS(root), "migrations")
	require.NoError(t, err)
	require.Equal(t, 1, v)
	require.True(t, tableExists(t, db, "alpha"))
}

// A leading segment that isn't a number names no version.
func TestApplyRejectsANonNumericVersion(t *testing.T) {
	fsys := fsWith(t, map[string]string{"init_first.sql": `SELECT 1;`})
	_, err := Apply(context.Background(), openDB(t), fsys, "migrations")
	require.ErrorContains(t, err, "non-numeric version")
}

// An empty set leaves the DB at its recorded version — the bookkeeping table is
// still created, so the next release has somewhere to record.
func TestApplyWithNoMigrationsIsANoOp(t *testing.T) {
	db := openDB(t)
	v, err := Apply(context.Background(), db, fsWith(t, nil), "migrations")
	require.NoError(t, err)
	require.Equal(t, 0, v)
	require.True(t, tableExists(t, db, "schema_migrations"))
}

// A migration that fails leaves neither its own changes nor its version row: the
// transaction rolls back whole, so the next run retries it from the same version.
func TestApplyRollsBackAFailedMigration(t *testing.T) {
	db := openDB(t)
	fsys := fsWith(t, map[string]string{
		"0001_first.sql":  `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`,
		"0002_broken.sql": `CREATE TABLE beta (id INTEGER PRIMARY KEY); NOT SQL;`,
	})

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.ErrorContains(t, err, "0002_broken.sql")
	require.Equal(t, 1, v)
	require.True(t, tableExists(t, db, "alpha"))
	require.False(t, tableExists(t, db, "beta"))
}

// A DB that cannot be written can't even record progress, so Apply stops at the
// bookkeeping table rather than running a migration it could not record.
func TestApplyFailsOnAClosedDB(t *testing.T) {
	db := openDB(t)
	require.NoError(t, db.Close())
	_, err := Apply(context.Background(), db, fsWith(t, nil), "migrations")
	require.ErrorContains(t, err, "create schema_migrations")
}

// unreadableFS lists its files but refuses to read them.
type unreadableFS struct{ fstest.MapFS }

func (unreadableFS) ReadFile(string) ([]byte, error) { return nil, fs.ErrPermission }

// A listed migration that cannot be read stops the run before any migration applies.
func TestApplyRejectsAnUnreadableMigration(t *testing.T) {
	db := openDB(t)
	fsys := unreadableFS{fstest.MapFS{"migrations/0001_first.sql": {Data: []byte(`SELECT 1;`)}}}

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.ErrorIs(t, err, fs.ErrPermission)
	require.ErrorContains(t, err, "0001_first.sql")
	require.Equal(t, 0, v)
}

// A migration that cannot open its transaction is not run. A connection already inside
// one refuses a second BEGIN.
func TestApplyStopsWhenAMigrationCannotBegin(t *testing.T) {
	db := openDB(t)
	db.SetMaxOpenConns(1)
	_, err := db.Exec(`BEGIN`)
	require.NoError(t, err)
	fsys := fsWith(t, map[string]string{"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY);`})

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.ErrorContains(t, err, "0001_first.sql")
	require.Equal(t, 0, v)
	require.False(t, tableExists(t, db, "alpha"))
}

// A migration whose version cannot be recorded is rolled back with it, so the next run
// retries it rather than finding its changes unrecorded.
func TestApplyRollsBackAMigrationItCannotRecord(t *testing.T) {
	db := openDB(t)
	fsys := fsWith(t, map[string]string{
		"0001_first.sql": `CREATE TABLE alpha (id INTEGER PRIMARY KEY); DROP TABLE schema_migrations;`,
	})

	v, err := Apply(context.Background(), db, fsys, "migrations")
	require.ErrorContains(t, err, "no such table: schema_migrations")
	require.Equal(t, 0, v)
	require.False(t, tableExists(t, db, "alpha"))
	require.True(t, tableExists(t, db, "schema_migrations"))
}

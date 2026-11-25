// Package sqlitemigrate is a forward-only SQL migration runner: each caller embeds its
// own numbered `*.sql` files, and Apply records progress in a schema_migrations table.
//
// Deliberately minimal — no down-migrations (a shipped binary never rolls back a fleet),
// no dependencies. Each migration runs in its own transaction, so a crash mid-upgrade
// leaves the DB at the last committed version. A DB written by a newer binary is refused
// rather than truncated. Used by internal/appdb and kubestore; the pools a migration runs
// on are internal/lib/sqlitepool's.
package sqlitemigrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NewerSchemaError refuses a database whose recorded schema version is above the newest
// migration this build carries: a newer build wrote it.
type NewerSchemaError struct {
	Recorded  int
	Supported int
}

func (e *NewerSchemaError) Error() string {
	return fmt.Sprintf("schema version %d is newer than this build supports (%d)", e.Recorded, e.Supported)
}

// migration is one numbered SQL file; version comes from the filename's leading digits
// (0001_init.sql → 1) and is both the sort key and the schema_migrations row id.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations returns dir's `*.sql` files in version order; versions must be unique
// and gap-free.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// NNNN_description.sql; a non-numeric prefix is a packaging bug, not user
		// input, so surface it loudly.
		base := e.Name()
		underscore := strings.IndexByte(base, '_')
		if underscore <= 0 {
			return nil, fmt.Errorf("migration %q has no version prefix", base)
		}
		v, err := strconv.Atoi(base[:underscore])
		if err != nil {
			return nil, fmt.Errorf("migration %q has non-numeric version: %w", base, err)
		}
		b, err := fs.ReadFile(fsys, dir+"/"+base)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", base, err)
		}
		out = append(out, migration{version: v, name: base, sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	// Gap-free, so a file missing from a release is caught at startup rather than
	// after the schema is half-applied.
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration version gap: expected %d, got %d (%s)", i+1, m.version, m.name)
		}
	}
	return out, nil
}

// Apply brings db up to dir's latest migration, each in its own transaction, and returns
// the resulting version. A DB recorded newer than the embedded set is refused with
// *NewerSchemaError.
func Apply(ctx context.Context, db *sql.DB, fsys fs.FS, dir string) (int, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}

	migs, err := loadMigrations(fsys, dir)
	if err != nil {
		return current, err
	}
	if len(migs) == 0 {
		return current, nil
	}

	// A DB written by a newer binary must be refused: downgrading would silently
	// truncate columns the newer schema relies on.
	latest := migs[len(migs)-1].version
	if current > latest {
		return current, &NewerSchemaError{Recorded: current, Supported: latest}
	}

	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := runMigration(ctx, db, m); err != nil {
			return current, fmt.Errorf("migration %s: %w", m.name, err)
		}
		current = m.version
	}
	return current, nil
}

func runMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
		m.version, m.name, time.Now().UnixMilli(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

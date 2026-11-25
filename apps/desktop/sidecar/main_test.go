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

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/filelock"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/ipc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// runArgs parses a command line the way main does, so a run test still starts
// from arguments even though run itself takes a parsed config — over an
// environment naming only roots in a scratch folder, so nothing of the shell's
// reaches the sidecar it starts and its folders are the test's, and over a fake
// keyring, so it never asks the machine's.
func runArgs(t *testing.T, args ...string) config {
	t.Helper()
	cfg, err := configFromArgs(args, scratchRoots(t).get)
	if err != nil {
		t.Fatalf("configFromArgs(%v): %v", args, err)
	}
	if err := resolveFolders(&cfg); err != nil {
		t.Fatalf("resolveFolders: %v", err)
	}
	cfg.App.Keyring = oskeyringtest.New()
	return cfg
}

// A config the composition root rejects exits 1, naming the failure on stdout.
// READY follows a composed app and FATAL a failed one, so the host never sees an
// endpoint that will not serve and can show why.
func TestRunFailsWhenTheAppCannotBeComposed(t *testing.T) {
	// stdin stays open: its EOF is a quit, and a quit during startup exits 0.
	stdin, hold := io.Pipe()
	t.Cleanup(func() { _ = hold.Close() })
	var out strings.Builder
	// A file where the chats' folder goes: app.New rejects it.
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	if err := os.WriteFile(filepath.Join(cfg.App.Folders.Data, "chats"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(t.Context(), cfg, stdin, &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	line, rest, _ := strings.Cut(out.String(), "\n")
	if !strings.HasPrefix(line, "FATAL ") || !strings.Contains(line, "chats is not a folder") {
		t.Errorf("stdout = %q, want one FATAL line naming the compose error", out.String())
	}
	if rest != "" || strings.Contains(out.String(), "READY") {
		t.Errorf("stdout = %q, want the FATAL line alone", out.String())
	}
}

// A second sidecar on a data directory exits 1 with a FATAL naming the
// directory, before it opens app.db.
func TestRunFailsWhenTheDataDirectoryIsLocked(t *testing.T) {
	data := t.TempDir()
	held, err := filelock.TryLock(filepath.Join(data, "sidecar.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })

	// stdin stays open: its EOF is a quit, and a quit during startup exits 0.
	stdin, hold := io.Pipe()
	t.Cleanup(func() { _ = hold.Close() })
	var out strings.Builder
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	cfg.App.Folders.Data = data
	if code := run(t.Context(), cfg, stdin, &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "FATAL another Kubetail is using " + data + "\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if _, err := os.Stat(filepath.Join(data, "app.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("app.db: %v, want it never made", err)
	}
}

// A compose that fails while a quit lands still logs its error: the quit makes
// the exit clean, not the failure silent. Serial: it swaps the default logger.
func TestAQuitDuringAFailedComposeLogsTheError(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out strings.Builder
	// A file where the chats' folder goes: app.New rejects it.
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	if err := os.WriteFile(filepath.Join(cfg.App.Folders.Data, "chats"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(ctx, cfg, strings.NewReader(""), &out); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(logs.String(), "chats is not a folder") {
		t.Errorf("logs = %q, want the compose error", logs.String())
	}
}

// An endpoint that cannot be bound exits 1 before anything is printed.
func TestRunFailsWhenTheEndpointCannotBeBound(t *testing.T) {
	var out strings.Builder
	cfg := runArgs(t, "--socket", unbindableEndpoint(t), "--runtime-dir", t.TempDir())
	code := run(t.Context(), cfg, strings.NewReader(""), &out)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty (the READY line follows a successful bind)", out.String())
	}
}

// A shutdown signal that lands before run — during the login shell — exits 0
// without announcing READY, like the stdin-EOF shutdown, and releases the
// endpoint: a named pipe's name stays taken while its listener is open.
func TestRunExitsCleanlyWhenTheContextEndedBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out strings.Builder
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	if code := run(ctx, cfg, strings.NewReader(""), &out); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty (a run that never starts announces nothing)", out.String())
	}
	ln, err := ipc.Listen(cfg.Socket)
	if err != nil {
		t.Fatalf("rebinding %s after run: %v (run left its listener open)", cfg.Socket, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
}

// refusingStartDataDir returns a data directory whose app.db fails the start. The
// chat service's start fails the runs a previous process left unfinished, so a
// trigger refusing that update fails it.
func refusingStartDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := appdb.Open(filepath.Join(dir, "app.db"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('k', 'kubeconfig', 'dev', 0, 0)`,
		`INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', 'k', 'chat', 0, 0)`,
		`INSERT INTO messages (id, chat_id, seq, role, content, created_at) VALUES ('u', 'c', 0, 'user', '[]', 0)`,
		`INSERT INTO agent_runs (id, agent_type, app_version, trigger, chat_id, trigger_message_id, provider, model, dialect, created_at)
		 VALUES ('r', 'chat', 'dev', 'chat', 'c', 'u', 'p', 'm', 'fake', 0)`,
		`CREATE TRIGGER refuse_start BEFORE UPDATE ON agent_runs BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`,
	} {
		if _, err := db.Write.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A failed start still closes the app, and app.db with it. SQLite deletes the -wal
// beside a database when its last connection closes, so its absence shows the
// file was released.
func TestRunClosesTheAppWhenStartFails(t *testing.T) {
	dir := refusingStartDataDir(t)

	// stdin stays open: its EOF means the parent died, and a start cancelled that way
	// would fail on whichever part was starting rather than on the trigger.
	stdin, hold := io.Pipe()
	t.Cleanup(func() { _ = hold.Close() })
	var out strings.Builder
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	cfg.App.Folders.Data = dir
	if code := run(t.Context(), cfg, stdin, &out); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.db-wal")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("app.db-wal after a failed start: stat = %v, want absent (the database was not closed)", err)
	}
}

// cancelOnWrite cancels on its first write: run writes READY between composing
// the app and starting it.
type cancelOnWrite struct{ cancel context.CancelFunc }

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

// A start that fails while a quit lands exits 0 and still logs its error. Serial:
// it swaps the default logger.
func TestAQuitDuringAFailedStartExitsCleanly(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	stdin, hold := io.Pipe()
	t.Cleanup(func() { _ = hold.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir())
	cfg.App.Folders.Data = refusingStartDataDir(t)
	if code := run(ctx, cfg, stdin, cancelOnWrite{cancel}); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(logs.String(), "app start") {
		t.Errorf("logs = %q, want the start error", logs.String())
	}
}

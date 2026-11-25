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

package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// open runs Open with stderr captured, so a test can assert on both sinks.
func open(t *testing.T, path string, stderr bool) (*slog.Logger, *bytes.Buffer, error) {
	t.Helper()
	var errBuf bytes.Buffer
	logger, closeLog, err := openTo(&errBuf, path, stderr, slog.LevelInfo)
	t.Cleanup(func() {
		if closeLog != nil {
			if e := closeLog(); e != nil {
				t.Errorf("close: %v", e)
			}
		}
	})
	return logger, &errBuf, err
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// logDir is a directory Open takes: owner-only, as the host makes the log folder.
func logDir(t *testing.T) string {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenWritesToTheFileAlone(t *testing.T) {
	path := filepath.Join(logDir(t), "sidecar.log")
	logger, errBuf, err := open(t, path, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	logger.Info("hello")

	if !strings.Contains(read(t, path), `"message":"hello"`) {
		t.Errorf("the record did not reach the file: %q", read(t, path))
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr should be quiet without --log-stderr, got %q", errBuf.String())
	}
}

// Open is openTo on the process's stderr; with no stderr sink asked for, it writes the
// file and leaves the test's output alone.
func TestOpenWritesTheFileItIsGiven(t *testing.T) {
	path := filepath.Join(logDir(t), "sidecar.log")
	logger, closeLog, err := Open(path, false, slog.LevelInfo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	logger.Info("hello")
	if err := closeLog(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if !strings.Contains(read(t, path), `"message":"hello"`) {
		t.Errorf("the record did not reach the file: %q", read(t, path))
	}
}

func TestOpenWritesToBothSinks(t *testing.T) {
	path := filepath.Join(logDir(t), "sidecar.log")
	logger, errBuf, err := open(t, path, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	logger.Info("hello")

	if !strings.Contains(read(t, path), `"message":"hello"`) {
		t.Errorf("the record did not reach the file: %q", read(t, path))
	}
	if !strings.Contains(errBuf.String(), `"message":"hello"`) {
		t.Errorf("the record did not reach stderr: %q", errBuf.String())
	}
}

// No path: stderr is the whole log.
func TestOpenWithNoPathWritesToStderr(t *testing.T) {
	logger, errBuf, err := open(t, "", true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	logger.Info("hello")

	if !strings.Contains(errBuf.String(), `"message":"hello"`) {
		t.Errorf("the record did not reach stderr: %q", errBuf.String())
	}
}

// lumberjack opens on the first write and slog discards a handler's write error, so a bad
// path would swallow every record forever. Open probes it instead, and still returns a
// logger — a sidecar that cannot log is not a sidecar that should refuse to start.
func TestOpenReportsAnUnusablePathAndStillLogs(t *testing.T) {
	dir := logDir(t)
	if err := os.Mkdir(filepath.Join(dir, "taken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wall"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(dir, "taken"),              // a directory, not a file
		filepath.Join(dir, "wall", "nested.log"), // a file where the parent would go
	} {
		logger, errBuf, err := open(t, path, false)
		if err == nil {
			t.Errorf("Open(%q) = nil error, want one", path)
		}
		if logger == nil {
			t.Fatalf("Open(%q) returned no logger", path)
		}
		logger.Info("hello")
		if !strings.Contains(errBuf.String(), `"message":"hello"`) {
			t.Errorf("the fallback logger does not write to stderr: %q", errBuf.String())
		}
	}
}

func TestOpenCreatesTheFileBeforeTheFirstRecord(t *testing.T) {
	path := filepath.Join(logDir(t), "sidecar.log")
	if _, _, err := open(t, path, false); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the probe should have created the file: %v", err)
	}
}

// The directory is the log's gate, so Open never makes one: a missing directory leaves the
// log on stderr.
func TestOpenRefusesAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	if _, _, err := open(t, filepath.Join(dir, "sidecar.log"), false); err == nil {
		t.Fatal("Open = nil error, want one")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Open made the directory: %v", err)
	}
}

// brokenWriter is a sink that has run out of disk.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// A full disk or a failed rotation must not take the terminal copy with it. slog discards a
// handler's write error, so a sink that gave up at the first failure would drop the record
// from both places without a word. Driven through the writer rather than through Open: a real
// lumberjack write is not something a test can make fail.
func TestBothSinksReachesStderrWhenTheFileFails(t *testing.T) {
	var errBuf bytes.Buffer

	Init(bothSinks{file: brokenWriter{}, stderr: &errBuf}, slog.LevelInfo).Info("hello")

	if !strings.Contains(errBuf.String(), `"message":"hello"`) {
		t.Errorf("a failed file write suppressed the terminal record: %q", errBuf.String())
	}
}

// And the other way round: a closed terminal is not a reason to stop keeping the log.
func TestBothSinksReachesTheFileWhenStderrFails(t *testing.T) {
	var fileBuf bytes.Buffer

	Init(bothSinks{file: &fileBuf, stderr: brokenWriter{}}, slog.LevelInfo).Info("hello")

	if !strings.Contains(fileBuf.String(), `"message":"hello"`) {
		t.Errorf("a failed stderr write suppressed the file record: %q", fileBuf.String())
	}
}

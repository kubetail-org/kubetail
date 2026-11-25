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
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Where the live file rotates and how many archives survive, matching the host's main.log.
// Rotation is by size, so a bad day costs the same disk as a quiet week.
const (
	maxFileMB  = 2
	maxBackups = 5
)

// Open builds the logger the command line describes: a rotating file at path, stderr, or
// both. An empty path means no file.
//
// It never leaves the caller without a logger. A path that cannot be opened yields the
// stderr-only logger *and* the error, so the caller installs what it gets and reports the
// failure through it. Close closes the file, and is a no-op when there is none.
func Open(path string, stderr bool, level slog.Level) (*slog.Logger, func() error, error) {
	return openTo(os.Stderr, path, stderr, level)
}

// openTo is Open with the stderr sink injected, so a test can read what reached it.
func openTo(errSink io.Writer, path string, stderr bool, level slog.Level) (*slog.Logger, func() error, error) {
	noClose := func() error { return nil }
	if path == "" {
		return Init(errSink, level), noClose, nil
	}

	if err := probe(path); err != nil {
		return Init(errSink, level), noClose, err
	}

	file := &lumberjack.Logger{Filename: path, MaxSize: maxFileMB, MaxBackups: maxBackups}
	var w io.Writer = file
	if stderr {
		w = bothSinks{file: file, stderr: errSink}
	}
	return Init(w, level), file.Close, nil
}

// bothSinks writes a record to both sinks whether or not the other one takes it. Not
// io.MultiWriter: it stops at the first sink that fails, so a full disk or a failed
// rotation would take the terminal copy with it, and slog discards the error that would
// have said so.
type bothSinks struct{ file, stderr io.Writer }

func (b bothSinks) Write(p []byte) (int, error) {
	n, err := b.file.Write(p)
	if _, e := b.stderr.Write(p); err == nil {
		return n, e
	}
	return n, err
}

// probe does the first write's work up front. lumberjack opens the file on its first Write
// and slog discards a handler's write error, so a path that cannot be opened would swallow
// every record for the life of the process without a word.
//
// The directory is the log's gate: the log holds cluster names and server hostnames. probe
// never makes it, and on Unix refuses one other users can reach, so whoever made it owner-only
// vouches for it. On Windows the profile's ACL is the gate.
func probe(path string) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("log directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("log directory %s is not a directory", dir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("log directory %s is open to other users (%#o)", dir, info.Mode().Perm())
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("log file: %w", err)
	}
	return f.Close()
}

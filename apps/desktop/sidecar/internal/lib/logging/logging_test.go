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

package logging_test

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"garbage", slog.LevelInfo}, // default-on-unknown rather than fail-closed
	}
	for _, c := range cases {
		if got := logging.ParseLevel(c.in); got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestInitWritesOneJSONObjectAtConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.Init(&buf, slog.LevelInfo)
	logger.Info("hello", "k", "v")

	line := buf.String()
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("want exactly one record on one line, got %q", line)
	}
	for _, want := range []string{`"level":"INFO"`, `"message":"hello"`, `"k":"v"`} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q: %s", want, line)
		}
	}
	// sidecar.log holds nothing else, so naming the process on every record is
	// bytes spent on what the filename already says.
	if strings.Contains(line, `"target"`) {
		t.Errorf("records should carry no target: %s", line)
	}
}

// The one thing keeping a cluster-controlled string from forging a log line, now that the
// host no longer sanitizes what the sidecar writes.
func TestInitWritesOneLinePerRecord(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.Init(&buf, slog.LevelInfo)

	logger.Info("first\nsecond\x1b[31m", "attr", "third\nfourth\x1b[31m")

	line := buf.String()
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("a control character forged a line: %q", line)
	}
	if strings.ContainsRune(line, 0x1b) {
		t.Errorf("an escape byte reached the writer raw: %q", line)
	}
	// Escaped, not dropped: the diagnostic still has to be readable.
	if n := strings.Count(line, `\n`); n != 2 {
		t.Errorf("want both newlines escaped, got %d: %s", n, line)
	}
}

// Both files are read side by side, so a reader can only line them up if the sidecar
// stamps its records the way the host stamps its own: UTC, RFC 3339, microseconds.
func TestInitStampsTimeAsUTCMicroseconds(t *testing.T) {
	var buf bytes.Buffer
	logging.Init(&buf, slog.LevelInfo).Info("hello")

	m := regexp.MustCompile(`"timestamp":"([^"]+)"`).FindStringSubmatch(buf.String())
	if m == nil {
		t.Fatalf("no timestamp: %s", buf.String())
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$`).MatchString(m[1]) {
		t.Errorf("time = %q, want UTC RFC 3339 with microseconds", m[1])
	}
	if _, err := time.Parse(time.RFC3339Nano, m[1]); err != nil {
		t.Errorf("time %q does not parse: %v", m[1], err)
	}
}

func TestInitSuppressesBelowLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.Init(&buf, slog.LevelInfo)
	logger.Debug("not shown")
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

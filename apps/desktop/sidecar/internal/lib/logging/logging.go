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

// Package logging configures the sidecar's structured logger.
//
// Records go where the command line says — a file, stderr, or both. stdout is never a sink:
// it is reserved for the READY IPC line.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// stamp is the host's timestamp format: UTC, RFC 3339, microseconds.
const stamp = "2006-01-02T15:04:05.000000Z"

// Init returns a logger emitting one JSON object per record to w, keyed the way the
// host's tracing keys its own (see hostKeys). Everything it writes is rendered through
// internal/lib/safe on the way out (see redact.go), and the JSON encoder escapes control
// characters, so a cluster-controlled string cannot forge a line.
//
// Records carry no name for the process: sidecar.log holds nothing else, and the host
// labels the lines it renders from the pipes itself.
func Init(w io.Writer, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: hostKeys})
	return slog.New(redactHandler{h})
}

// hostKeys renames slog's built-in keys to tracing's, so one record from either process
// is one shape. The timestamp is formatted here for the same reason: slog would write
// nanoseconds and a local offset.
func hostKeys(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		if a.Value.Kind() == slog.KindTime {
			return slog.String("timestamp", a.Value.Time().UTC().Format(stamp))
		}
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// ParseLevel maps KUBETAIL_LOG_LEVEL to a slog.Level; an unknown value falls back to Info
// so a typo can't silence the logger.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

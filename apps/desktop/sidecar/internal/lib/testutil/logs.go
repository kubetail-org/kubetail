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

package testutil

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
)

// LogCapture holds everything the default logger emitted for the length of one test — the
// encoded bytes, since that is what the sink writes to the sidecar's log file.
type LogCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// CaptureLogs redirects the default slog logger into a LogCapture until the test ends. A
// test using it must not run in parallel: the default logger is process-wide.
//
// Built by logging.Init, so what a sentinel reads is what the process would actually have
// written — a plain handler would report a leak the production sink does not have.
func CaptureLogs(t testing.TB) *LogCapture {
	t.Helper()
	c := &LogCapture{}
	prev := slog.Default()
	slog.SetDefault(logging.Init(c, slog.LevelDebug))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return c
}

// Write records one encoded record. The handler may be called from any goroutine the code
// under test logs on.
func (c *LogCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *LogCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

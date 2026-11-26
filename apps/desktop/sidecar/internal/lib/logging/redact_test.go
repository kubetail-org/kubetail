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
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// The boundary that matters: the bytes this handler writes are what reaches the log file.
// A rendered error must survive encoding with the credential still gone — a Rust test in
// logs.rs would only prove the forwarder forwards.
func TestInitWritesNoCredentialFromARenderedError(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo)

	err := errors.New(`Post "https://issuer.example/token?client_secret=SEKRIT": 401 Unauthorized`)
	logger.Error("cloud sign-in failed", "err", safe.Safe(err))

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("the credential reached the writer: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "401 Unauthorized") {
		t.Errorf("the diagnostic did not survive: %s", buf.String())
	}
}

// The backstop. Every call site of ours renders its own error, but the loggers we do not
// own — beehive's verdicts, client-go, oauth2 — log through this handler and pass theirs
// whole. Message, attr, With-attr and group are the four positions a record has, and the
// count is what proves the value was rendered rather than dropped.
func TestInitRedactsWhatACallerDidNotRender(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo).With("cache", "https://api.example.com/api?token=SEKRIT")

	logger.Error(`reconcile "https://api.example.com/x?token=SEKRIT" failed`,
		"err", errors.New(`Get "https://api.example.com/readyz?token=SEKRIT": 401 Unauthorized`),
		slog.Group("request", "url", "https://api.example.com/y?token=SEKRIT"))

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("a raw error reached the writer: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "401 Unauthorized") {
		t.Errorf("the diagnostic did not survive: %s", buf.String())
	}
	if n := strings.Count(buf.String(), "api.example.com"); n != 4 {
		t.Errorf("want all four positions rendered and kept, got %d: %s", n, buf.String())
	}
}

// A group inside a group: only recursion reaches the leaf. WithGroup is not this case —
// the inner handler does that nesting, and the attr still arrives as a plain string.
func TestInitRedactsAGroupNestedInAGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo)

	logger.Info("probe", slog.Group("outer",
		slog.Group("inner", "url", "https://api.example.com/z?token=SEKRIT")))

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("a nested group reached the writer unrendered: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"outer":{"inner":{"url":`) {
		t.Errorf("the group structure was not preserved: %s", buf.String())
	}
}

// WithGroup's own path: the handler must hand the name to the inner handler rather than
// swallow it, or every field logged after one collapses into the record's top level.
func TestInitKeepsWithGroupNesting(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo).WithGroup("outer").WithGroup("inner")

	logger.Info("probe", "url", "https://api.example.com/z?token=SEKRIT")

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("the credential reached the writer: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"outer":{"inner":{"url":`) {
		t.Errorf("the group structure was not preserved: %s", buf.String())
	}
}

// A LogValuer produces its value after the handler chain would otherwise be done, so the
// renderer resolves first. Without that, the credential is encoded past it.
func TestInitRendersAResolvedLogValuer(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo)

	logger.Info("dial", "endpoint", lazyEndpoint{})

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("a LogValuer's value reached the writer unrendered: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "api.example.com") {
		t.Errorf("the diagnostic did not survive: %s", buf.String())
	}
}

type lazyEndpoint struct{}

func (lazyEndpoint) LogValue() slog.Value {
	return slog.StringValue("https://api.example.com/api?token=SEKRIT")
}

// A struct is encoded field by field past the renderer, so the sink reduces it to its type.
// Written as a loose pair because that is how every call site in the sidecar logs: the
// stdlib turns the pair into slog.Any before the handler sees it.
func TestInitReducesAStructToItsType(t *testing.T) {
	var buf bytes.Buffer
	logger := Init(&buf, slog.LevelInfo)

	logger.Info("cache state", "state", cacheConfig{Server: "https://api.example.com", Token: "SEKRIT"})

	if strings.Contains(buf.String(), "SEKRIT") {
		t.Errorf("a struct field reached the writer: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "<unrendered logging.cacheConfig>") {
		t.Errorf("want the type named, got: %s", buf.String())
	}
}

type cacheConfig struct {
	Server string
	Token  string
}

// The reduction sits at the sink, so it does not matter how the value got there. These are
// the other three ways in: the explicit call the stdlib writes for a loose pair, an attr
// carried on the logger, and one nested in a group.
func TestInitReducesAStructWhicheverWayItArrives(t *testing.T) {
	value := cacheConfig{Server: "https://api.example.com", Token: "SEKRIT"}

	for _, tt := range []struct {
		name string
		log  func(*slog.Logger)
	}{
		{"slog.Any", func(l *slog.Logger) { l.Info("cache state", slog.Any("state", value)) }},
		{"With", func(l *slog.Logger) { l.With("state", value).Info("cache state") }},
		{"in a group", func(l *slog.Logger) { l.Info("cache state", slog.Group("cache", "state", value)) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(Init(&buf, slog.LevelInfo))

			if strings.Contains(buf.String(), "SEKRIT") {
				t.Errorf("a struct field reached the writer: %s", buf.String())
			}
			if !strings.Contains(buf.String(), "<unrendered logging.cacheConfig>") {
				t.Errorf("want the type named, got: %s", buf.String())
			}
		})
	}
}

// recorder stands in for the encoder: it keeps the attrs redactHandler hands down. The three
// tests below read it rather than the output, because a text line renders 7, true and 1s the
// same whether they arrived as values or as strings, and has no null at all.
type recorder struct{ attrs map[string]slog.Value }

func newRecorder() *recorder { return &recorder{attrs: map[string]slog.Value{}} }

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	rec.Attrs(func(a slog.Attr) bool {
		r.attrs[a.Key] = a.Value
		return true
	})
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func log(h slog.Handler, msg string, args ...any) {
	slog.New(redactHandler{h}).Info(msg, args...)
}

// Only strings and errors are rendered. A number, a bool or a duration must reach the
// encoder as its own kind — a renderer that stringified everything would quietly turn every
// log field into text.
func TestRedactLeavesNonTextValuesTyped(t *testing.T) {
	rec := newRecorder()

	log(rec, "sweep", "cacheID", 7, "stopped", true, "took", 250*time.Millisecond)

	for key, want := range map[string]slog.Kind{
		"cacheID": slog.KindInt64,
		"stopped": slog.KindBool,
		"took":    slog.KindDuration,
	} {
		if got := rec.attrs[key].Kind(); got != want {
			t.Errorf("%s arrived as %v, want %v", key, got, want)
		}
	}
	if got := rec.attrs["took"].Duration(); got != 250*time.Millisecond {
		t.Errorf("took = %v, want 250ms", got)
	}
}

// A nil error is an absence, not a value. Without the nil guard the struct reduction
// rewrites it to a string, turning "nothing failed" into log noise.
func TestRedactLeavesANilErrorAlone(t *testing.T) {
	rec := newRecorder()

	var err error
	log(rec, "sweep", "err", err)

	v, ok := rec.attrs["err"]
	if !ok {
		t.Fatalf("the attr was dropped: %v", rec.attrs)
	}
	if v.Kind() != slog.KindAny || v.Any() != nil {
		t.Errorf("want a nil value passed through, got %v (%v)", v, v.Kind())
	}
}

// The guard is on the nil, not on the type: an error that is there is still rendered.
func TestRedactRendersANonNilError(t *testing.T) {
	rec := newRecorder()

	log(rec, "sweep", "err", errors.New("Get \"https://api.example.com/readyz?token=SEKRIT\": 401"))

	if got := rec.attrs["err"].String(); got == "" || strings.Contains(got, "SEKRIT") {
		t.Errorf("want the error rendered without the credential, got %q", got)
	}
}

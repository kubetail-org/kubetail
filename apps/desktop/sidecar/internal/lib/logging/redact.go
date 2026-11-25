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
	"context"
	"fmt"
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// redactHandler renders every string a record carries before the inner handler encodes it.
//
// Our own call sites render their errors themselves, which is what makes a diff reviewable;
// this is the backstop for the loggers we do not own — beehive's verdicts, client-go,
// oauth2 — which reach the same sink and pass their errors whole. A value that is neither
// string nor error it cannot read, so it logs the type in place of the value rather than
// letting the encoder write every field.
type redactHandler struct{ inner slog.Handler }

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, safe.String(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	rendered := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		rendered[i] = redactAttr(a)
	}
	return redactHandler{h.inner.WithAttrs(rendered)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{h.inner.WithGroup(name)}
}

// redactAttr renders one attribute. Resolve first, so a LogValuer's value is rendered
// rather than the wrapper that would produce it after this handler is done.
func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, safe.String(v.String()))
	case slog.KindGroup:
		group := v.Group()
		rendered := make([]any, len(group))
		for i, g := range group {
			rendered[i] = redactAttr(g)
		}
		return slog.Group(a.Key, rendered...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, safe.Safe(err))
		}
		// Anything else the JSON handler would encode field by field, past this
		// renderer. Name the type; a caller who wants the value renders it themselves.
		// A nil falls through, so an absent error still encodes as null.
		if v.Any() != nil {
			return slog.String(a.Key, fmt.Sprintf("<unrendered %T>", v.Any()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}

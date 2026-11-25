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

// Package lifecycle is the shared start/stop/close shape, and the helpers that
// compose a level of it. Every service and leaf in the sidecar wears it, so an owner
// holds its parts in one slice instead of a hand-written stop func per owner.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
)

// StartCloser is the three-phase shape, and it means the same at every level: Start
// bounds startup with ctx and returns the func that drains the background work, taking
// a drain deadline; Close releases what the drain left. Two methods, three phases — stop
// is Start's return value, so it cannot be called before there is anything to stop.
//
// ctx bounds STARTUP ONLY. The background work must outlive it and end only when the
// stop func runs, so an owner can time-limit startup without killing what started.
//
// A stop func must be idempotent, and must wait with drain.WithContext rather than a
// bare wg.Wait: the helpers below call them uniformly, so a retry or a double drain
// anywhere above must neither panic nor return before the work is joined.
type StartCloser interface {
	Start(ctx context.Context) (func(context.Context) error, error)
	io.Closer
}

// StartFunc adapts a service whose stop func releases everything, so it has nothing left
// to Close. Wrapping it here keeps the empty Close out of that service's own API.
type StartFunc func(ctx context.Context) (func(context.Context) error, error)

func (f StartFunc) Start(ctx context.Context) (func(context.Context) error, error) {
	return f(ctx)
}

func (StartFunc) Close() error { return nil }

// CloseFunc adapts something with no background work, so it has nothing to Start:
// a handle the owner opened itself and releases after every part that uses it.
type CloseFunc func() error

func (CloseFunc) Start(context.Context) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}

func (f CloseFunc) Close() error { return f() }

// None supplies the lifecycle for something with no background work. Embed it, and
// override either method when it grows some.
type None struct{}

func (None) Start(context.Context) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}

func (None) Close() error { return nil }

// Part is one participant and the name every phase reports it under. The underlying
// error is often generic — "already stopped", a wrapped io error — and reaches the
// process log as one line, so the name is what makes a failure attributable.
type Part struct {
	Name string
	StartCloser
}

// StartAll starts each in order and returns their stop funcs. A failure unwinds
// whatever already started, and the stop func it returns is then a no-op — truthful,
// since the unwind already drained them, and it spares every caller from having to
// know that a `defer stop(ctx)` written above the error check would be nil. Closing
// stays the caller's either way.
func StartAll(ctx context.Context, parts []Part) (func(context.Context) error, error) {
	stops := make([]func(context.Context) error, 0, len(parts))
	for _, p := range parts {
		stop, err := p.Start(ctx)
		if err != nil {
			return noStop, errors.Join(fmt.Errorf("start %s: %w", p.Name, err), unwind(ctx, stops))
		}
		stops = append(stops, func(ctx context.Context) error {
			if err := stop(ctx); err != nil {
				return fmt.Errorf("stop %s: %w", p.Name, err)
			}
			return nil
		})
	}
	return func(ctx context.Context) error { return stopAll(ctx, stops) }, nil
}

// noStop is what a failed StartAll hands back in place of nil.
func noStop(context.Context) error { return nil }

// unwindTimeout bounds the unwind of a failed start. A budget of its own because the
// caller never receives a stop func in that case, so there is no drain deadline to
// inherit.
const unwindTimeout = 10 * time.Second

// unwind drains what already started, on a context detached from the startup one.
// The usual reason a start fails is that ctx died, and draining on a dead context
// returns the instant it is asked to wait — signalling the background work to stop
// and reporting it drained while it is still running, just as the caller is free to
// close what it is using.
func unwind(ctx context.Context, stops []func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unwindTimeout)
	defer cancel()
	return stopAll(ctx, stops)
}

// stopAll runs stops in reverse, joining every error rather than returning at the
// first: a stop that fails must not leave the rest running.
func stopAll(ctx context.Context, stops []func(context.Context) error) error {
	var errs []error
	for _, stop := range slices.Backward(stops) {
		errs = append(errs, stop(ctx))
	}
	return errors.Join(errs...)
}

// CloseAll closes in reverse, mirroring stopAll.
func CloseAll(parts []Part) error {
	var errs []error
	for _, part := range slices.Backward(parts) {
		if err := part.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", part.Name, err))
		}
	}
	return errors.Join(errs...)
}

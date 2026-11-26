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

// Package poke is a cross-subsystem resync broadcaster: a wall-clock gap detector (the
// sleep/resume backstop) fanning Signals to all subscribers. A poke is a FAN-OUT, never a
// cascade through spec counters or conditions.
// See docs/adr/2026-08-09-poke-resync-fanout.md.
package poke

import (
	"context"
	"time"

	"github.com/amorey/gochan/broadcast"
)

const hubCapacity = 2

// Source identifies what triggered a poke.
type Source string

const (
	// SourceWallClock: the gap detector inferred a sleep/resume from a clock jump.
	SourceWallClock Source = "wallclock"
	// SourceHost: the Tauri host forwarded an OS wake or network-on event.
	SourceHost Source = "host"
)

// Notification is broadcast to all subscribers on every poke.
type Notification struct {
	Source Source
	At     time.Time
}

// options configures a Service; zero values get defaults. Unexported because production
// tunes nothing — only white-box tests inject seams.
type options struct {
	tick      time.Duration // gap-detector heartbeat (default 15s)
	gapFactor float64       // wall gap > tick*gapFactor ⇒ treat as resume (default 2.0)
	now       func() time.Time
	newTicker func(time.Duration) (<-chan time.Time, func())
}

func (o *options) applyDefaults() {
	if o.tick <= 0 {
		o.tick = 15 * time.Second
	}
	if o.gapFactor <= 0 {
		o.gapFactor = 2.0
	}
	if o.now == nil {
		o.now = time.Now
	}
	if o.newTicker == nil {
		o.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		}
	}
}

// option is an unexported build seam; production New takes none.
type option func(*options)

// withTick overrides the gap-detector heartbeat (default 15s).
func withTick(d time.Duration) option { return func(o *options) { o.tick = d } }

// withGapFactor overrides the resume-detection factor (default 2.0).
func withGapFactor(f float64) option { return func(o *options) { o.gapFactor = f } }

// withNow overrides the clock seam.
func withNow(f func() time.Time) option { return func(o *options) { o.now = f } }

// withTicker overrides the ticker seam.
func withTicker(f func(time.Duration) (<-chan time.Time, func())) option {
	return func(o *options) { o.newTicker = f }
}

// Service fans resync pokes to all subscribers; Start (or Run) launches the gap detector.
// Poke is safe from any goroutine at any time.
type Service struct {
	hub *broadcast.Hub[Notification]
	tx  *broadcast.Sender[Notification]
	opt options
}

// New builds a Service with production defaults; it starts nothing — call Start or Run.
func New() *Service {
	return newWithOptions()
}

// newWithOptions is New plus the unexported test seams.
func newWithOptions(opts ...option) *Service {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	o.applyDefaults()
	hub := broadcast.New[Notification](hubCapacity)
	return &Service{
		hub: hub,
		tx:  hub.Sender(),
		opt: o,
	}
}

// Poke broadcasts a Signal to all subscribers; never blocks.
func (s *Service) Poke(src Source) {
	_ = s.tx.Send(Notification{Source: src, At: s.opt.now()})
}

// ListenForNotifications returns a Notification channel and its cancel func; the channel closes on cancel or
// when Run's context ends. Cancel is REQUIRED — abandoning the channel leaks the feeder
// goroutine.
func (s *Service) ListenForNotifications() (<-chan Notification, func()) {
	rx := s.hub.Receiver()
	return rx.Chan(), rx.Close
}

// Start runs the detector in a goroutine; the returned stop cancels it and blocks until
// it exits (closing the hub and every subscriber channel), bounded by its own context.
// Call once.
//
// ctx bounds startup alone, as lifecycle.StartCloser requires: the detector runs on a
// context detached from it, so a caller can time-limit startup without killing the bus.
func (s *Service) Start(ctx context.Context) (func(context.Context) error, error) {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(runCtx)
	}()

	stop := func(ctx context.Context) error {
		cancel()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return stop, nil
}

// Run blocks on the gap detector until ctx is cancelled, closing the hub on exit. Call
// once per Service (Start wraps it).
func (s *Service) Run(ctx context.Context) {
	tickC, stop := s.opt.newTicker(s.opt.tick)
	defer stop()

	lastSeen := s.opt.now()
	threshold := time.Duration(float64(s.opt.tick) * s.opt.gapFactor)

	for {
		select {
		case <-ctx.Done():
			s.hub.Close()
			return
		case <-tickC:
			now := s.opt.now()
			if now.Sub(lastSeen) > threshold {
				s.Poke(SourceWallClock)
			}
			lastSeen = now
		}
	}
}

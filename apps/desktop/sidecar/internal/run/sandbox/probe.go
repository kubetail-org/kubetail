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

package sandbox

import (
	"context"
	"sync"
)

// probePolicy is what a probe's run reaches before any run exists: System over
// its shell, its folder dir writable, and the Never paths under home denied.
// It names no Kubetail path and no relay.
func (s *Sandbox) probePolicy(shell, dir, home string) Policy {
	files := s.System(home, shell).Files
	files.Write = append(files.Write, dir)
	return Policy{Files: files, Always: AlwaysPolicy{Deny: s.Never(home)}}
}

// probePolicyWithin is probePolicy, or ctx's error if ctx ends first: the
// policy reads folders under the home and lists the other homes.
func (s *Sandbox) probePolicyWithin(ctx context.Context, shell, dir, home string) (Policy, error) {
	build := s.buildProbePolicy
	if build == nil {
		build = s.probePolicy
	}
	return bounded(ctx, func() (Policy, error) {
		return build(shell, dir, home), nil
	})
}

// bounded is f's answer, or ctx's error if ctx ends first. f reads paths,
// which can hang on a network mount, so it runs on a goroutine left behind
// then. Each one left behind holds a thread in its syscall, so at most
// maxBoundedCalls run at once; past that, a caller waits for one to end until
// ctx does.
func bounded[T any](ctx context.Context, f func() (T, error)) (T, error) {
	type answer struct {
		value T
		err   error
	}
	var zero T
	select {
	case boundedSlots <- struct{}{}:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	// Both cases are ready when ctx has ended and a slot is free, and a
	// call nobody waits for must not take the slot.
	if err := ctx.Err(); err != nil {
		<-boundedSlots
		return zero, err
	}
	done := make(chan answer, 1)
	boundedCalls.Go(func() {
		defer func() { <-boundedSlots }()
		value, err := f()
		done <- answer{value, err}
	})
	select {
	case a := <-done:
		return a.value, a.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// maxBoundedCalls is how many calls bounded runs at once.
const maxBoundedCalls = 16

// boundedSlots holds a token for each call bounded is running.
var boundedSlots = make(chan struct{}, maxBoundedCalls)

// boundedCalls counts the calls bounded still runs, which tests wait on.
var boundedCalls sync.WaitGroup

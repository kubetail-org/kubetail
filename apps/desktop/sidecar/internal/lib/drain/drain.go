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

// Package drain is the shared shutdown primitive: wait for in-flight work, but no longer
// than the caller's deadline.
package drain

import "context"

// WithContext blocks until wait returns (nil) or ctx is done. wait must return on its own
// — typically a WaitGroup.Wait over already-signalled handlers — since ctx bounds only
// how long the CALLER waits, not wait itself.
func WithContext(ctx context.Context, wait func()) error {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

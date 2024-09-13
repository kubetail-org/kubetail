// Copyright 2024 Andres Morey
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

package throttler

import (
	"context"
	"fmt"
	"time"
)

type CallbackFunctionType func()

// Facilitates throttling function callbacks with both leading and trailing edge execution.
//
// * Processes the first callback in a group immediately when a burst of events begins
// * Coalesces intermediate callbacks
// * Processes the last callback after the burst has ended (i.e., after a period of inactivity)
type Throttler struct {
	callbackCh chan CallbackFunctionType
}

func (t *Throttler) Do(fn func()) {
	fmt.Println(t.callbackCh)
	t.callbackCh <- fn
	fmt.Println("Xxx")
}

// Returns new Throttler instance
func NewThrottler(ctx context.Context) *Throttler {
	callbackCh := make(chan CallbackFunctionType, 100)

	debounceDuration := 1 * time.Second
	var debounceTimer *time.Timer
	var debounceChan <-chan time.Time
	var lastCallbackFn func()
	inBurst := false

	go func() {
		fmt.Println("running")
		for {
			select {
			case <-ctx.Done():
				return
			case callbackFn := <-callbackCh:
				fmt.Println("callbackFn")
				if !inBurst {
					// First event in a burst
					inBurst = true
					lastCallbackFn = nil
					callbackFn()
					debounceTimer = time.NewTimer(debounceDuration)
				} else {
					// Subsequent events in the burst
					lastCallbackFn = callbackFn
					if !debounceTimer.Stop() {
						<-debounceTimer.C
					}
					debounceTimer.Reset(debounceDuration)
				}
			case <-debounceChan:
				fmt.Println("debounceTimer")
				inBurst = false
				debounceTimer = nil
				if lastCallbackFn != nil {
					lastCallbackFn()
					lastCallbackFn = nil
				}
			}
		}
	}()

	return &Throttler{callbackCh: callbackCh}
}

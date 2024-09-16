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

package debounce

import (
	"context"
	"sync"
	"time"

	"github.com/zmwangx/debounce"
)

func DebounceByKey[K comparable, T any](ctx context.Context, wait time.Duration, actionFn func(T)) func(K, T) {
	var mu sync.Mutex

	type cacheVal struct {
		debounceFn func(...T) error
		controller debounce.ControlWithReturnValue[error]
	}

	// init cache
	cache := make(map[K]*cacheVal)

	// cancel all controllers when context closes
	go func() {
		<-ctx.Done()

		mu.Lock()
		defer mu.Unlock()

		for _, val := range cache {
			val.controller.Cancel()
		}
	}()

	// return debounce function
	return func(key K, input T) {
		mu.Lock()

		// exit if context is finished
		if ctx.Err() != nil {
			mu.Unlock()
			return
		}

		val, exists := cache[key]
		if !exists {
			// initialize new debouncer
			debounceFn, controller := debounce.DebounceWithCustomSignature(
				func(inputs ...T) error {
					actionFn(inputs[0])
					return nil
				},
				wait,
				debounce.WithLeading(true),
				debounce.WithTrailing(true),
			)
			val = &cacheVal{debounceFn, controller}
			cache[key] = val
		}

		mu.Unlock()

		// call debounced action function
		val.debounceFn(input)
	}
}

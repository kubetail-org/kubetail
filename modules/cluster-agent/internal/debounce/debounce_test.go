// Copyright 2024-2025 Andres Morey
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
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type DebounceFnType func(s string, i int)

func TestDebounceByKey(t *testing.T) {
	t.Run("executes leading edge", func(t *testing.T) {
		var mu sync.Mutex
		args := []int{}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		debounce := DebounceByKey[string](ctx, 10*time.Millisecond, func(i int) {
			mu.Lock()
			defer mu.Unlock()
			args = append(args, i)
		})

		debounce("key_1", 11)
		debounce("key_1", 12)
		debounce("key_1", 13)

		debounce("key_2", 21)
		debounce("key_2", 22)
		debounce("key_2", 23)

		time.Sleep(3 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		sort.Ints(args)
		require.Equal(t, []int{11, 21}, args)
	})

	t.Run("executes trailing edge", func(t *testing.T) {
		var mu sync.Mutex
		args := []int{}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		debounce := DebounceByKey[string](ctx, 10*time.Millisecond, func(i int) {
			mu.Lock()
			defer mu.Unlock()
			args = append(args, i)
		})

		debounce("key_1", 11)
		debounce("key_1", 12)
		debounce("key_1", 13)

		debounce("key_2", 21)
		debounce("key_2", 22)
		debounce("key_2", 23)

		time.Sleep(15 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		sort.Ints(args)
		require.Equal(t, []int{11, 13, 21, 23}, args)
	})

	t.Run("resets wait time", func(t *testing.T) {
		var mu sync.Mutex
		args := []int{}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		debounce := DebounceByKey[string](ctx, 10*time.Millisecond, func(i int) {
			mu.Lock()
			defer mu.Unlock()
			args = append(args, i)
		})

		debounce("key_1", 11)
		debounce("key_2", 21)

		time.Sleep(15 * time.Millisecond)

		debounce("key_1", 12)
		debounce("key_2", 22)

		time.Sleep(15 * time.Millisecond)

		debounce("key_1", 13)
		debounce("key_2", 23)

		mu.Lock()
		defer mu.Unlock()

		sort.Ints(args)
		require.Equal(t, []int{11, 12, 13, 21, 22, 23}, args)
	})
}

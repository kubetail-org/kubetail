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

package appdb

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ids sort in the order they were minted.
func TestNewIDIncreasesInGenerationOrder(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = NewID()
	}
	for i := 1; i < len(ids); i++ {
		require.Less(t, ids[i-1], ids[i], "id %d", i)
	}
	for _, id := range ids {
		require.NoError(t, ValidateUUID(id))
	}
}

// Concurrent minting never repeats. Which goroutine finished first says nothing
// about which id was minted first, so only uniqueness is asserted.
func TestNewIDIsUniqueAcrossGoroutines(t *testing.T) {
	const workers, each = 8, 500
	var mu sync.Mutex
	var wg sync.WaitGroup
	all := make([]string, 0, workers*each)
	for range workers {
		wg.Go(func() {
			ids := make([]string, each)
			for i := range ids {
				ids[i] = NewID()
			}
			mu.Lock()
			all = append(all, ids...)
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.Sort(all)
	assert.Equal(t, len(all), len(slices.Compact(all)))
}

// The canonical spellings of a v4 and a v7 pass, and nothing else does.
func TestParseIDAcceptsCanonicalV4AndV7Only(t *testing.T) {
	v4 := uuid.New().String()
	v7 := NewID()
	require.NoError(t, ValidateUUID(v4))
	require.NoError(t, ValidateUUID(v7))

	rejected := map[string]string{
		"empty":      "",
		"nil":        uuid.Nil.String(),
		"uppercase":  strings.ToUpper(v7),
		"braces":     "{" + v7 + "}",
		"urn":        "urn:uuid:" + v7,
		"bare hex":   strings.ReplaceAll(v7, "-", ""),
		"ulid":       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"v1":         "c232ab00-9414-11ec-b3c8-9f6bdeced846",
		"v3":         uuid.NewMD5(uuid.NameSpaceDNS, []byte("x")).String(),
		"v5":         uuid.NewSHA1(uuid.NameSpaceDNS, []byte("x")).String(),
		"non-rfc":    v7[:19] + "c" + v7[20:],
		"trailing":   v7 + "\n",
		"short":      v7[:35],
		"not a uuid": "chat-1",
	}
	for name, s := range rejected {
		assert.Error(t, ValidateUUID(s), name)
	}
}

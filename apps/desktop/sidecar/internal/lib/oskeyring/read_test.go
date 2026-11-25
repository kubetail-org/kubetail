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

package oskeyring_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// Read answers the keyring's value, and its error.
func TestReadAnswersTheKeyring(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	require.NoError(t, k.Set("a", "v"))

	v, err := oskeyring.Read(t.Context(), k, "a", oskeyring.SlowAfter)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	_, err = oskeyring.Read(t.Context(), k, "b", oskeyring.SlowAfter)
	assert.ErrorIs(t, err, oskeyring.ErrNoEntry)
}

// A Read the keyring holds answers its context's error when the context ends.
func TestReadEndsWithItsContext(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	t.Cleanup(k.Hold(oskeyringtest.Get))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := oskeyring.Read(ctx, k, "a", oskeyring.SlowAfter)
	assert.ErrorIs(t, err, context.Canceled)
}

// A slow Read logs once, naming the account and not the value, and keeps
// waiting. Serial: it captures the process's logger.
func TestReadLogsASlowKeyringOnce(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	synctest.Test(t, func(t *testing.T) {
		k := oskeyringtest.New()
		require.NoError(t, k.Set("account-a", "secret-value"))
		release := k.Hold(oskeyringtest.Get)
		got := make(chan string, 1)
		go func() {
			v, _ := oskeyring.Read(t.Context(), k, "account-a", time.Second)
			got <- v
		}()

		time.Sleep(5 * time.Second) // the bubble's clock
		synctest.Wait()
		assert.Equal(t, 1, strings.Count(logs.String(), "keyring: still waiting for the keyring"))
		assert.Contains(t, logs.String(), "account-a")
		assert.NotContains(t, logs.String(), "secret-value")

		release()
		assert.Equal(t, "secret-value", <-got)
	})
}

// Delete deletes, and answers the keyring's error.
func TestDeleteAnswersTheKeyring(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	require.NoError(t, k.Set("a", "v"))
	require.NoError(t, oskeyring.Delete(t.Context(), k, "a"))
	assert.Empty(t, k.Entries())

	boom := errors.New("boom")
	k.FailDelete = boom
	assert.ErrorIs(t, oskeyring.Delete(t.Context(), k, "a"), boom)
}

// A Delete the keyring holds answers its context's error when the context ends.
func TestDeleteEndsWithItsContext(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	t.Cleanup(k.Hold(oskeyringtest.Delete))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, oskeyring.Delete(ctx, k, "a"), context.Canceled)
}

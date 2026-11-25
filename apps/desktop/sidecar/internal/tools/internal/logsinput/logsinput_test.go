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

package logsinput

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// A refusal of the service reaches the model with its own code and its reason,
// stripped and redacted, bare or inside what the mirror lacked.
func TestAServiceRefusalKeepsItsCodeAndReason(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `{"error":"cursor-invalid","message":"this is not a page cursor a page handed back"}`,
		Refusal(refusal.New(refusal.CodeCursorInvalid, "this is not a page cursor a page handed back")))
	assert.Equal(t, `{"error":"bad-padding","message":"pod web [redacted] gone"}`,
		Refusal(refusal.New(refusal.CodeBadPadding, "pod web \x1b[31msk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\x1b[0m gone")))
	assert.Equal(t, `{"error":"no-nodes","message":"no nodes"}`,
		Refusal(refusal.NoNodes{Refusal: refusal.New(refusal.CodeNoNodes, "no nodes")}))
	assert.Equal(t, `{"error":"syncing","message":"`+logs.ErrSyncing.Error()+`"}`, Refusal(logs.ErrSyncing))
}

// The cluster package's own errors, which Resolve answers reading the cache
// itself, are no-cache and syncing; anything else is read-failed with none of
// its text.
func TestAClusterErrorIsItsCode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `{"error":"no-cache"}`, Refusal(cluster.ErrNoCache))
	assert.Equal(t, `{"error":"syncing"}`, Refusal(cluster.ErrSyncing))
	assert.Equal(t, `{"error":"read-failed"}`, Refusal(errors.New("open /home/user/db: disk I/O error")))
}

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

package logline

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

func TestForModelStripsControlSequences(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "red alert", ForModel("\x1b[31mred\x1b[0m alert"))
}

// Serial: a registered secret is process state.
func TestForModelRedacts(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	safe.AddSecret("sk-live-provider-0123456789")

	assert.Equal(t, "Authorization: "+safe.Redacted, ForModel("Authorization: Bearer abc.def.ghi"))
	assert.Equal(t, "key="+safe.Redacted+" rejected", ForModel("key=sk-live-provider-0123456789 rejected"))
}

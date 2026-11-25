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

package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// On a machine with no sandbox nothing reads or writes the shell's PATH.
func TestAServiceWithNoSandboxChangesNothing(t *testing.T) {
	store := newStore(t)
	s := NewService(store, nil, nil, nil, "")

	assert.Empty(t, s.PathFault())
	_, err := s.SetShellPath(t.Context(), []string{t.TempDir()})
	assert.ErrorIs(t, err, ErrNoSandbox)
	assert.ErrorIs(t, s.RefreshPath(t.Context()), ErrNoSandbox)
	assert.ErrorIs(t, s.SetPath(t.Context(), "$PATH"), ErrNoSandbox)
	_, err = s.RunPath(t.Context())
	assert.ErrorIs(t, err, ErrNoSandbox)
}

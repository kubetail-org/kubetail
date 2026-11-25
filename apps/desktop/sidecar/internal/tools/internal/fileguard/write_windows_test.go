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

//go:build windows

package fileguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The read-only attribute is the one thing replaceable checks.
func TestAReadOnlyFileIsNotReplaceable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o400))
	info, err := os.Stat(file)
	require.NoError(t, err)

	assert.ErrorIs(t, replaceable(file, info, self()), ErrReadOnly)
}

// A NUL in either path is refused before MoveFileEx, which would read the
// path only up to it.
func TestRenameNoReplaceRefusesANUL(t *testing.T) {
	dir := t.TempDir()
	assert.Error(t, renameNoReplace(filepath.Join(dir, "a\x00"), filepath.Join(dir, "b")))
	assert.Error(t, renameNoReplace(filepath.Join(dir, "a"), filepath.Join(dir, "b\x00")))
}

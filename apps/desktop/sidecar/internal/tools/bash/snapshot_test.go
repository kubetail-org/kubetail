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

package bash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The snapshot is only ever a new file: one already there is an error, never
// appended to or replaced.
func TestWriteSnapshotRefusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "snapshot.sh"), []byte("old"), 0o600))
	_, err := writeSnapshot(dir, "new")
	require.ErrorIs(t, err, os.ErrExist)
	b, err := os.ReadFile(filepath.Join(dir, "snapshot.sh"))
	require.NoError(t, err)
	assert.Equal(t, "old", string(b))
}

// The end marker is found in the new bytes alone, including one split across
// two reads.
func TestSnapshotDoneFindsTheMarkerInTheNewBytes(t *testing.T) {
	out := []byte(snapshotStart + "f() { :; }\n" + snapshotEnd)
	split := len(out) - len(snapshotEnd)/2
	assert.False(t, snapshotDone(out[:split], 0))
	assert.True(t, snapshotDone(out, split), "a marker split across two reads")
	assert.True(t, snapshotDone(out, 0))
}

// The dump is what lies between the two markers, and nothing is a dump without
// both.
func TestSnapshotTextIsBetweenTheMarkers(t *testing.T) {
	text, ok := snapshotText([]byte("banner" + snapshotStart + "f() { :; }\n" + snapshotEnd + "tail"))
	assert.True(t, ok)
	assert.Equal(t, "f() { :; }\n", text)

	_, ok = snapshotText([]byte("f() { :; }\n" + snapshotEnd))
	assert.False(t, ok, "no start marker")
	_, ok = snapshotText([]byte(snapshotStart + "f() { :; }\n"))
	assert.False(t, ok, "no end marker")
}

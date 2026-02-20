package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDotColorAssignerPersistsAcrossInstantiations(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "log-dot-colors.v1.json")
	source := logs.LogSource{
		Namespace:     "default",
		PodName:       "web-abc",
		ContainerName: "app",
		ContainerID:   "container-123",
	}

	a1, err := newDotColorAssignerAtPath(statePath)
	require.NoError(t, err)
	dot1 := a1.Dot(source)
	require.NoError(t, a1.Close())

	a2, err := newDotColorAssignerAtPath(statePath)
	require.NoError(t, err)
	dot2 := a2.Dot(source)
	require.NoError(t, a2.Close())

	assert.Equal(t, dot1, dot2)
}

func TestDotColorAssignerSchemaChangeResetsState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "log-dot-colors.v1.json")

	initial := `{"schemaVersion":999,"nextColor":7,"entries":{"container-id:container-a":{"colorIndex":11,"lastSeenAt":"2025-01-01T00:00:00Z"}}}`
	require.NoError(t, os.WriteFile(statePath, []byte(initial), 0o644))

	a, err := newDotColorAssignerAtPath(statePath)
	require.NoError(t, err)
	_ = a.Dot(logs.LogSource{ContainerID: "container-b"})
	require.NoError(t, a.Close())

	state, err := a.readStateFile()
	require.NoError(t, err)
	assert.Equal(t, dotColorStateSchemaVersion, state.SchemaVersion)
	_, hasOld := state.Entries["container-id:container-a"]
	assert.False(t, hasOld)
	_, hasNew := state.Entries["container-id:container-b"]
	assert.True(t, hasNew)
}

func TestDotColorAssignerCleansUpStaleEntries(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "log-dot-colors.v1.json")

	stale := time.Now().UTC().Add(-dotColorStateTTL - time.Hour)
	state := &dotColorStateFile{
		SchemaVersion: dotColorStateSchemaVersion,
		NextColor:     1,
		Entries: map[string]dotColorStateEntry{
			"container-id:stale": {
				ColorIndex: 0,
				LastSeenAt: stale,
			},
		},
	}
	require.NoError(t, writeJSONAtomic(statePath, state))

	a, err := newDotColorAssignerAtPath(statePath)
	require.NoError(t, err)
	now := time.Now().UTC()
	a.now = func() time.Time { return now }
	_ = a.Dot(logs.LogSource{ContainerID: "fresh"})
	require.NoError(t, a.Close())

	updated, err := a.readStateFile()
	require.NoError(t, err)
	_, staleExists := updated.Entries["container-id:stale"]
	assert.False(t, staleExists)
	_, freshExists := updated.Entries["container-id:fresh"]
	assert.True(t, freshExists)
}

func TestDotColorAssignerPersistsDisplayedColorForMultipleNewSources(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "log-dot-colors.v1.json")

	a, err := newDotColorAssignerAtPath(statePath)
	require.NoError(t, err)

	sourceA := logs.LogSource{ContainerID: "container-a"}
	sourceB := logs.LogSource{ContainerID: "container-b"}

	dotA := a.Dot(sourceA)
	dotB := a.Dot(sourceB)
	require.NoError(t, a.Close())

	state, err := a.readStateFile()
	require.NoError(t, err)

	entryA, ok := state.Entries["container-id:container-a"]
	require.True(t, ok)
	entryB, ok := state.Entries["container-id:container-b"]
	require.True(t, ok)

	assert.Equal(t, dotA, ansiDot(entryA.ColorIndex))
	assert.Equal(t, dotB, ansiDot(entryB.ColorIndex))
}

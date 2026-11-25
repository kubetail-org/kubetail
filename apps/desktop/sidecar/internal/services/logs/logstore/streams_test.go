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

package logstore

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// app is the instance containerd://1 of web/api-7f9c's app container, on node-a.
func app() Stream {
	return Stream{
		Namespace: "web", Pod: "api-7f9c", Container: "app", ContainerID: "containerd://1",
		StartedAt: second(1), Node: "node-a", OS: "linux", Arch: "amd64",
		Labels: `{"app":"api"}`, PodUID: "uid-api", Default: true,
	}
}

// An instance is one row however often it is filed, and filing it again updates
// its place, its labels and when it finished, never what names it.
func TestPutStreamsInsertsOnceAndUpdatesTheSnapshot(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	sidecar := app()
	sidecar.Container, sidecar.ContainerID, sidecar.Default = "istio-proxy", "containerd://2", false

	ids, err := s.PutStreams(t.Context(), []Stream{app(), sidecar})
	require.NoError(t, err)
	require.Len(t, ids, 2)
	assert.NotEqual(t, ids[0], ids[1])

	moved := app()
	moved.Node, moved.Zone, moved.Labels, moved.FinishedAt = "node-b", "eu-1b", `{"app":"api","v":"2"}`, second(9)
	again, err := s.PutStreams(t.Context(), []Stream{moved})
	require.NoError(t, err)
	assert.Equal(t, ids[:1], again)

	rows, err := s.PodStreams(t.Context(), "web", "api-7f9c", "")
	require.NoError(t, err)
	moved.ID = ids[0]
	sidecar.ID = ids[1]
	assert.ElementsMatch(t, []Stream{moved, sidecar}, rows)
}

// second is s seconds past 10:00 on 2026-10-08, in unix nanoseconds.
func second(s int64) int64 {
	return 1_791_453_600_000_000_000 + s*1_000_000_000
}

// A pod's streams are its UID's, and a pod the mirror no longer holds is asked for
// by name, any UID: the instances of a gone pod and of the one before it.
func TestPodStreamsFindsAPodByUIDOrAGonePodByName(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	later := app()
	later.ContainerID, later.PodUID, later.StartedAt = "containerd://9", "uid-api-2", second(5)
	other := app()
	other.Pod, other.ContainerID = "api-8c1d", "containerd://3"
	ids, err := s.PutStreams(t.Context(), []Stream{app(), later, other})
	require.NoError(t, err)

	idsOf := func(rows []Stream) []int64 {
		var out []int64
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	rows, err := s.PodStreams(t.Context(), "web", "api-7f9c", "uid-api-2")
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[1]}, idsOf(rows))
	rows, err = s.PodStreams(t.Context(), "web", "api-7f9c", "")
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[1], ids[0]}, idsOf(rows), "newest first")
}

// ownersIn is each pair of pod_owners in the file behind s, "pod owner", in order.
func ownersIn(t *testing.T, s *Store) []string {
	t.Helper()
	var out []string
	using(t, s, func(f *file) {
		rows, err := f.readDB.Query(`SELECT pod_uid || ' ' || owner_uid FROM pod_owners ORDER BY pod_uid, owner_uid`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var pair string
			require.NoError(t, rows.Scan(&pair))
			out = append(out, pair)
		}
		require.NoError(t, rows.Err())
	})
	return out
}

// A pod's controller chain is filed with its streams, once for the pod however many
// of its containers are filed.
func TestAPodsChainIsFiledWithItsStreams(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	api := app()
	api.Owners = []string{"uid-rs", "uid-dep"}
	sidecar := api
	sidecar.Container, sidecar.ContainerID, sidecar.Default = "istio-proxy", "containerd://2", false

	_, err := s.PutStreams(t.Context(), []Stream{api, sidecar})

	require.NoError(t, err)
	assert.Equal(t, []string{"uid-api uid-dep", "uid-api uid-rs"}, ownersIn(t, s))
}

// The mirror can show a pod before its ReplicaSet, so a filing of a stream the file
// holds as it is still adds the owners the file lacks.
func TestALaterFilingAddsTheOwnersTheFileLacks(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	api := app()
	api.Owners = []string{"uid-rs"}
	_, err := s.PutStreams(t.Context(), []Stream{api})
	require.NoError(t, err)

	api.Owners = []string{"uid-rs", "uid-dep"}
	_, err = s.PutStreams(t.Context(), []Stream{api})

	require.NoError(t, err)
	assert.Equal(t, []string{"uid-api uid-dep", "uid-api uid-rs"}, ownersIn(t, s))
}

// The streams an owner's pods hold are found by the owner, each with its oldest and
// newest line: not another owner's, nor a stream with no line.
func TestOwnedStreamsFindsAnOwnersPodsByTheirLines(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	pod := func(name, container, id string, owners ...string) Stream {
		row := app()
		row.Pod, row.PodUID, row.Container, row.ContainerID, row.Owners = name, "uid-"+name, container, id, owners
		return row
	}
	gone := pod("gone", "app", "containerd://1", "uid-rs", "uid-dep")
	sidecar := pod("gone", "istio-proxy", "containerd://2", "uid-rs", "uid-dep")
	sidecar.Default = false
	empty := pod("empty", "app", "containerd://3", "uid-rs", "uid-dep")
	running := pod("running", "app", "containerd://4", "uid-rs", "uid-dep")
	other := pod("other", "app", "containerd://5", "uid-other")
	ids, err := s.PutStreams(t.Context(), []Stream{gone, sidecar, empty, running, other})
	require.NoError(t, err)
	write(t, s,
		Fill{Stream: ids[0], Backend: "kubelet", Lines: lines(at(2, 0), at(4, 1), at(6, 0))},
		Fill{Stream: ids[1], Backend: "kubelet", Lines: lines(at(3, 0))},
		Fill{Stream: ids[3], Backend: "kubelet", Lines: lines(at(5, 0))},
		Fill{Stream: ids[4], Backend: "kubelet", Lines: lines(at(5, 0))})

	owned, err := s.OwnedStreams(t.Context(), "uid-dep")

	require.NoError(t, err)
	gone.ID, sidecar.ID, running.ID = ids[0], ids[1], ids[3]
	gone.Owners, sidecar.Owners, running.Owners = nil, nil, nil
	assert.Equal(t, []Owned{
		{Stream: gone, Oldest: at(2, 0), Newest: at(6, 0)},
		{Stream: sidecar, Oldest: at(3, 0), Newest: at(3, 0)},
		{Stream: running, Oldest: at(5, 0), Newest: at(5, 0)},
	}, owned)
}

// A pod has one default container: filing another as the default, after its
// annotation moved, moves the mark onto every instance of it and off every instance
// of the one before, and a row filed as not the default loses it.
func TestADefaultMovesWithThePod(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	earlier := app()
	earlier.ContainerID = "containerd://0"
	sidecar := app()
	sidecar.Container, sidecar.ContainerID, sidecar.Default = "istio-proxy", "containerd://2", false
	olderSidecar := sidecar
	olderSidecar.ContainerID = "containerd://3"
	_, err := s.PutStreams(t.Context(), []Stream{earlier, app(), sidecar, olderSidecar})
	require.NoError(t, err)

	sidecar.Default = true
	_, err = s.PutStreams(t.Context(), []Stream{sidecar})
	require.NoError(t, err)

	defaults := func() map[string]bool {
		rows, err := s.PodStreams(t.Context(), "web", "api-7f9c", "")
		require.NoError(t, err)
		out := map[string]bool{}
		for _, r := range rows {
			out[r.ContainerID] = r.Default
		}
		return out
	}
	assert.Equal(t, map[string]bool{"containerd://0": false, "containerd://1": false, "containerd://2": true, "containerd://3": true}, defaults())

	sidecar.Default = false
	_, err = s.PutStreams(t.Context(), []Stream{sidecar})
	require.NoError(t, err)
	assert.False(t, defaults()["containerd://2"])
}

// A default filed again as it is still moves the mark when an instance of its pod
// is marked otherwise: the filing is not skipped as unchanged.
func TestARefiledDefaultSettlesItsPod(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	earlier := app()
	earlier.ContainerID, earlier.Default = "containerd://0", false
	_, err := s.PutStreams(t.Context(), []Stream{app()})
	require.NoError(t, err)
	_, err = s.PutStreams(t.Context(), []Stream{earlier})
	require.NoError(t, err)

	_, err = s.PutStreams(t.Context(), []Stream{app()})
	require.NoError(t, err)

	rows, err := s.PodStreams(t.Context(), "web", "api-7f9c", "")
	require.NoError(t, err)
	for _, r := range rows {
		assert.True(t, r.Default, r.ContainerID)
	}
}

// Filing rows the file already holds as they are, their pod's owners included, or
// finishing rows it holds finished, takes no write lock: they pass while another connection holds it.
func TestUnchangedStreamsTakeNoWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManager(dir, testRetention)
	t.Cleanup(func() { assert.NoError(t, m.Close()) })
	s, err := m.OpenOrCreate(1)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	sidecar := app()
	sidecar.Container, sidecar.ContainerID, sidecar.Default = "istio-proxy", "containerd://2", false
	finished := app()
	finished.ContainerID, finished.FinishedAt = "containerd://0", second(0)
	finished.Owners = []string{"uid-rs", "uid-dep"}
	ids, err := s.PutStreams(t.Context(), []Stream{finished, app(), sidecar})
	require.NoError(t, err)

	locker, err := sql.Open("sqlite", filepath.Join(dir, "1.db"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, locker.Close()) })
	lock, err := locker.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, lock.Close()) })
	_, err = lock.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)

	again, err := s.PutStreams(t.Context(), []Stream{finished, app(), sidecar})
	require.NoError(t, err)
	assert.Equal(t, ids, again)
	require.NoError(t, s.FinishStreams(t.Context(), []Stream{finished}))
	_, err = lock.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
}

// A filing whose write the file refuses fails whole: the insert, the default's move
// off another container, an owner of its pod, and the trim of a stream it finishes.
func TestAFilingTheFileRefusesFails(t *testing.T) {
	t.Parallel()
	sidecar := app()
	sidecar.Container, sidecar.ContainerID = "istio-proxy", "containerd://2"
	done := app()
	done.FinishedAt = second(5)
	owned := app()
	owned.Owners = []string{"uid-rs"}
	for name, c := range map[string]struct {
		op, table string
		put       Stream
	}{
		"the insert":         {"INSERT", "streams", app()},
		"the default's move": {"UPDATE", "streams", app()},
		"an owner":           {"INSERT", "pod_owners", owned},
		"the trim":           {"DELETE", "lines", done},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			switch c.table {
			case "lines":
				id := streamOf(t, s)
				write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(6, 0))})
			case "streams":
				_, err := s.PutStreams(t.Context(), []Stream{sidecar})
				require.NoError(t, err)
			}
			refuse(t, s, c.op, c.table)

			_, err := s.PutStreams(t.Context(), []Stream{c.put})
			assert.ErrorContains(t, err, "put streams: ")
			assert.ErrorContains(t, err, "refused")
		})
	}
}

// Finishing a stream fails when the file refuses to set its finished_at or to trim it.
func TestAFinishTheFileRefusesFails(t *testing.T) {
	t.Parallel()
	done := app()
	done.FinishedAt = second(5)
	for name, c := range map[string]struct{ op, table string }{
		"the finish": {"UPDATE", "streams"},
		"the trim":   {"DELETE", "lines"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			id := streamOf(t, s)
			write(t, s, Fill{Stream: id, Backend: "kubelet", Lines: lines(at(6, 0))})
			refuse(t, s, c.op, c.table)

			err := s.FinishStreams(t.Context(), []Stream{done})
			assert.ErrorContains(t, err, "finish streams: ")
			assert.ErrorContains(t, err, "refused")
		})
	}
}

// Finishing a stream fails when the file cannot read its streams.
func TestAFinishThatCannotReadTheFileFails(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	alter(t, s, `DROP TABLE streams`)

	err := s.FinishStreams(t.Context(), []Stream{app()})

	assert.ErrorContains(t, err, "finish streams: ")
	assert.ErrorContains(t, err, "no such table")
}

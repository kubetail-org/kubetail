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

package memory

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// The last save under a name wins: there is no version to name.
func TestASaveReplacesByName(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"c1", "c2"} {
		exec(t, h.db, `INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES (?, ?, 'chat', 0, 0)`, id, string(clusterA))
	}
	require.NoError(t, h.svc.Save(t.Context(), clusterA, "pages", "first", "c1"))
	first := h.get(t, clusterA, "pages")

	h.svc.now = func() time.Time { return first.UpdatedAt.Add(time.Hour) }
	require.NoError(t, h.svc.Save(t.Context(), clusterA, "pages", "rewritten", "c2"))

	assert.Equal(t, []string{"pages"}, h.names(t, clusterA))
	m := h.get(t, clusterA, "pages")
	assert.Equal(t, first.ID, m.ID)
	assert.Equal(t, "rewritten", m.Body)
	assert.True(t, m.UpdatedAt.After(first.UpdatedAt))
	assert.Equal(t, AuthorModel, m.WrittenBy)
	assert.Equal(t, apimeta.ChatID("c2"), *m.ChatID)
	assert.Equal(t, clusterA, *m.ClusterID)
}

// Cluster first, then by name, so the section is stable while nothing changes.
func TestVisibleIsTheClustersThenTheGlobalOnesByName(t *testing.T) {
	h := newHarness(t)

	h.global(t, "a-user")
	h.save(t, clusterA, "z-note")
	h.save(t, clusterA, "b-note")
	h.save(t, clusterB, "other")

	assert.Equal(t, []string{"b-note", "z-note", "a-user"}, h.names(t, clusterA))
	assert.Equal(t, []string{"other", "a-user"}, h.names(t, clusterB))
	assert.Nil(t, h.get(t, clusterA, "a-user").ClusterID)
}

func TestAMarkedClusterTakesNoMemories(t *testing.T) {
	h := newHarness(t)
	exec(t, h.db, `UPDATE clusters SET delete_requested_at = 1 WHERE id = ?`, string(clusterA))

	require.ErrorIs(t, h.svc.Save(t.Context(), clusterA, "n", "b", ""), ErrClusterGone)
	require.ErrorIs(t, h.svc.Save(t.Context(), "gone", "n", "b", ""), ErrClusterGone)
	require.ErrorIs(t, h.svc.Forget(t.Context(), clusterA, "n"), ErrClusterGone)
	assert.Empty(t, h.names(t, clusterB))
}

func TestAClustersMemoriesGoWithItsRow(t *testing.T) {
	h := newHarness(t)
	h.save(t, clusterA, "n")
	h.global(t, "g")

	exec(t, h.db, `DELETE FROM clusters WHERE id = ?`, string(clusterA))

	assert.Equal(t, []string{"g"}, h.names(t, clusterB))
}

func TestADeletedChatLeavesItsMemories(t *testing.T) {
	h := newHarness(t)
	exec(t, h.db, `INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', ?, 'chat', 0, 0)`, string(clusterA))
	require.NoError(t, h.svc.Save(t.Context(), clusterA, "n", "b", "c"))

	exec(t, h.db, `DELETE FROM chats WHERE id = 'c'`)

	assert.Nil(t, h.get(t, clusterA, "n").ChatID)
}

// A note the user wrote is a standing request, on one cluster or every cluster,
// so the model can neither save over one nor forget one, and changes nothing
// trying.
func TestTheModelChangesOnlyItsOwnNotes(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.global(t, "prefs")
	_, err := h.svc.Create(ctx, Input{ClusterID: new(clusterA), Name: "runbook", Body: "the body of runbook"})
	require.NoError(t, err)

	require.ErrorIs(t, h.svc.Save(ctx, clusterA, "runbook", "narrowed", ""), ErrUserNote)
	require.ErrorIs(t, h.svc.Forget(ctx, clusterA, "runbook"), ErrUserNote)
	require.ErrorIs(t, h.svc.SaveEverywhere(ctx, "prefs", "narrowed", ""), ErrUserNote)
	require.ErrorIs(t, h.svc.ForgetEverywhere(ctx, "prefs"), ErrUserNote)
	for _, name := range []string{"prefs", "runbook"} {
		m := h.get(t, clusterA, name)
		assert.Equal(t, "the body of "+name, m.Body)
		assert.Equal(t, AuthorUser, m.WrittenBy)
	}
}

// Each of the model's calls reaches only the scope it names. A call for the
// cluster never reaches a note for every cluster, whoever wrote it, so a call
// nobody was asked about cannot change what every cluster reads.
func TestACallForTheClusterNeverReachesANoteForEveryCluster(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.global(t, "prefs")
	require.NoError(t, h.svc.SaveEverywhere(ctx, "pages", "for every cluster", ""))

	for _, name := range []string{"prefs", "pages"} {
		require.ErrorIs(t, h.svc.Forget(ctx, clusterA, name), ErrNotFound, name)
		require.NoError(t, h.svc.Save(ctx, clusterA, name, "the cluster's own", ""), name)
		assert.Equal(t, "the cluster's own", h.get(t, clusterA, name).Body, "the cluster's own comes first")
		require.NoError(t, h.svc.Forget(ctx, clusterA, name), name)
		assert.Nil(t, h.get(t, clusterA, name).ClusterID, name)
	}
	assert.Equal(t, "for every cluster", h.get(t, clusterB, "pages").Body)
}

// A save for every cluster is the model's own note, like a save for the
// cluster: no cluster, no server UID, and the chat that asked.
func TestASaveForEveryClusterIsTheModels(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	exec(t, h.db, `INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('c', ?, 'chat', 0, 0)`, string(clusterA))

	require.NoError(t, h.svc.SaveEverywhere(ctx, "prefs", "the body of prefs", "c"))

	m := h.get(t, clusterB, "prefs")
	assert.Nil(t, m.ClusterID)
	assert.Nil(t, m.ServerUID)
	assert.Equal(t, AuthorModel, m.WrittenBy)
	assert.Equal(t, apimeta.ChatID("c"), *m.ChatID)
	assert.Contains(t, string(h.svc.Section(ctx, clusterB)), `"name":"prefs","scope":"everywhere","by":"model"`)
}

// A save for every cluster replaces the model's own note of that name there, and
// never the user's.
func TestASaveForEveryClusterReplacesOnlyTheModelsNote(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.global(t, "prefs")

	require.ErrorIs(t, h.svc.SaveEverywhere(ctx, "prefs", "replaced", ""), ErrUserNote)
	assert.Equal(t, "the body of prefs", h.get(t, clusterA, "prefs").Body)

	require.NoError(t, h.svc.SaveEverywhere(ctx, "pages", "first", ""))
	first := h.get(t, clusterA, "pages")
	require.NoError(t, h.svc.SaveEverywhere(ctx, "pages", "rewritten", ""))
	m := h.get(t, clusterA, "pages")
	assert.Equal(t, first.ID, m.ID)
	assert.Equal(t, "rewritten", m.Body)
	assert.Nil(t, m.ClusterID)
}

// A forget for every cluster removes the model's own note there, never the
// user's, and never a cluster's note of the same name.
func TestAForgetForEveryClusterRemovesOnlyTheModelsNote(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.global(t, "prefs")
	h.save(t, clusterA, "pages")
	require.NoError(t, h.svc.SaveEverywhere(ctx, "pages", "for every cluster", ""))

	require.ErrorIs(t, h.svc.ForgetEverywhere(ctx, "nothing"), ErrNotFound)
	require.NoError(t, h.svc.ForgetEverywhere(ctx, "pages"))
	require.ErrorIs(t, h.svc.ForgetEverywhere(ctx, "pages"), ErrNotFound)
	assert.Equal(t, []string{"pages", "prefs"}, h.names(t, clusterA))
	assert.Equal(t, clusterA, *h.get(t, clusterA, "pages").ClusterID)
}

// A cluster's note and a note for every cluster may share a name; within one
// scope a name is unique, whoever moves a note into it.
func TestANameIsUniqueInItsScope(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.save(t, clusterA, "pages")
	h.save(t, clusterB, "pages")
	h.global(t, "prefs")

	_, err := h.svc.Create(ctx, input(nil, "pages"))
	require.NoError(t, err, "a global name a cluster holds")
	_, err = h.svc.Create(ctx, input(new(clusterA), "prefs"))
	require.NoError(t, err, "a cluster name a global holds")
	assert.Equal(t, []string{"pages", "prefs", "pages", "prefs"}, h.names(t, clusterA))

	_, err = h.svc.Create(ctx, input(nil, "prefs"))
	require.ErrorIs(t, err, ErrNameTaken, "a second global under one name")
	_, err = h.svc.Update(ctx, h.get(t, clusterB, "pages").ID, input(nil, "pages"))
	require.ErrorIs(t, err, ErrNameTaken, "a move into a scope that holds the name")
	mine, err := h.svc.Create(ctx, input(new(clusterA), "mine"))
	require.NoError(t, err)
	_, err = h.svc.Update(ctx, mine.ID, input(new(clusterA), "pages"))
	require.ErrorIs(t, err, ErrNameTaken, "a rename onto a name the scope holds")
}

func TestForgetRemovesTheClustersOwnNote(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.save(t, clusterA, "mine")
	h.save(t, clusterB, "mine")

	require.ErrorIs(t, h.svc.Forget(ctx, clusterA, "nothing"), ErrNotFound)
	require.NoError(t, h.svc.Forget(ctx, clusterA, "mine"))
	assert.Empty(t, h.names(t, clusterA))
	assert.Equal(t, []string{"mine"}, h.names(t, clusterB))
}

// A forget for one cluster reaches its own notes alone: another cluster's note and a
// note for every cluster are not found, and both stay.
func TestAForgetReachesOnlyItsOwnScope(t *testing.T) {
	h := newHarness(t)
	h.save(t, clusterB, "theirs")
	h.global(t, "shared")

	require.ErrorIs(t, h.svc.Forget(t.Context(), clusterA, "theirs"), ErrNotFound)
	require.ErrorIs(t, h.svc.Forget(t.Context(), clusterA, "shared"), ErrNotFound)
	assert.Equal(t, []string{"theirs", "shared"}, h.names(t, clusterB))
}

func input(cluster *apimeta.ClusterID, name string) Input {
	return Input{ClusterID: cluster, Name: name, Body: "body"}
}

func TestTheUserCreatesUpdatesAndDeletesByID(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	m, err := h.svc.Create(ctx, input(new(clusterA), "mine"))
	require.NoError(t, err)
	assert.Equal(t, AuthorUser, m.WrittenBy)
	assert.Equal(t, m, h.get(t, clusterA, "mine"))

	m, err = h.svc.Update(ctx, m.ID, input(nil, "renamed"))
	require.NoError(t, err, "a rename and a move to every cluster")
	assert.Nil(t, m.ClusterID)
	assert.Equal(t, []string{"renamed"}, h.names(t, clusterB))

	m, err = h.svc.Update(ctx, m.ID, input(new(clusterB), "renamed"))
	require.NoError(t, err, "a move back to one cluster")
	assert.Equal(t, clusterB, *m.ClusterID)
	assert.Empty(t, h.names(t, clusterA))

	_, err = h.svc.Update(ctx, "missing", input(new(clusterB), "renamed"))
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, h.svc.Delete(ctx, m.ID))
	require.ErrorIs(t, h.svc.Delete(ctx, m.ID), ErrNotFound)
	assert.Empty(t, h.names(t, clusterB))
}

// written_by changes one way: every dialog write makes a note the user's, and
// no model write makes one the user's.
func TestOnlyTheUserWritesARequest(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	for _, name := range []string{"edited", "renamed", "moved"} {
		h.save(t, clusterA, name)
		assert.Equal(t, AuthorModel, h.get(t, clusterA, name).WrittenBy)
	}

	for name, in := range map[string]Input{
		"edited":  {ClusterID: new(clusterA), Name: "edited", Body: "tidied"},
		"renamed": {ClusterID: new(clusterA), Name: "renamed-again", Body: "the body of renamed"},
		"moved":   {Name: "moved", Body: "the body of moved"},
	} {
		m, err := h.svc.Update(ctx, h.get(t, clusterA, name).ID, in)
		require.NoError(t, err, name)
		assert.Equal(t, AuthorUser, m.WrittenBy, name)
	}

	h.save(t, clusterA, "fresh")
	require.NoError(t, h.svc.Save(ctx, clusterA, "fresh", "rewritten", ""))
	assert.Equal(t, AuthorModel, h.get(t, clusterA, "fresh").WrittenBy)
}

func TestTheUsersWritesFollowTheStoresRules(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Create(ctx, input(nil, "Bad Name"))
	require.ErrorIs(t, err, ErrBadInput)
	secret := input(new(clusterA), "secret")
	secret.Body = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	_, err = h.svc.Create(ctx, secret)
	require.ErrorIs(t, err, ErrSecret)

	exec(t, h.db, `UPDATE clusters SET delete_requested_at = 1 WHERE id = ?`, string(clusterB))
	_, err = h.svc.Create(ctx, input(new(clusterB), "late"))
	require.ErrorIs(t, err, ErrClusterGone)
}

func TestAWriteStampsTheServerUID(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	h.save(t, clusterA, "unknown")
	assert.Nil(t, h.get(t, clusterA, "unknown").ServerUID, "a cluster with no UID")

	h.uids.set(clusterA, "uid-1")
	h.save(t, clusterA, "saved")
	assert.Equal(t, "uid-1", *h.get(t, clusterA, "saved").ServerUID)
	created, err := h.svc.Create(ctx, input(new(clusterA), "created"))
	require.NoError(t, err)
	assert.Equal(t, "uid-1", *created.ServerUID)

	h.uids.set(clusterA, "uid-2")
	updated, err := h.svc.Update(ctx, created.ID, input(new(clusterA), "created"))
	require.NoError(t, err)
	assert.Equal(t, "uid-2", *updated.ServerUID, "a rewrite after a rebuild stamps the new one")

	moved, err := h.svc.Update(ctx, h.get(t, clusterA, "saved").ID, input(nil, "saved"))
	require.NoError(t, err)
	assert.Nil(t, moved.ServerUID, "a move to every cluster clears it")
}

// ownSize is what cluster's own notes take in the section, as it encodes them.
func (h *harness) ownSize(t *testing.T, cluster apimeta.ClusterID) int {
	t.Helper()
	ms, err := h.svc.Visible(t.Context(), cluster)
	require.NoError(t, err)
	var own []Memory
	for _, m := range ms {
		if m.ClusterID != nil {
			own = append(own, m)
		}
	}
	return sectionSize(entries(own))
}

// fill saves notes of body into cluster until the next is refused ErrFull.
func (h *harness) fill(t *testing.T, cluster apimeta.ClusterID, body string) {
	t.Helper()
	for i := 0; ; i++ {
		err := h.svc.Save(t.Context(), cluster, fmt.Sprintf("n%02d", i), body, "")
		if errors.Is(err, ErrFull) {
			require.Positive(t, i, "the first note fits")
			return
		}
		require.NoError(t, err)
	}
}

// A scope holds what fits in ScopeBudget as the section encodes it, so the
// section never has to cut a note.
func TestEachScopeIsBoundedBySize(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	h.fill(t, clusterA, strings.Repeat("b", 400))
	assert.LessOrEqual(t, h.ownSize(t, clusterA), ScopeBudget)
	require.NoError(t, h.svc.Save(ctx, clusterA, "n00", "shrunk", ""), "a save that shrinks a note in a full scope")

	// Each < encodes as six bytes, so these fill the scope long before their raw
	// bytes would.
	require.NoError(t, h.svc.Save(ctx, clusterB, "escaped", strings.Repeat("<", 400), ""))
	require.ErrorIs(t, h.svc.Save(ctx, clusterB, "escaped-too", strings.Repeat("<", 400), ""), ErrFull)

	// Fill every cluster's scope with notes just smaller than the one that moves.
	for i := 0; ; i++ {
		_, err := h.svc.Create(ctx, input(nil, fmt.Sprintf("g%02d", i)))
		if errors.Is(err, ErrFull) {
			break
		}
		require.NoError(t, err)
	}
	h.save(t, clusterB, "mover")
	_, err := h.svc.Update(ctx, h.get(t, clusterB, "mover").ID, input(nil, "mover"))
	require.ErrorIs(t, err, ErrFull, "a move is measured against the scope it enters")
}

func TestSaveChecksTheShape(t *testing.T) {
	h := newHarness(t)
	h.save(t, clusterA, "kept")

	for name, n := range map[string]struct{ name, body string }{
		"a bad name":        {"Bad Name", "b"},
		"an empty body":     {"empty", ""},
		"an oversized body": {"big", strings.Repeat("b", BodyMax+1)},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, h.svc.Save(t.Context(), clusterA, n.name, n.body, ""), ErrBadInput)
		})
	}
	assert.Equal(t, []string{"kept"}, h.names(t, clusterA))
}

func TestASaveForEveryClusterChecksTheShape(t *testing.T) {
	h := newHarness(t)

	for name, n := range map[string]struct {
		name, body string
		want       error
	}{
		"a bad name":        {"Bad Name", "b", ErrBadInput},
		"an empty body":     {"empty", "", ErrBadInput},
		"an oversized body": {"big", strings.Repeat("b", BodyMax+1), ErrBadInput},
		"a credential":      {"secret", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln", ErrSecret},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, h.svc.SaveEverywhere(t.Context(), n.name, n.body, ""), n.want)
		})
	}
	assert.Empty(t, h.names(t, clusterA))
}

func TestASaveForEveryClusterIsBoundedBySize(t *testing.T) {
	h := newHarness(t)

	for i := 0; ; i++ {
		err := h.svc.SaveEverywhere(t.Context(), fmt.Sprintf("g%02d", i), strings.Repeat("b", 400), "")
		if errors.Is(err, ErrFull) {
			require.Positive(t, i, "the first note fits")
			assert.Len(t, h.names(t, clusterA), i, "the refused one is not written")
			return
		}
		require.NoError(t, err)
	}
}

func TestASaveRefusesACredential(t *testing.T) {
	h := newHarness(t)

	for name, body := range map[string]string{
		"a PEM block":    "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----",
		"a bearer token": "call it with bearer abc.def.ghi",
		"a JWT":          "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln",
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, h.svc.Save(t.Context(), clusterA, "secret", body, ""), ErrSecret)
		})
	}
	assert.Empty(t, h.names(t, clusterA))

	require.NoError(t, h.svc.Save(t.Context(), clusterA, "dashboard", "https://grafana.internal/d/abc?orgId=1&var-ns=payments", ""))
	require.NoError(t, h.svc.Save(t.Context(), clusterA, "rotation", "password rotation is monthly", ""))
}

// A check answers what the write would, and changes nothing.
func TestACheckForEveryClusterAnswersAsTheWrite(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	h.global(t, "prefs")
	require.NoError(t, h.svc.SaveEverywhere(ctx, "pages", "first", ""))

	require.NoError(t, h.svc.CheckSaveEverywhere(ctx, "pages", "rewritten"))
	require.NoError(t, h.svc.CheckSaveEverywhere(ctx, "new", "b"))
	require.ErrorIs(t, h.svc.CheckSaveEverywhere(ctx, "prefs", "b"), ErrUserNote)
	require.ErrorIs(t, h.svc.CheckSaveEverywhere(ctx, "Bad Name", "b"), ErrBadInput)
	require.ErrorIs(t, h.svc.CheckSaveEverywhere(ctx, "jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"), ErrSecret)
	require.NoError(t, h.svc.CheckForgetEverywhere(ctx, "pages"))
	require.ErrorIs(t, h.svc.CheckForgetEverywhere(ctx, "prefs"), ErrUserNote)
	require.ErrorIs(t, h.svc.CheckForgetEverywhere(ctx, "nothing"), ErrNotFound)
	require.ErrorIs(t, h.svc.CheckForgetEverywhere(ctx, "Bad Name"), ErrBadInput)

	assert.Equal(t, []string{"pages", "prefs"}, h.names(t, clusterA))
	assert.Equal(t, "first", h.get(t, clusterA, "pages").Body)
}

func TestACheckForEveryClusterIsBoundedBySize(t *testing.T) {
	h := newHarness(t)
	body := strings.Repeat("b", 400)

	for i := 0; ; i++ {
		name := fmt.Sprintf("g%02d", i)
		err := h.svc.CheckSaveEverywhere(t.Context(), name, body)
		if errors.Is(err, ErrFull) {
			require.Positive(t, i, "the first note fits")
			require.ErrorIs(t, h.svc.SaveEverywhere(t.Context(), name, body, ""), ErrFull)
			return
		}
		require.NoError(t, err)
		require.NoError(t, h.svc.SaveEverywhere(t.Context(), name, body, ""))
	}
}

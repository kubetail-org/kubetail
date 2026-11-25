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

package envvars

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

const (
	every = apimeta.ClusterID("")
	prod  = apimeta.ClusterID("prod")
	dev   = apimeta.ClusterID("dev")
)

// newService is a service over an app.db of the test's own holding the prod
// and dev clusters, on a clock that steps a second at each read, so every row
// has its own created_at.
func newService(t *testing.T) (*Service, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	for _, id := range []apimeta.ClusterID{prod, dev} {
		_, err := db.Write.Exec(`INSERT INTO clusters (id, source, source_key, created_at, updated_at)
			VALUES (?, 'kubeconfig', ?, 0, 0)`, id, id)
		require.NoError(t, err)
	}
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s, err := New(db, func() time.Time {
		at = at.Add(time.Second)
		return at
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s, db
}

// set sets NAME=value for both sides of the sandbox.
func set(t *testing.T, s *Service, cluster apimeta.ClusterID, name, value string) {
	t.Helper()
	require.NoError(t, s.Set(t.Context(), Var{ClusterID: cluster, Name: name, Value: value, Target: Both}))
}

func list(t *testing.T, s *Service, cluster apimeta.ClusterID) []Var {
	t.Helper()
	vars, err := s.List(t.Context(), cluster)
	require.NoError(t, err)
	return vars
}

func envFor(t *testing.T, s *Service, cluster apimeta.ClusterID, sandboxed bool) []string {
	t.Helper()
	env, err := s.For(t.Context(), cluster, sandboxed)
	require.NoError(t, err)
	return env
}

func TestSetAddsANewNameAtItsScopesEnd(t *testing.T) {
	s, _ := newService(t)
	assert.Empty(t, list(t, s, every))
	assert.NotNil(t, list(t, s, every), "no variables is an empty list")

	set(t, s, every, "HELM_NAMESPACE", "apps")
	set(t, s, prod, "HELM_NAMESPACE", "prod-apps")
	set(t, s, every, "EDITOR", "vi")

	assert.Equal(t, []Var{
		{Name: "HELM_NAMESPACE", Value: "apps", Target: Both},
		{Name: "EDITOR", Value: "vi", Target: Both},
	}, list(t, s, every))
	assert.Equal(t, []Var{{ClusterID: prod, Name: "HELM_NAMESPACE", Value: "prod-apps", Target: Both}}, list(t, s, prod))
	assert.Empty(t, list(t, s, dev))
}

func TestSetOfAListedNameChangesItInPlace(t *testing.T) {
	s, _ := newService(t)
	set(t, s, every, "EDITOR", "vi")
	set(t, s, every, "PAGER", "less")
	require.NoError(t, s.Set(t.Context(), Var{Name: "EDITOR", Value: "nvim", Target: Outside}))

	assert.Equal(t, []Var{
		{Name: "EDITOR", Value: "nvim", Target: Outside},
		{Name: "PAGER", Value: "less", Target: Both},
	}, list(t, s, every))
}

func TestSetRefusesWhatTheCheckRefusesAndWritesNothing(t *testing.T) {
	s, _ := newService(t)
	var refusal Refusal
	require.ErrorAs(t, s.Set(t.Context(), Var{Name: "AWS_PROFILE", Value: "dev", Target: Both}), &refusal)
	assert.Empty(t, list(t, s, every))

	require.NoError(t, s.Set(t.Context(), Var{Name: "AWS_PROFILE", Value: "dev", Target: Outside}))
	require.ErrorAs(t, s.Set(t.Context(), Var{Name: "AWS_PROFILE", Value: "dev", Target: Sandbox}), &refusal)
	assert.Equal(t, Outside, list(t, s, every)[0].Target, "a refused change leaves the row")
}

// The bound is each scope's own, and a listed name still changes at it.
func TestAFullScopeTakesNoNewVariable(t *testing.T) {
	s, _ := newService(t)
	for i := range maxVars {
		set(t, s, every, fmt.Sprintf("V%d", i), "x")
	}
	assert.ErrorIs(t, s.Set(t.Context(), Var{Name: "ONE_MORE", Value: "x", Target: Both}), ErrFull)
	assert.Len(t, list(t, s, every), maxVars)

	set(t, s, every, "V0", "changed")
	set(t, s, prod, "ONE_MORE", "x")
}

func TestSetForAClusterThatIsGoneIsRefused(t *testing.T) {
	s, db := newService(t)
	v := Var{ClusterID: "nowhere", Name: "EDITOR", Value: "vi", Target: Both}
	assert.ErrorIs(t, s.Set(t.Context(), v), ErrClusterGone)

	v.ClusterID = prod
	require.NoError(t, s.Set(t.Context(), v))
	_, err := db.Write.Exec(`UPDATE clusters SET delete_requested_at = 1 WHERE id IN (?, ?)`, dev, prod)
	require.NoError(t, err)
	v.ClusterID = dev
	assert.ErrorIs(t, s.Set(t.Context(), v), ErrClusterGone, "a cluster marked for deletion")
	v.ClusterID, v.Value = prod, "nvim"
	assert.ErrorIs(t, s.Set(t.Context(), v), ErrClusterGone, "a listed name, on a cluster marked for deletion")
	assert.Equal(t, "vi", list(t, s, prod)[0].Value)
}

func TestRemoveRemovesFromItsScopeAlone(t *testing.T) {
	s, _ := newService(t)
	set(t, s, every, "EDITOR", "vi")
	set(t, s, prod, "EDITOR", "nvim")

	assert.ErrorIs(t, s.Remove(t.Context(), dev, "EDITOR"), ErrNotSet)
	require.NoError(t, s.Remove(t.Context(), prod, "EDITOR"))
	assert.Empty(t, list(t, s, prod))
	assert.Len(t, list(t, s, every), 1)
}

func TestAClustersVariablesGoWithIt(t *testing.T) {
	s, db := newService(t)
	set(t, s, prod, "EDITOR", "nvim")
	_, err := db.Write.Exec(`DELETE FROM clusters WHERE id = ?`, prod)
	require.NoError(t, err)
	assert.Empty(t, list(t, s, prod))
}

// A command gets every cluster's variables, its cluster's value in place of
// one of the same name, then its cluster's others.
func TestACommandGetsItsClustersValueOverEveryClusters(t *testing.T) {
	s, _ := newService(t)
	set(t, s, every, "HELM_NAMESPACE", "apps")
	set(t, s, every, "EDITOR", "vi")
	set(t, s, prod, "PAGER", "less")
	set(t, s, prod, "HELM_NAMESPACE", "prod-apps")

	for _, sandboxed := range []bool{true, false} {
		assert.Equal(t, []string{"HELM_NAMESPACE=prod-apps", "EDITOR=vi", "PAGER=less"}, envFor(t, s, prod, sandboxed))
		assert.Equal(t, []string{"HELM_NAMESPACE=apps", "EDITOR=vi"}, envFor(t, s, dev, sandboxed))
		assert.Equal(t, []string{"HELM_NAMESPACE=apps", "EDITOR=vi"}, envFor(t, s, every, sandboxed), "a command with no cluster")
	}
}

func TestACommandGetsTheVariablesOfItsSideOfTheSandbox(t *testing.T) {
	s, _ := newService(t)
	ctx := t.Context()
	require.NoError(t, s.Set(ctx, Var{Name: "IN", Value: "1", Target: Sandbox}))
	require.NoError(t, s.Set(ctx, Var{Name: "OUT", Value: "1", Target: Outside}))
	require.NoError(t, s.Set(ctx, Var{Name: "ANY", Value: "1", Target: Both}))
	// A cluster's value replaces every cluster's only where it reaches.
	require.NoError(t, s.Set(ctx, Var{ClusterID: prod, Name: "ANY", Value: "2", Target: Outside}))

	assert.Equal(t, []string{"IN=1", "ANY=1"}, envFor(t, s, prod, true))
	assert.Equal(t, []string{"OUT=1", "ANY=2"}, envFor(t, s, prod, false))
	assert.Equal(t, []string{"IN=1", "ANY=1"}, envFor(t, s, "nowhere", true), "an unknown cluster has every cluster's alone")
}

// A stored row the check refuses, as an older build would have left it, is
// listed with its reason, given to no command, and removed like any other.
func TestARefusedRowIsListedWithItsReasonAndGivenToNoCommand(t *testing.T) {
	s, db := newService(t)
	set(t, s, every, "EDITOR", "vi")
	_, err := db.Write.Exec(`INSERT INTO env_vars (id, name, value, target, created_at, updated_at)
		VALUES (?, 'LD_PRELOAD', '/tmp/x.so', 'both', 0, 0)`, appdb.NewID())
	require.NoError(t, err)

	vars := list(t, s, every)
	require.Len(t, vars, 2)
	assert.Equal(t, "LD_PRELOAD changes how a program starts.", vars[0].Refusal)
	assert.Empty(t, vars[1].Refusal)
	assert.Equal(t, []string{"EDITOR=vi"}, envFor(t, s, every, true))
	assert.Equal(t, []string{"EDITOR=vi"}, envFor(t, s, every, false))

	require.NoError(t, s.Remove(t.Context(), every, "LD_PRELOAD"))
	assert.Len(t, list(t, s, every), 1)
}

// A cluster's value the check refuses still replaces every cluster's, so the
// command gets neither, never the value the user replaced. A row for both
// sides that only the sandbox refuses still reaches commands outside it.
func TestARefusedValueIsCheckedForItsSide(t *testing.T) {
	s, db := newService(t)
	set(t, s, every, "HELM_NAMESPACE", "apps")
	set(t, s, every, "EDITOR", "vi")
	insert := func(name, value string) {
		_, err := db.Write.Exec(`INSERT INTO env_vars (id, cluster_id, name, value, target, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'both', 0, 0)`, appdb.NewID(), prod, name, value)
		require.NoError(t, err)
	}
	insert("HELM_NAMESPACE", strings.Repeat("a", maxValue+1))
	insert("AWS_PROFILE", "dev")

	assert.Equal(t, []string{"EDITOR=vi"}, envFor(t, s, prod, true))
	assert.Equal(t, []string{"EDITOR=vi", "AWS_PROFILE=dev"}, envFor(t, s, prod, false))
	assert.Equal(t, []string{"HELM_NAMESPACE=apps", "EDITOR=vi"}, envFor(t, s, dev, false), "another cluster keeps every cluster's")
}

func TestNewFailsWithoutTheTable(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`DROP TABLE env_vars`)
	require.NoError(t, err)
	_, err = New(db, time.Now)
	assert.ErrorContains(t, err, "prepare envvars statements")
}

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A store that fails is an error from every read and write, never a panic or a
// silent success. Closing the DB is how a test fails it.
func TestAFailedStoreIsAnError(t *testing.T) {
	s, db := newService(t)
	set(t, s, every, "EDITOR", "vi")
	require.NoError(t, db.Close())
	ctx := t.Context()
	st := s.store.Stmts()
	v := Var{ClusterID: prod, Name: "EDITOR", Value: "nvim", Target: Both}

	_, err := s.List(ctx, every)
	assert.Error(t, err)
	_, err = s.For(ctx, prod, true)
	assert.Error(t, err)
	assert.Error(t, s.Set(ctx, v))
	assert.Error(t, s.Remove(ctx, every, "EDITOR"))

	// The helpers, each on its own, since a transaction that cannot begin
	// never reaches them.
	_, err = update(ctx, st, v, 0)
	assert.Error(t, err)
	err = checkCluster(ctx, st, prod)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrClusterGone)
	_, err = insert(ctx, st, v, 0)
	assert.Error(t, err)
}

// A row that does not scan is an error from every read that finds it.
func TestARowThatDoesNotScanIsAnError(t *testing.T) {
	s, db := newService(t)
	set(t, s, every, "EDITOR", "vi")
	for _, q := range []string{
		`ALTER TABLE env_vars RENAME TO env_vars_held`,
		`CREATE VIEW env_vars AS SELECT id, cluster_id, NULL AS name, value, target, created_at, updated_at FROM env_vars_held`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err)
	}

	_, err := s.List(t.Context(), every)
	assert.ErrorContains(t, err, "scan variable")
	_, err = s.For(t.Context(), every, true)
	assert.ErrorContains(t, err, "scan variable")
}

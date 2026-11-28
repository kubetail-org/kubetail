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

package clustertest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// A read answers what was applied, and a statement SQLite refuses is a
// *cluster.QueryError, as CachedData's ReadOwn answers it.
func TestReadAnswersAsTheServiceDoes(t *testing.T) {
	s := NewStore(t)
	s.Apply(t, map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "uid-1", "name": "api-0", "namespace": "prod"},
	})

	var got cluster.ClusterCachedDataQueryResult
	require.NoError(t, s.Read(context.Background(), func(query cluster.OwnQuery) error {
		var err error
		got, err = query(`SELECT name FROM objects`, 10, 1<<20)
		return err
	}))
	assert.Equal(t, [][]any{{"api-0"}}, got.Rows)

	err := s.Read(context.Background(), func(query cluster.OwnQuery) error {
		_, err := query(`SELECT nope FROM objects`, 10, 1<<20)
		return err
	})
	var qe *cluster.QueryError
	require.ErrorAs(t, err, &qe)
	assert.Contains(t, qe.Message, "no such column: nope")
}

// SetBody replaces the stored body alone: a statement that decompresses it reads
// the new one, and the columns written from the old one stay as they were.
func TestSetBodyReplacesTheBodyAlone(t *testing.T) {
	s := NewStore(t)
	s.Apply(t, map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "uid-1", "name": "api-0", "namespace": "prod"},
		"status":   map[string]any{"phase": "Running"},
	})

	s.SetBody(t, "uid-1", map[string]any{"status": map[string]any{"phase": "Pending"}})

	var got cluster.ClusterCachedDataQueryResult
	require.NoError(t, s.Read(context.Background(), func(query cluster.OwnQuery) error {
		var err error
		got, err = query(`SELECT status_summary, body(raw_json) ->> '$.status.phase' FROM main.objects`, 10, 1<<20)
		return err
	}))
	assert.Equal(t, [][]any{{"Running", "Pending"}}, got.Rows)
}

// A plan names how SQLite runs a statement, the tables by their main. names.
func TestPlanNamesTheIndexAStatementSearches(t *testing.T) {
	s := NewStore(t)

	plan := s.Plan(t, `SELECT name FROM main.objects WHERE api_version = 'v1' AND kind = 'Pod'`)

	assert.Contains(t, strings.Join(plan, "\n"), "INDEX objects_kind_ns_name (api_version=? AND kind=?)")
}

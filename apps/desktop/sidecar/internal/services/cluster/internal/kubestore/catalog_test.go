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

package kubestore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	podRow        = KindRow{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: ScopeNamespaced}
	deploymentRow = KindRow{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: ScopeNamespaced}
)

// catalogRows reads the table back in a deterministic order.
func catalogRows(t *testing.T, s *Store) []KindRow {
	t.Helper()
	rows, err := db(t, s).QueryContext(context.Background(),
		`SELECT api_version, kind, resource, scope, is_crd, COALESCE(printer_columns, '')
		 FROM kind_catalog ORDER BY api_version, resource`)
	require.NoError(t, err)
	defer rows.Close()

	var out []KindRow
	for rows.Next() {
		var r KindRow
		require.NoError(t, rows.Scan(&r.APIVersion, &r.Kind, &r.Resource, &r.Scope, &r.IsCRD, &r.PrinterColumns))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// metaValue reads one cluster_meta key back, or "" for one that is not there.
func metaValue(t *testing.T, s *Store, key string) string {
	t.Helper()
	var v string
	err := db(t, s).QueryRowContext(context.Background(),
		`SELECT value FROM cluster_meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	require.NoError(t, err)
	return v
}

// A sweep's answer lands as the table's rows.
func TestSyncKindsWritesTheAnswer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, deploymentRow}, true, 42))

	assert.Equal(t, []KindRow{deploymentRow, podRow}, catalogRows(t, s))
}

// The fingerprint is what tells a table the sweep wrote from one wiped under it, so it
// rides the rows' own transaction. Stored as its decimal string: the column is TEXT.
func TestSyncKindsRecordsTheFingerprint(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	require.NoError(t, s.SyncKinds(context.Background(), []KindRow{podRow}, true, 42))

	assert.Equal(t, "42", metaValue(t, s, kindsFingerprintKey))
}

// The atomicity the fold's whole fork rests on: a write that fails moves neither the rows
// nor the fingerprint, so the pair never claims a sweep wrote a table it did not.
func TestSyncKindsLeavesBothAloneWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 42))

	failed, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, s.SyncKinds(failed, []KindRow{deploymentRow}, true, 99))

	assert.Equal(t, []KindRow{podRow}, catalogRows(t, s))
	assert.Equal(t, "42", metaValue(t, s, kindsFingerprintKey))
}

// Pruning is the caller's call, because a partial answer has not seen every group: a kind
// missing from an incomplete sweep has not stopped being served.
func TestSyncKindsPrunesOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, deploymentRow}, true, 42))

	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, false, 7))
	assert.Len(t, catalogRows(t, s), 2, "an incomplete answer dropped a kind")

	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	assert.Equal(t, []KindRow{podRow}, catalogRows(t, s))
}

// The plural is unique too, so a Kind renamed under an unchanged plural collides with the
// row it replaces — on the index rather than on the primary key, which is the case a
// single-target upsert cannot resolve.
func TestSyncKindsResolvesARenamedKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	old := KindRow{APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", Scope: ScopeNamespaced, IsCRD: true}
	require.NoError(t, s.SyncKinds(ctx, []KindRow{old}, true, 1))

	renamed := old
	renamed.Kind = "Gadget"
	require.NoError(t, s.SyncKinds(ctx, []KindRow{renamed}, false, 2))

	assert.Equal(t, []KindRow{renamed}, catalogRows(t, s), "the rename's loser survived")
}

// A CRD's printer columns are the sweep's own data, so unlike schema_json they ride the update:
// a definition that drops a column must stop serving it rather than keep it forever.
func TestSyncKindsRoundTripsPrinterColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	withColumns := KindRow{
		APIVersion: "example.com/v1", Kind: "Widget", Resource: "widgets", Scope: ScopeNamespaced,
		IsCRD: true, PrinterColumns: `[{"name":"Replicas","type":"integer","jsonPath":".spec.replicas","priority":0}]`,
	}
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, withColumns}, true, 1))

	assert.Equal(t, []KindRow{withColumns, podRow}, catalogRows(t, s))

	// A built-in's NULL column reads as empty rather than failing the scan.
	assert.Empty(t, catalogRows(t, s)[1].PrinterColumns)

	cleared := withColumns
	cleared.PrinterColumns = ""
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow, cleared}, true, 2))

	assert.Equal(t, []KindRow{cleared, podRow}, catalogRows(t, s), "a dropped column kept being served")
}

// Nothing on this path fills schema_json, so an upsert must leave it alone: writing NULL
// on every sweep would quietly make the column unusable to whoever fills it.
func TestSyncKindsLeavesTheSchemaAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))
	_, err := db(t, s).ExecContext(ctx,
		`UPDATE kind_catalog SET schema_json = '{"x":1}' WHERE api_version = 'v1' AND kind = 'Pod'`)
	require.NoError(t, err)

	require.NoError(t, s.SyncKinds(ctx, []KindRow{podRow}, true, 7))

	var got *string
	require.NoError(t, db(t, s).QueryRowContext(ctx,
		`SELECT schema_json FROM kind_catalog WHERE api_version = 'v1' AND kind = 'Pod'`).Scan(&got))
	require.NotNil(t, got)
	assert.JSONEq(t, `{"x":1}`, *got)
}

// The prune runs before the fingerprint, so a prune that fails takes the whole sweep's
// answer with it — a catalog emptied under a fingerprint that says it was written would
// read as a cluster serving nothing.
func TestSyncKindsReportsAFailedPrune(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.SyncKinds(ctx, []KindRow{
		{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced"},
	}, true, 1))
	failWrites(t, s, "kind_catalog", "DELETE")

	// No rows, so the prune is the sweep's first statement and the fault lands on it.
	err := s.SyncKinds(ctx, nil, true, 2)

	assert.ErrorContains(t, err, "prune")
}

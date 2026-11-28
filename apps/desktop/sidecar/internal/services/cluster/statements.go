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

// Every statement cluster issues against app.db, named once and prepared once
// (sqlstmt.Set). The text lives here and never at a call site.
package cluster

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"

// stmtID indexes statements.
type stmtID int

const (
	stmtInsertCluster stmtID = iota
	stmtSetClusterEnabled
	stmtSetClusterSyncEnabled
	stmtClearClusterDefault
	stmtSetClusterDefault
	stmtMarkCluster
	stmtDeleteMarkedCluster
	stmtSelectCluster
	stmtSelectClusters
	numStmts int = iota
)

// clusterColumns is the column list every read scans, in scanClusterRow's order.
// The monitoring switch is the cluster's monitor target's, on until the monitor
// has made one. 'kubernetes' and the key's being the cluster's id are
// kubesignals.Name and TargetKey's, which this package cannot import.
const clusterColumns = `id, source, source_key, name, enabled, sync_enabled,
	coalesce((SELECT t.enabled FROM monitor_targets t WHERE t.integration = 'kubernetes' AND t.key = clusters.id), 1),
	is_default, created_at, updated_at, delete_requested_at`

// Every write that may touch nothing RETURNs what it wrote, so the row — or no
// row — comes back from the write itself rather than from a read a concurrent
// write can land between.
var statements = []sqlstmt.Statement{
	// The toggles are left to their defaults, which is what preserves a returning
	// context's choices: the conflict is the row that already holds them.
	stmtInsertCluster: sqlstmt.OnWriter(`INSERT INTO clusters (id, source, source_key, created_at, updated_at)
	VALUES (?, 'kubeconfig', ?, ?, ?) ON CONFLICT (source, source_key) DO NOTHING RETURNING id`),

	// A toggle refuses a marked row: the user's choices are closed once its
	// deletion is asked for. A disabled cluster is never the default, so disabling
	// clears is_default.
	stmtSetClusterEnabled: sqlstmt.OnWriter(`UPDATE clusters SET enabled = ?1, is_default = is_default AND ?1, updated_at = ?2
	WHERE id = ?3 AND delete_requested_at IS NULL RETURNING ` + clusterColumns),
	stmtSetClusterSyncEnabled: sqlstmt.OnWriter(`UPDATE clusters SET sync_enabled = ?, updated_at = ?
	WHERE id = ? AND delete_requested_at IS NULL RETURNING ` + clusterColumns),
	// The default's two writes run in one transaction, the clear first, so the
	// one-default index never sees two marked rows.
	stmtClearClusterDefault: sqlstmt.OnWriter(`UPDATE clusters SET is_default = 0, updated_at = ? WHERE is_default = 1`),
	stmtSetClusterDefault: sqlstmt.OnWriter(`UPDATE clusters SET is_default = ?, updated_at = ?
	WHERE id = ? AND delete_requested_at IS NULL RETURNING ` + clusterColumns),
	// A repeat is a no-op that keeps the first stamp. A marked row is never the
	// default, so marking clears is_default.
	stmtMarkCluster: sqlstmt.OnWriter(`UPDATE clusters SET delete_requested_at = ?, is_default = 0, updated_at = ?
	WHERE id = ? AND delete_requested_at IS NULL RETURNING id`),
	// Only a marked row goes, and only once no chat is filed under it: the
	// chat sweeper empties the cluster first, and the FK cascade is the backstop.
	stmtDeleteMarkedCluster: sqlstmt.OnWriter(`DELETE FROM clusters WHERE id = ? AND delete_requested_at IS NOT NULL
	AND NOT EXISTS (SELECT 1 FROM chats c WHERE c.cluster_id = clusters.id) RETURNING id`),

	stmtSelectCluster: sqlstmt.OnBoth(`SELECT ` + clusterColumns + ` FROM clusters WHERE id = ?`),
	// Ordered by id for a stable listing, nothing more: a cloud row keeps the id
	// the cloud minted, so id order is not import order.
	stmtSelectClusters: sqlstmt.OnReader(`SELECT ` + clusterColumns + ` FROM clusters ORDER BY id`),
}

// stmts issues the set's statements, on the pools or inside a transaction.
type stmts = sqlstmt.Stmts[stmtID]

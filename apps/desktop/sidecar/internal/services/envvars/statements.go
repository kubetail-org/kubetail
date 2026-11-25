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

// Every statement envvars issues, named once and prepared once on the app's
// DB (sqlstmt.Set). The text lives here and never at a call site. A scope is
// keyed as memory's is: a cluster's by its id, every cluster's by the empty
// string.
package envvars

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"

// stmtID indexes statements.
type stmtID int

const (
	stmtSelectScope stmtID = iota
	stmtSelectFor
	stmtSelectClusterAccepts
	stmtInsert
	stmtUpdate
	stmtDelete
	numStmts int = iota
)

const columns = `ifnull(cluster_id, ''), name, value, target`

// statements is the table. A read that runs inside a write transaction is OnBoth.
var statements = []sqlstmt.Statement{
	stmtSelectScope: sqlstmt.OnReader(`SELECT ` + columns + ` FROM env_vars
	WHERE ifnull(cluster_id, '') = ? ORDER BY created_at, id`),
	// What a command of a cluster's chat is given: every cluster's variables,
	// then the cluster's own, of one target or both.
	stmtSelectFor: sqlstmt.OnReader(`SELECT ` + columns + ` FROM env_vars
	WHERE (cluster_id IS NULL OR cluster_id = ?) AND target IN (?, 'both')
	ORDER BY cluster_id IS NOT NULL, created_at, id`),
	stmtSelectClusterAccepts: sqlstmt.OnBoth(`SELECT 1 FROM clusters WHERE id = ? AND delete_requested_at IS NULL`),
	// Inserts nothing once the scope holds the last argument's rows.
	stmtInsert: sqlstmt.OnWriter(`INSERT INTO env_vars (id, cluster_id, name, value, target, created_at, updated_at)
	SELECT ?1, nullif(?2, ''), ?3, ?4, ?5, ?6, ?6
	WHERE (SELECT count(*) FROM env_vars WHERE ifnull(cluster_id, '') = ?2) < ?7`),
	stmtUpdate: sqlstmt.OnWriter(`UPDATE env_vars SET value = ?, target = ?, updated_at = ?
	WHERE ifnull(cluster_id, '') = ? AND name = ?`),
	stmtDelete: sqlstmt.OnWriter(`DELETE FROM env_vars WHERE ifnull(cluster_id, '') = ? AND name = ?`),
}

type stmts = sqlstmt.Stmts[stmtID]

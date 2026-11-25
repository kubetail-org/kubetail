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

// Every statement memory issues, named once and prepared once on the app's DB
// (sqlstmt.Set). The text lives here and never at a call site.
package memory

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"

// stmtID indexes statements.
type stmtID int

const (
	stmtSelectVisible stmtID = iota
	stmtInsert
	stmtUpdate
	stmtSelectClusterAccepts
	stmtSelectNameInScope
	stmtSelectByNameInScope
	stmtSelectByID
	stmtSelectScope
	stmtDelete
	numStmts int = iota
)

// memoryColumns is the projection every read scans, in scanMemory's order.
const memoryColumns = `id, cluster_id, server_uid, name, body, written_by, chat_id, created_at, updated_at`

// statements is the table. A read that runs inside a write transaction is OnBoth.
var statements = []sqlstmt.Statement{
	// A cluster's own first, then the global ones, each by name.
	stmtSelectVisible: sqlstmt.OnBoth(`SELECT ` + memoryColumns + ` FROM memories
	WHERE cluster_id = ? OR cluster_id IS NULL ORDER BY cluster_id IS NULL, name`),
	stmtInsert: sqlstmt.OnWriter(`INSERT INTO memories (` + memoryColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
	stmtUpdate: sqlstmt.OnWriter(`UPDATE memories SET cluster_id = ?, server_uid = ?, name = ?, body = ?,
	written_by = ?, chat_id = ?, updated_at = ? WHERE id = ?`),
	stmtSelectClusterAccepts: sqlstmt.OnBoth(`SELECT 1 FROM clusters WHERE id = ? AND delete_requested_at IS NULL`),
	// Whether any memory but the one named holds the name in a scope: a cluster's
	// by its id, the global ones by ''.
	stmtSelectNameInScope: sqlstmt.OnBoth(`SELECT 1 FROM memories WHERE ifnull(cluster_id, '') = ? AND name = ? AND id <> ?`),
	// The memory under a name in one scope: a cluster's by its id, the global ones by ''.
	stmtSelectByNameInScope: sqlstmt.OnBoth(`SELECT ` + memoryColumns + ` FROM memories
	WHERE ifnull(cluster_id, '') = ? AND name = ?`),
	stmtSelectByID: sqlstmt.OnBoth(`SELECT ` + memoryColumns + ` FROM memories WHERE id = ?`),
	// The memories in one scope: a cluster's by its id, the global ones by ''.
	stmtSelectScope: sqlstmt.OnBoth(`SELECT ` + memoryColumns + ` FROM memories WHERE ifnull(cluster_id, '') = ?`),
	stmtDelete:      sqlstmt.OnWriter(`DELETE FROM memories WHERE id = ?`),
}

type stmts = sqlstmt.Stmts[stmtID]

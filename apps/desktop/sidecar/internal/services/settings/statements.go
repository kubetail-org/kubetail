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

package settings

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"

// stmtID names one of the store's statements over app.db.
type stmtID int

const (
	stmtSelectClusterKubeContext stmtID = iota
	stmtSelectRules
	stmtInsertRule
	stmtUpdateRule
	stmtDeleteRule
	stmtSelectAccountModes
	stmtSelectModes
	stmtUpsertMode
	stmtDeleteMode
	stmtSelectFolderGrants
	stmtUpsertFolderGrant
	stmtDeleteFolderGrant
	stmtSelectExecutables
	stmtSelectExecutableListed
	stmtInsertExecutable
	stmtDeleteExecutable
	stmtSelectShellPath
	stmtUpsertShellPath
	stmtSelectCloudPlatformSwitches
	stmtSelectScopedSetting
	stmtSelectSettingScopes
	stmtUpsertSetting
)

// statements are the store's. Those over a table with a chat_id touch only
// rows whose chat_id is NULL: a chat's rows are agent/chat's. The text lives here and never at a call site.
var statements = []sqlstmt.Statement{
	// Whether a cluster not awaiting deletion has a kube-context. OnBoth,
	// since a mode's write reads it in its transaction.
	stmtSelectClusterKubeContext: sqlstmt.OnBoth(`SELECT source = 'kubeconfig' AND ifnull(source_key, '') <> ''
	FROM clusters WHERE id = ? AND delete_requested_at IS NULL`),
	// OnBoth, since a rule's write reads the rules in its transaction.
	stmtSelectRules: sqlstmt.OnBoth(`SELECT ` + RuleColumns + ` FROM permission_rules WHERE chat_id IS NULL ORDER BY created_at, id`),
	stmtInsertRule: sqlstmt.OnWriter(`INSERT INTO permission_rules (chat_id, ` + RuleColumns + `, created_at, updated_at)
	VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
	// An edit keeps the rule's id and created_at, so it keeps its place.
	stmtUpdateRule: sqlstmt.OnWriter(`UPDATE permission_rules SET effect = ?, access_level = ?, principal = ?, target = ?,
	scope = ?, service = ?, operation = ?, resource = ?, updated_at = ? WHERE chat_id IS NULL AND id = ?`),
	stmtDeleteRule: sqlstmt.OnWriter(`DELETE FROM permission_rules WHERE chat_id IS NULL AND id = ?`),

	// An account's row and its target's default: target, account.
	stmtSelectAccountModes: sqlstmt.OnReader(`SELECT account, mode FROM permission_modes WHERE target = ?1 AND account IN (?2, '*')`),
	stmtSelectModes:        sqlstmt.OnReader(`SELECT target, account, mode FROM permission_modes ORDER BY target, account`),
	// A row's first write sets created_at; every write moves updated_at.
	stmtUpsertMode: sqlstmt.OnWriter(`INSERT INTO permission_modes (target, account, mode, created_at, updated_at) VALUES (?1, ?2, ?3, ?4, ?4)
	ON CONFLICT (target, account) DO UPDATE SET mode = excluded.mode, updated_at = excluded.updated_at`),
	stmtDeleteMode: sqlstmt.OnWriter(`DELETE FROM permission_modes WHERE target = ? AND account = ?`),

	stmtSelectFolderGrants: sqlstmt.OnReader(`SELECT ` + FolderGrantColumns + ` FROM folder_grants WHERE chat_id IS NULL ORDER BY created_at, id`),
	// A folder granted again keeps its row's id and created_at, and changes its access.
	stmtUpsertFolderGrant: sqlstmt.OnWriter(`INSERT INTO folder_grants (id, chat_id, folder, access, created_at) VALUES (?, NULL, ?, ?, ?)
	ON CONFLICT (ifnull(chat_id, ''), folder) DO UPDATE SET access = excluded.access`),
	stmtDeleteFolderGrant: sqlstmt.OnWriter(`DELETE FROM folder_grants WHERE chat_id IS NULL AND id = ?`),

	// rowid orders two added in the same millisecond.
	stmtSelectExecutables: sqlstmt.OnReader(`SELECT name, invocation FROM executables ORDER BY created_at, rowid`),
	// OnBoth, since a register reads it in its transaction.
	stmtSelectExecutableListed: sqlstmt.OnBoth(`SELECT EXISTS (SELECT 1 FROM executables WHERE name = ?)`),
	stmtInsertExecutable:       sqlstmt.OnWriter(`INSERT INTO executables (name, invocation, created_at) VALUES (?, ?, ?)`),
	stmtDeleteExecutable:       sqlstmt.OnWriter(`DELETE FROM executables WHERE name = ?`),

	// OnBoth, since a write compares the row it replaces in its transaction.
	stmtSelectShellPath: sqlstmt.OnBoth(`SELECT path FROM shell_path WHERE id = 1`),
	stmtUpsertShellPath: sqlstmt.OnWriter(`INSERT INTO shell_path (id, path) VALUES (1, ?)
	ON CONFLICT (id) DO UPDATE SET path = excluded.path`),

	// The four switches for every cluster, by their keys (cloudPlatformSwitchKeys).
	stmtSelectCloudPlatformSwitches: sqlstmt.OnReader(`SELECT key, value FROM settings
	WHERE ifnull(cluster_id, '') = '' AND key IN (?, ?, ?, ?)`),
	// The settings reads below are OnBoth, so a caller reads inside its own write.
	stmtSelectScopedSetting: sqlstmt.OnBoth(selectScopedSettingQuery),
	stmtSelectSettingScopes: sqlstmt.OnBoth(`SELECT ifnull(cluster_id, ''), value FROM settings WHERE key = ?`),
	stmtUpsertSetting:       sqlstmt.OnWriter(upsertSettingQuery),
}

// selectScopedSettingQuery reads one key for one cluster and for every
// cluster, through the index on cluster_id that spells every cluster as the
// empty string. Its arguments are the cluster and the key.
const selectScopedSettingQuery = `SELECT ifnull(cluster_id, ''), value FROM settings
	WHERE ifnull(cluster_id, '') IN (?, '') AND key = ?`

// upsertSettingQuery writes a key's value in a scope: key, cluster (empty for
// every cluster), value, time. A row's first write sets created_at; every write
// moves updated_at.
const upsertSettingQuery = `INSERT INTO settings (key, cluster_id, value, created_at, updated_at)
	VALUES (?1, nullif(?2, ''), ?3, ?4, ?4)
	ON CONFLICT (ifnull(cluster_id, ''), key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

type stmts = sqlstmt.Stmts[stmtID]

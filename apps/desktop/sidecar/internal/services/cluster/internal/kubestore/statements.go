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

// Every statement the store issues, named once and prepared once per file
// (sqlstmt.Set). The text lives here and never at a call site.
package kubestore

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"

// stmtID names one statement, indexing statements.
type stmtID int

const (
	stmtUpsertObject stmtID = iota
	stmtInsertStatusTransition
	stmtDeleteObject
	stmtSweepObjects
	// The deletes log, one id per delete path: each copies the doomed rows off the
	// delete's own predicate, and runs before it.
	stmtLogDeleteObject
	stmtLogDeleteEvent
	stmtLogSweepObjects
	stmtLogPruneEvents
	stmtLogClearObjectsOfKind
	stmtLogDeleteAllEvents
	// The per-object cascade: one point delete per side table.
	stmtDeleteLabelsOfObject
	stmtDeleteOwnerRefsOfChild
	stmtDeleteOwnerRefsOwnedBy
	stmtDeleteStatusHistoryOfObject
	// The same four over the uid list a sweep returned.
	stmtDeleteLabelsOfObjects
	stmtDeleteOwnerRefsOfChildren
	stmtDeleteOwnerRefsOwnedByAny
	stmtDeleteStatusHistoryOfObjects
	stmtUpsertOwnerRefs
	stmtUpsertLabels
	stmtDeleteContainersOfPod
	stmtDeleteContainersOfPods
	stmtInsertContainers
	stmtDeleteRefsOfObject
	stmtDeleteRefsOfObjects
	stmtInsertRefs
	stmtDeleteSelectorOfObject
	stmtDeleteSelectorsOfObjects
	stmtDeleteSelectorTermsOfObject
	stmtDeleteSelectorTermsOfObjects
	stmtInsertSelector
	stmtInsertSelectorTerms
	// One kind's rows, for a kind that has stopped being synced.
	stmtClearOwnerRefsOfKind
	stmtClearLabelsOfKind
	stmtClearContainersOfKind
	stmtClearRefsOfKind
	stmtClearSelectorsOfKind
	stmtClearSelectorTermsOfKind
	stmtClearStatusHistoryOfKind
	stmtClearObjectsOfKind
	stmtDeleteKindCount
	stmtUpsertEvent
	stmtDeleteEvent
	stmtDeleteAllEvents
	stmtPruneEvents
	stmtUpsertMeta
	stmtDeleteMeta
	stmtMarkTrimmedDeletes
	stmtNextSeq
	stmtResolveKindRename
	stmtUpsertKind
	stmtDeleteAllKinds
	stmtPruneKinds
	stmtSweepStatusHistory
	stmtTrimDeletes
	stmtSelectMeta
	stmtCountKind
	stmtSelectKinds
	stmtSelectListedKinds
	stmtSelectEvents
	stmtSelectObjectBody
	stmtSelectObjects
	// What moved past a cursor: one range per table plus the log, and the kind the
	// caller's plural names.
	stmtResolveKind
	stmtSelectObjectsSince
	stmtSelectObjectDeletesSince
	stmtSelectEventsSince
	stmtSelectEventDeletesSince
	numStmts int = iota
)

// statements is the SQL each id stands for and the pool it is prepared on. The pool is
// declared by hand beside the text, so a write filed as a read is invisible until SQLite
// answers "attempt to write a readonly database"; the cross-check in statements_test.go
// is what catches it.
var statements = []sqlstmt.Statement{
	stmtUpsertObject: sqlstmt.OnWriter(`
		INSERT INTO objects (
			uid, api_version, kind, namespace, name,
			resource_version, generation, created_at, updated_at, raw_json,
			status_summary, ready_count, total_count, restart_count, host, write_seq, changed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uid) DO UPDATE SET
			api_version=excluded.api_version,
			kind=excluded.kind,
			namespace=excluded.namespace,
			name=excluded.name,
			resource_version=excluded.resource_version,
			-- The stamp moves only when the write was effective: a relist rewrites every
			-- row of a kind, and moving it on each rewrite would make a cold list read as
			-- one change per object. Two ways a row would otherwise keep a position no
			-- reader will look below. An EMPTY version (nothing upstream rejects one) is
			-- equal to itself forever, so the row would freeze at its first write. And
			-- identity is rewritten by this same SET list, so a uid that moved kind would
			-- sit below its new kind's readers — objects_identity_change logs the
			-- departure to the old kind at the position this CASE hands the row.
			write_seq=CASE WHEN excluded.resource_version <> ''
			                AND excluded.resource_version = objects.resource_version
			                AND excluded.api_version = objects.api_version
			                AND excluded.kind = objects.kind
			               THEN objects.write_seq ELSE excluded.write_seq END,
			-- The same condition as write_seq's, so the two agree on what a change is.
			changed_at=CASE WHEN excluded.resource_version <> ''
			                 AND excluded.resource_version = objects.resource_version
			                 AND excluded.api_version = objects.api_version
			                 AND excluded.kind = objects.kind
			                THEN objects.changed_at ELSE excluded.changed_at END,
			generation=excluded.generation,
			-- creationTimestamp is immutable, so a body without it carries no news;
			-- projectObject leaves it 0, which would otherwise overwrite a good value
			-- with the epoch.
			created_at=CASE WHEN excluded.created_at > 0 THEN excluded.created_at ELSE created_at END,
			updated_at=excluded.updated_at,
			raw_json=excluded.raw_json,
			status_summary=excluded.status_summary,
			ready_count=excluded.ready_count,
			total_count=excluded.total_count,
			restart_count=excluded.restart_count,
			host=excluded.host`),

	stmtInsertStatusTransition: sqlstmt.OnWriter(`
		INSERT INTO status_history(uid, at, summary)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM objects WHERE uid = ? AND status_summary = ?)`),

	stmtDeleteObject: sqlstmt.OnWriter(`DELETE FROM objects WHERE uid=?`),

	stmtLogDeleteObject: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, api_version, kind, uid, ? FROM objects WHERE uid = ?`),
	stmtLogDeleteEvent: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, 'v1', 'Event', uid, ? FROM events WHERE uid = ?`),
	stmtLogSweepObjects: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, api_version, kind, uid, ?
		FROM objects WHERE api_version=? AND kind=? AND updated_at < ?`),
	stmtLogPruneEvents: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, 'v1', 'Event', uid, ? FROM events WHERE updated_at < ?`),
	stmtLogClearObjectsOfKind: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, api_version, kind, uid, ? FROM objects WHERE api_version = ? AND kind = ?`),
	stmtLogDeleteAllEvents: sqlstmt.OnWriter(`
		INSERT INTO deletes (seq, api_version, kind, uid, at)
		SELECT ?, 'v1', 'Event', uid, ? FROM events`),

	stmtSweepObjects: sqlstmt.OnWriter(`DELETE FROM objects WHERE api_version=? AND kind=? AND updated_at < ? RETURNING uid`),

	stmtDeleteLabelsOfObject:        sqlstmt.OnWriter(`DELETE FROM labels WHERE uid=?`),
	stmtDeleteOwnerRefsOfChild:      sqlstmt.OnWriter(`DELETE FROM owner_refs WHERE child_uid=?`),
	stmtDeleteOwnerRefsOwnedBy:      sqlstmt.OnWriter(`DELETE FROM owner_refs WHERE owner_uid=?`),
	stmtDeleteStatusHistoryOfObject: sqlstmt.OnWriter(`DELETE FROM status_history WHERE uid=?`),

	stmtDeleteLabelsOfObjects:        sqlstmt.OnWriter(`DELETE FROM labels WHERE uid IN (SELECT value FROM json_each(?))`),
	stmtDeleteOwnerRefsOfChildren:    sqlstmt.OnWriter(`DELETE FROM owner_refs WHERE child_uid IN (SELECT value FROM json_each(?))`),
	stmtDeleteOwnerRefsOwnedByAny:    sqlstmt.OnWriter(`DELETE FROM owner_refs WHERE owner_uid IN (SELECT value FROM json_each(?))`),
	stmtDeleteStatusHistoryOfObjects: sqlstmt.OnWriter(`DELETE FROM status_history WHERE uid IN (SELECT value FROM json_each(?))`),

	// WHERE true is required on both: without it SQLite parses ON CONFLICT as a join
	// constraint on the SELECT and the statement is a syntax error at DO.
	stmtUpsertOwnerRefs: sqlstmt.OnWriter(`
		INSERT INTO owner_refs (child_uid, owner_uid, is_controller)
		SELECT ?1, value ->> 0, value ->> 1 FROM json_each(?2) WHERE true
		ON CONFLICT(child_uid, owner_uid) DO UPDATE SET is_controller=excluded.is_controller`),
	stmtUpsertLabels: sqlstmt.OnWriter(`
		INSERT INTO labels (uid, key, value)
		SELECT ?1, key, value FROM json_each(?2) WHERE true
		ON CONFLICT(uid, key) DO UPDATE SET value=excluded.value`),

	stmtDeleteContainersOfPod:  sqlstmt.OnWriter(`DELETE FROM containers WHERE pod_uid=?`),
	stmtDeleteContainersOfPods: sqlstmt.OnWriter(`DELETE FROM containers WHERE pod_uid IN (SELECT value FROM json_each(?))`),
	// Materialized, since json_each renders value again on every reference and a row reads
	// it eighteen times. Read off one text, the extractions share SQLite's parse of it.
	stmtInsertContainers: sqlstmt.OnWriter(`
		INSERT INTO containers (
			pod_uid, init, position, name, sidecar, image, image_id, container_id, ready, restarts,
			state, reason, exit_code, last_reason, last_exit_code, last_container_id,
			cpu_request, cpu_limit, memory_request, memory_limit
		)
		WITH c(v) AS MATERIALIZED (SELECT value FROM json_each(?2))
		SELECT ?1, v ->> 'init', v ->> 'position', v ->> 'name', v ->> 'sidecar',
			v ->> 'image', v ->> 'image_id', v ->> 'container_id', v ->> 'ready', v ->> 'restarts',
			v ->> 'state', v ->> 'reason', v ->> 'exit_code', v ->> 'last_reason',
			v ->> 'last_exit_code', v ->> 'last_container_id', v ->> 'cpu_request', v ->> 'cpu_limit',
			v ->> 'memory_request', v ->> 'memory_limit'
		FROM c`),

	stmtDeleteRefsOfObject:  sqlstmt.OnWriter(`DELETE FROM refs WHERE uid=?`),
	stmtDeleteRefsOfObjects: sqlstmt.OnWriter(`DELETE FROM refs WHERE uid IN (SELECT value FROM json_each(?))`),
	// Materialized for the reason stmtInsertContainers is.
	stmtInsertRefs: sqlstmt.OnWriter(`
		INSERT INTO refs (uid, path, to_group, to_kind, to_namespace, to_name, key, optional)
		WITH r(v) AS MATERIALIZED (SELECT value FROM json_each(?2))
		SELECT ?1, v ->> 'path', v ->> 'to_group', v ->> 'to_kind', v ->> 'to_namespace',
			v ->> 'to_name', v ->> 'key', v ->> 'optional'
		FROM r`),

	stmtDeleteSelectorOfObject:       sqlstmt.OnWriter(`DELETE FROM selectors WHERE uid=?`),
	stmtDeleteSelectorsOfObjects:     sqlstmt.OnWriter(`DELETE FROM selectors WHERE uid IN (SELECT value FROM json_each(?))`),
	stmtDeleteSelectorTermsOfObject:  sqlstmt.OnWriter(`DELETE FROM selector_terms WHERE uid=?`),
	stmtDeleteSelectorTermsOfObjects: sqlstmt.OnWriter(`DELETE FROM selector_terms WHERE uid IN (SELECT value FROM json_each(?))`),
	stmtInsertSelector:               sqlstmt.OnWriter(`INSERT INTO selectors (uid, namespace) VALUES (?, ?)`),
	// A term's number is its index in the bound array; vals comes out as the array's JSON text.
	stmtInsertSelectorTerms: sqlstmt.OnWriter(`
		INSERT INTO selector_terms (uid, term, key, op, vals)
		WITH t(i, v) AS MATERIALIZED (SELECT key, value FROM json_each(?2))
		SELECT ?1, i, v ->> 'key', v ->> 'op', v ->> 'vals' FROM t`),

	stmtClearOwnerRefsOfKind:     sqlstmt.OnWriter(`DELETE FROM owner_refs WHERE child_uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearLabelsOfKind:        sqlstmt.OnWriter(`DELETE FROM labels WHERE uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearContainersOfKind:    sqlstmt.OnWriter(`DELETE FROM containers WHERE pod_uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearRefsOfKind:          sqlstmt.OnWriter(`DELETE FROM refs WHERE uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearSelectorsOfKind:     sqlstmt.OnWriter(`DELETE FROM selectors WHERE uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearSelectorTermsOfKind: sqlstmt.OnWriter(`DELETE FROM selector_terms WHERE uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearStatusHistoryOfKind: sqlstmt.OnWriter(`DELETE FROM status_history WHERE uid IN (SELECT uid FROM objects WHERE api_version = ? AND kind = ?)`),
	stmtClearObjectsOfKind:       sqlstmt.OnWriter(`DELETE FROM objects WHERE api_version = ? AND kind = ?`),
	stmtDeleteKindCount:          sqlstmt.OnWriter(`DELETE FROM kind_counts WHERE api_version = ? AND kind = ?`),

	stmtUpsertEvent: sqlstmt.OnWriter(`
		INSERT INTO events (
			uid, involved_uid, involved_kind, involved_ns, involved_name,
			type, reason, message, first_seen, last_seen, count, raw_json, updated_at,
			resource_version, write_seq
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uid) DO UPDATE SET
			resource_version=excluded.resource_version,
			-- As on objects: a re-observed event that has not moved keeps its stamp, so a
			-- relist of the table is not one change per event — and an empty version is
			-- never "unchanged", or the row freezes at the position of its first write.
			write_seq=CASE WHEN excluded.resource_version <> ''
			                AND excluded.resource_version = events.resource_version
			               THEN events.write_seq ELSE excluded.write_seq END,
			involved_uid=excluded.involved_uid,
			involved_kind=excluded.involved_kind,
			involved_ns=excluded.involved_ns,
			involved_name=excluded.involved_name,
			type=excluded.type,
			reason=excluded.reason,
			message=excluded.message,
			first_seen=excluded.first_seen,
			last_seen=excluded.last_seen,
			count=excluded.count,
			raw_json=excluded.raw_json,
			updated_at=excluded.updated_at`),
	stmtDeleteEvent:     sqlstmt.OnWriter(`DELETE FROM events WHERE uid=?`),
	stmtDeleteAllEvents: sqlstmt.OnWriter(`DELETE FROM events`),
	stmtPruneEvents:     sqlstmt.OnWriter(`DELETE FROM events WHERE updated_at < ?`),

	stmtUpsertMeta: sqlstmt.OnWriter(`
		INSERT INTO cluster_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`),
	stmtDeleteMeta: sqlstmt.OnWriter(`DELETE FROM cluster_meta WHERE key = ?`),
	// The trim's marks, read from the rows the delete beside it is about to take — same
	// predicate, same transaction, so the two cannot disagree about which entries went.
	// The key mirrors deletesTrimmedKey; trimmedMark reads through it, so a drift fails
	// every janitor mark test. Raise-only: `at` and `seq` rise together only within one
	// file lifetime (the clock stamp restarts on reopen, the counter is on disk), so a
	// later sweep can compute a LOWER mark, and following it down would revalidate a
	// cursor whose deletes are gone.
	stmtMarkTrimmedDeletes: sqlstmt.OnWriter(`
		INSERT INTO cluster_meta (key, value)
		SELECT 'deletes/trimmed/' || api_version || '/' || kind, CAST(MAX(seq) AS TEXT)
		FROM deletes WHERE at < ?
		GROUP BY api_version, kind
		ON CONFLICT(key) DO UPDATE SET
			value = CASE WHEN CAST(excluded.value AS INTEGER) > CAST(cluster_meta.value AS INTEGER)
			             THEN excluded.value ELSE cluster_meta.value END`),
	// A write that returns a row, so it runs on the writer like any other. The key is
	// spliced from seqKey rather than spelled again: a drift would match no row, and every
	// write in the store would end in a bare sql.ErrNoRows.
	stmtNextSeq: sqlstmt.OnWriter(`UPDATE cluster_meta SET value = value + 1 WHERE key = '` + seqKey + `' RETURNING value`),

	stmtResolveKindRename: sqlstmt.OnWriter(`DELETE FROM kind_catalog WHERE api_version = ? AND resource = ? AND kind <> ?`),
	// schema_json is deliberately absent from the update: nothing here fills it, and
	// writing NULL on every sweep would make the column unusable to whoever does.
	// printer_columns is the opposite case — the sweep is what fills it, so it rides the
	// update, or a CRD that drops a column would keep being served with it.
	stmtUpsertKind: sqlstmt.OnWriter(`
		INSERT INTO kind_catalog (api_version, kind, resource, scope, is_crd, printer_columns)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_version, kind) DO UPDATE SET
			resource = excluded.resource, scope = excluded.scope, is_crd = excluded.is_crd,
			printer_columns = excluded.printer_columns`),
	stmtDeleteAllKinds: sqlstmt.OnWriter(`DELETE FROM kind_catalog`),
	stmtPruneKinds: sqlstmt.OnWriter(`
		DELETE FROM kind_catalog
		WHERE (api_version, kind) NOT IN (SELECT value ->> 0, value ->> 1 FROM json_each(?))`),

	stmtSweepStatusHistory: sqlstmt.OnWriter(`DELETE FROM status_history WHERE at < ?`),
	stmtTrimDeletes:        sqlstmt.OnWriter(`DELETE FROM deletes WHERE at < ?`),

	stmtSelectMeta: sqlstmt.OnReader(`SELECT value FROM cluster_meta WHERE key = ?`),
	stmtCountKind:  sqlstmt.OnReader(`SELECT count FROM kind_counts WHERE api_version=? AND kind=?`),
	// The kind catalog with each kind's count. ?1 is a namespace: empty reads every
	// count off kind_counts; otherwise a namespaced kind is counted in ?1 alone, its
	// objects through objects_ns_kind and Events by the namespace of the object each is
	// about, through events_involved_ns. A cluster-scoped kind keeps its whole count.
	// CASE stops at the first match, so an empty ?1 runs neither subquery.
	stmtSelectKinds: sqlstmt.OnReader(`
		SELECT kc.api_version, kc.kind, kc.resource, kc.scope, kc.is_crd,
		       COALESCE(kc.printer_columns, ''),
		       CASE
		           WHEN ?1 = '' OR kc.scope <> 'Namespaced' THEN COALESCE(knt.count, 0)
		           WHEN kc.api_version = 'v1' AND kc.kind = 'Event'
		               THEN (SELECT COUNT(*) FROM events e WHERE e.involved_ns = ?1)
		           ELSE (SELECT COUNT(*) FROM objects o
		                 WHERE o.namespace = ?1 AND o.api_version = kc.api_version AND o.kind = kc.kind)
		       END
		FROM kind_catalog kc
		LEFT JOIN kind_counts knt ON knt.api_version = kc.api_version AND knt.kind = kc.kind
		ORDER BY kc.api_version, kc.kind`),
	// The key is cookieKey's, as the refs view builds it.
	stmtSelectListedKinds: sqlstmt.OnReader(`
		SELECT kc.api_version, kc.kind, kc.resource
		FROM kind_catalog kc
		JOIN cluster_meta m ON m.key = 'cookie/' || kc.api_version || '/' || kc.resource
		ORDER BY kc.api_version, kc.kind`),
	stmtSelectEvents: sqlstmt.OnReader(`
		SELECT ` + eventColumns + `
		FROM events
		ORDER BY last_seen DESC, uid DESC`),
	stmtSelectObjectBody: sqlstmt.OnReader(`SELECT raw_json FROM objects WHERE uid = ?`),
	// The caller binds the Kind its plural resolved to (resolveKind); the empty Kind of an
	// unresolved plural matches nothing.
	stmtSelectObjects: sqlstmt.OnReader(`
		SELECT ` + objectColumns + `
		FROM objects
		WHERE api_version = ? AND kind = ?
		ORDER BY namespace, name`),

	stmtResolveKind: sqlstmt.OnReader(`SELECT kind FROM kind_catalog WHERE api_version = ? AND resource = ?`),
	// Both ranges are index descents: objects_kind_seq and deletes_kind_seq are keyed by
	// (api_version, kind) with the position last, so the cost is the rows returned.
	// Ordered by it, so a burst of writes to one row reads as that row at its latest
	// position and the frames come out in the order the writes landed.
	stmtSelectObjectsSince: sqlstmt.OnReader(`
		SELECT ` + objectColumns + `
		FROM objects
		WHERE api_version = ? AND kind = ? AND write_seq > ?
		ORDER BY write_seq, uid`),
	stmtSelectObjectDeletesSince: sqlstmt.OnReader(`
		SELECT uid FROM deletes
		WHERE api_version = ? AND kind = ? AND seq > ? ORDER BY seq, uid`),
	stmtSelectEventsSince: sqlstmt.OnReader(`
		SELECT ` + eventColumns + `
		FROM events WHERE write_seq > ? ORDER BY write_seq, uid`),
	stmtSelectEventDeletesSince: sqlstmt.OnReader(`
		SELECT uid FROM deletes
		WHERE api_version = '` + eventsLogAPIVersion + `' AND kind = '` + eventsLogKind + `'
		  AND seq > ? ORDER BY seq, uid`),
}

// The column lists the identity reads share, in the order scanObjects and scanEvents scan.
const (
	objectColumns = `uid, api_version, kind, namespace, name, resource_version, created_at`
	eventColumns  = `uid,
	       COALESCE(type, ''), COALESCE(reason, ''), COALESCE(message, ''),
	       COALESCE(count, 0), COALESCE(first_seen, 0), COALESCE(last_seen, 0),
	       COALESCE(involved_kind, ''), COALESCE(involved_ns, ''), COALESCE(involved_name, '')`
)

// stmts issues the file's prepared statements: a delta writes straight on the writer, a
// relist page inside its transaction, a read that pairs rows with a position inside one on
// the reader, and one helper serves all three.
type stmts = sqlstmt.Stmts[stmtID]

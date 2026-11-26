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
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The objects table's write path: how one body becomes a row, and the statements that
// land it. Every statement is kind-scoped — the table is shared by every kind a cache
// mirrors, and concurrent workers touch disjoint rows.

// redactedValue replaces a credential at write time, so the cache file never holds one.
// What it replaces is named by the redactions table below.
const redactedValue = "[redacted]"

// objectRow is one row of the objects table, flattened from an object body.
type objectRow struct {
	UID             string
	Namespace       string
	Name            string
	ResourceVersion string
	Generation      int64
	CreatedAt       int64
	RawJSON         []byte
	OwnerRefs       []ownerRef
	Labels          map[string]string
	// Pod says the body is a core Pod, whose containers are written with it.
	Pod        bool
	Containers []containerRow
	// Referring says the body's kind can make references, which are written with it.
	Referring bool
	Refs      []refRow
	// Selecting says the body's kind can carry a Pod selector, written with it; Selector is
	// the one it carries, nil for none.
	Selecting bool
	Selector  *selectorRow
	// status holds the materialized status columns — see status.go.
	status statusReading
}

type ownerRef struct {
	UID          string
	IsController bool
}

// projectObject flattens an object body into the objects columns, redacting and
// stripping the body along the way.
// errUnprojectable marks a body the projection cannot turn into a row. Every writer skips one
// rather than failing over it: a body a relist drops is simply missing until the next one, where
// a delta the server replays from its position would be handed back forever.
var errUnprojectable = errors.New("kubestore: unprojectable body")

func projectObject(u *unstructured.Unstructured) (objectRow, error) {
	// A nil body would panic on GetUID inside a worker goroutine, taking the process
	// with it.
	if u == nil || u.Object == nil {
		return objectRow{}, fmt.Errorf("%w: object is empty", errUnprojectable)
	}
	uid := string(u.GetUID())
	if uid == "" {
		return objectRow{}, fmt.Errorf("%w: %s has empty UID", errUnprojectable, u.GetKind())
	}

	// Every side table read from the body is read from the sanitized copy: a table a query
	// reads must never hold what redaction removed.
	sanitized := sanitize(u)
	rawJSON, err := json.Marshal(sanitized.Object)
	if err != nil {
		return objectRow{}, fmt.Errorf("%w: %w", errUnprojectable, err)
	}

	row := objectRow{
		UID:             uid,
		Namespace:       u.GetNamespace(),
		Name:            u.GetName(),
		ResourceVersion: u.GetResourceVersion(),
		Generation:      u.GetGeneration(),
		RawJSON:         rawJSON,
		Labels:          u.GetLabels(),
		Pod:             isCorePod(u),
		Containers:      containersOf(sanitized),
		Referring:       refersByKind(u),
		Refs:            refsOf(sanitized),
		Selecting:       selectsByKind(u),
	}
	if sel, ok := selectorOf(sanitized); ok {
		row.Selector = &sel
	}
	if ts := u.GetCreationTimestamp(); !ts.IsZero() {
		row.CreatedAt = ts.UnixMilli()
	}
	// Read the ORIGINAL body, not the sanitized copy, whose redaction and strips could
	// drop a field a reading depends on.
	row.status = extractStatus(u)
	for _, ref := range u.GetOwnerReferences() {
		if ref.UID == "" {
			continue
		}
		row.OwnerRefs = append(row.OwnerRefs, ownerRef{
			UID:          string(ref.UID),
			IsController: ref.Controller != nil && *ref.Controller,
		})
	}
	return row, nil
}

// sanitize returns the body as it should be stored: server noise removed, Secret values
// redacted. It deep-copies because the caller's body is the live watch object.
//
// Stripping is a real saving — managedFields plus the last-applied annotation are
// roughly half a typical object's bytes and nothing reads them. Reads are then pure
// pass-through, which is what lets ClusterCachedDataObject serve raw_json verbatim.
func sanitize(u *unstructured.Unstructured) *unstructured.Unstructured {
	out := u.DeepCopy()
	unstructured.RemoveNestedField(out.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(out.Object, "metadata", "annotations",
		"kubectl.kubernetes.io/last-applied-configuration")
	// An annotations map emptied by that removal is noise of its own.
	if ann, ok, _ := unstructured.NestedMap(out.Object, "metadata", "annotations"); ok && len(ann) == 0 {
		unstructured.RemoveNestedField(out.Object, "metadata", "annotations")
	}
	for _, r := range redactionsFor(out) {
		redact(out, r)
	}
	return out
}

// How one redaction treats the field it names.
const (
	// redactValue replaces a scalar, keeping the field so a reader sees it was set.
	redactValue = iota
	// redactMapValues replaces every value in a map, keeping the keys so a reader can
	// list what the object holds without seeing any of it.
	redactMapValues
	// dropField removes the field outright, for one the stored object should not claim
	// to carry at all.
	dropField
)

// redaction is one path into a body and what to do at the end of it.
type redaction struct {
	path []string
	mode int
}

// redactions names every field a mirrored body carries in the clear, keyed by api group —
// empty for the core group — and Kind. A path that is absent costs nothing, so an entry is
// cheap; what it must be is CORRECT, since redacting a field that merely sounds sensitive
// throws away the diagnostic value the mirror exists for.
//
// **This table is necessarily incomplete, and is not the only defence.** Most operators
// reference credentials rather than inlining them (a `secretRef`, a `key`/`name` selector),
// and those need no entry — but a long tail offers an inline value beside the reference, and
// no table enumerates every operator. Add an entry when a real one turns up; the shape of
// the cache does not depend on this list being exhaustive.
//
// Every entry below was read off the CRD's own schema, not inferred from the field's name.
var redactions = map[[2]string][]redaction{
	{"", "Secret"}: {
		{path: []string{"data"}, mode: redactMapValues},
		// Write-only server-side: a stored object claiming to hold it would be a lie.
		{path: []string{"stringData"}, mode: dropField},
	},
	{"cert-manager.io", "Certificate"}: {
		// "Password provides a literal password", the alternative to passwordSecretRef.
		{path: []string{"spec", "keystores", "jks", "password"}, mode: redactValue},
		{path: []string{"spec", "keystores", "pkcs12", "password"}, mode: redactValue},
	},
	{"grafana.integreatly.org", "GrafanaDatasource"}: {
		// Grafana's own split: jsonData is configuration, secureJsonData the credentials.
		{path: []string{"spec", "datasource", "secureJsonData"}, mode: redactMapValues},
	},
	{"grafana.integreatly.org", "GrafanaDashboard"}: {
		{path: []string{"spec", "publicSharing", "accessToken"}, mode: redactValue},
	},
}

// redactionsFor reads the BODY's own group and kind, not the worker's configured ones, so
// redaction cannot be bypassed by how the object was addressed.
func redactionsFor(u *unstructured.Unstructured) []redaction {
	return redactions[groupKindOf(u)]
}

// redact applies one entry.
//
// A value we cannot read is a value we cannot prove is safe. Dropping it loses a
// diagnostic; keeping it can store a credential. An err from a Nested* read means the
// path is occupied by the wrong type — not that it is missing, which stays a no-op.
func redact(u *unstructured.Unstructured, r redaction) {
	switch r.mode {
	case dropField:
		unstructured.RemoveNestedField(u.Object, r.path...)
	case redactValue:
		_, ok, err := unstructured.NestedString(u.Object, r.path...)
		switch {
		case ok:
			_ = unstructured.SetNestedField(u.Object, redactedValue, r.path...)
		case err != nil:
			unstructured.RemoveNestedField(u.Object, r.path...)
		}
	case redactMapValues:
		m, ok, err := unstructured.NestedMap(u.Object, r.path...)
		if !ok {
			if err != nil {
				unstructured.RemoveNestedField(u.Object, r.path...)
			}
			return
		}
		for k := range m {
			m[k] = redactedValue
		}
		_ = unstructured.SetNestedMap(u.Object, m, r.path...)
	}
}

// insertObjectRow upserts one object and rewrites its edges — the chokepoint both the
// watch-delta and relist-page paths route through.
//
// Edges are DELETEd then re-inserted, not upserted: an object that lost a label or an
// ownerReference must lose the row too. Both tables are uid-keyed, so it is a point
// lookup.
func insertObjectRow(ctx context.Context, st stmts, k Kind, row objectRow, stamp writeStamp) error {
	if err := recordStatusTransition(ctx, st, row, stamp.at); err != nil {
		return err
	}
	rawJSON, err := compressRaw(row.RawJSON)
	if err != nil { // coverage:ignore zlib into a bytes.Buffer cannot fail
		return err
	}
	_, err = st.Exec(ctx, stmtUpsertObject,
		row.UID, k.APIVersion, k.Kind, row.Namespace, row.Name,
		row.ResourceVersion, row.Generation, row.CreatedAt, stamp.at, rawJSON,
		nullIfEmpty(row.status.Summary), row.status.Ready, row.status.Total,
		row.status.Restart, nullIfEmpty(row.status.Host), stamp.seq, stamp.at,
	)
	if err != nil {
		return err
	}

	// At most two statements per edge table (one DELETE, one multi-row INSERT), not one
	// per row: a 500-object relist page runs in one transaction on the cache's shared
	// writer, where per-row statements would mean thousands.
	if _, err := st.Exec(ctx, stmtDeleteOwnerRefsOfChild, row.UID); err != nil {
		return err
	}
	// Guard, not an optimization: OwnerRefs is built by append, so an unowned object
	// marshals to `null`, and json_each('null') yields one all-NULL row — a NOT NULL
	// violation under STRICT. An *empty* array would expand to nothing; a nil does not.
	if len(row.OwnerRefs) > 0 {
		refs := make([][2]any, len(row.OwnerRefs))
		for i, ref := range row.OwnerRefs {
			refs[i] = [2]any{ref.UID, ref.IsController}
		}
		// Tuples, so the columns come out of the element's positions. Marshalling
		// []ownerRef instead gives an array of objects, where `value ->> 0` is NULL.
		refsJSON, err := json.Marshal(refs)
		if err != nil { // coverage:ignore pairs of a string and a bool always marshal
			return err
		}
		if _, err := st.Exec(ctx, stmtUpsertOwnerRefs, row.UID, string(refsJSON)); err != nil {
			return err
		}
	}

	if _, err := st.Exec(ctx, stmtDeleteLabelsOfObject, row.UID); err != nil {
		return err
	}
	// Same guard: an unlabelled object's Labels is the nil map apimachinery returns, and
	// it marshals to `null` exactly as the ref slice does.
	if len(row.Labels) > 0 {
		labelsJSON, err := json.Marshal(row.Labels)
		if err != nil { // coverage:ignore a map of strings always marshals
			return err
		}
		// A map marshals to an object, so stmtUpsertLabels reads json_each's own
		// key/value. `value ->> 0` here is not merely empty — it fails with "malformed JSON".
		if _, err := st.Exec(ctx, stmtUpsertLabels, row.UID, string(labelsJSON)); err != nil {
			return err
		}
	}

	// Like the containers, written only for the kinds that can have them.
	if row.Referring {
		if err := writeRefs(ctx, st, row); err != nil {
			return err
		}
	}
	if row.Selecting {
		if err := writeSelector(ctx, st, row); err != nil {
			return err
		}
	}

	if row.Pod {
		if _, err := st.Exec(ctx, stmtDeleteContainersOfPod, row.UID); err != nil {
			return err
		}
		// The same guard as the labels': no containers marshals to `null`.
		if len(row.Containers) > 0 {
			containersJSON, err := json.Marshal(row.Containers)
			if err != nil { // coverage:ignore plain values always marshal
				return err
			}
			if _, err := st.Exec(ctx, stmtInsertContainers, row.UID, string(containersJSON)); err != nil {
				return err
			}
		}
	}
	return nil
}

// recordStatusTransition appends to the status timeline only when the summary CHANGED —
// a relist rewrites every row, so an unconditional insert would bury real transitions
// under a copy of the collection each resync. The guard is a NOT EXISTS on the caller's
// transaction rather than a separate read; this is the hottest path in the store. A
// summaryless kind records nothing.
func recordStatusTransition(ctx context.Context, st stmts, row objectRow, now int64) error {
	if row.status.Summary == "" {
		return nil
	}
	_, err := st.Exec(ctx, stmtInsertStatusTransition,
		row.UID, now, row.status.Summary, row.UID, row.status.Summary)
	return err
}

// cascadeTables are the per-object side tables, as the pair of statements that clears
// one. Every deleter walks this list before the objects row, so it lives here once and no
// deleter can silently skip a table.
//
// owner_refs appears TWICE on purpose: a deleted object is both a child (references
// out) and an owner (its children's references in), and with orphan deletion the
// children outlive it, so inbound edges left behind point at a uid that is gone.
var cascadeTables = []struct {
	// byUID deletes one object's rows; byUIDs deletes every row of a bound uid list.
	byUID, byUIDs stmtID
}{
	{stmtDeleteLabelsOfObject, stmtDeleteLabelsOfObjects},
	{stmtDeleteContainersOfPod, stmtDeleteContainersOfPods},
	{stmtDeleteRefsOfObject, stmtDeleteRefsOfObjects},
	{stmtDeleteSelectorOfObject, stmtDeleteSelectorsOfObjects},
	{stmtDeleteSelectorTermsOfObject, stmtDeleteSelectorTermsOfObjects},
	{stmtDeleteOwnerRefsOfChild, stmtDeleteOwnerRefsOfChildren},
	{stmtDeleteOwnerRefsOwnedBy, stmtDeleteOwnerRefsOwnedByAny},
	{stmtDeleteStatusHistoryOfObject, stmtDeleteStatusHistoryOfObjects},
}

// deleteObjectRow removes one object and its edges; the objects delete fires the
// kind_counts trigger, keeping the per-kind tally exact.
func deleteObjectRow(ctx context.Context, st stmts, uid string, stamp writeStamp) error {
	if err := logDeletes(ctx, st, stmtLogDeleteObject, stamp, uid); err != nil {
		return err
	}
	for _, c := range cascadeTables {
		if _, err := st.Exec(ctx, c.byUID, uid); err != nil {
			return err
		}
	}
	_, err := st.Exec(ctx, stmtDeleteObject, uid)
	return err
}

// sweepObjects is the relist prune: it deletes one kind's objects left behind by the
// mark, plus their edges, and returns how many objects went.
//
// The objects delete goes first and names the uids it took, so the predicate is evaluated
// once rather than once per side table. Safe in that order because nothing references
// objects — the side tables are uid-keyed with no foreign key — and the caller's
// transaction rolls the whole sweep back on any failure.
func sweepObjects(ctx context.Context, st stmts, k Kind, mark int64, stamp writeStamp) (int, error) {
	if err := logDeletes(ctx, st, stmtLogSweepObjects, stamp, k.APIVersion, k.Kind, mark); err != nil {
		return 0, err
	}
	rows, err := st.Query(ctx, stmtSweepObjects, k.APIVersion, k.Kind, mark)
	if err != nil {
		return 0, err
	}
	// Drained to the end, and Err checked: the delete runs whether or not its rows are
	// read, so a short read yields a short uid list and orphans every side-table row of
	// the objects it never saw — silently, since the delete itself succeeded.
	uids := []string{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil { // coverage:ignore objects.uid is a STRICT table's primary key, never NULL
			rows.Close()
			return 0, err
		}
		uids = append(uids, uid)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil { // coverage:ignore RETURNING makes every change at the first step, which Query takes
		return 0, err
	}

	uidsJSON, err := json.Marshal(uids)
	if err != nil { // coverage:ignore a list of strings always marshals
		return 0, err
	}
	for _, c := range cascadeTables {
		if _, err := st.Exec(ctx, c.byUIDs, string(uidsJSON)); err != nil {
			return 0, err
		}
	}
	return len(uids), nil
}

// nullIfEmpty writes NULL for an empty string, so an absent reading reads as absence
// rather than as a value of "".
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

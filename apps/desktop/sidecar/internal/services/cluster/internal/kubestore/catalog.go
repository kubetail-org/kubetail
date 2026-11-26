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

// The kind_catalog table: what the cluster serves, as one sweep's answer reconciles it.
package kubestore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// The two scopes a kind is served at, as the column spells them.
const (
	ScopeNamespaced = "Namespaced"
	ScopeCluster    = "Cluster"
)

// KindRow is one kind_catalog row, in the table's own vocabulary: the plural and the
// scope as the column spells it, not the caller's bool.
type KindRow struct {
	APIVersion string
	Kind       string
	Resource   string
	Scope      string
	IsCRD      bool
	// PrinterColumns is a CRD's additionalPrinterColumns for this version, as stored JSON,
	// empty for a kind that declares none. **A string, not a decoded slice**: this row is the
	// kinds watch's diff value (runCachedDataWatch is [T comparable]), so a slice here would
	// not compile — and the string is what makes an edited CRD arrive as Modified. Decoded at
	// the projection boundary, cluster.toCachedDataKind.
	PrinterColumns string
	// Count is how many objects of this kind the cache holds. Written by nothing — the
	// sweep does not know it, and SyncKinds ignores it; Kinds fills it from kind_counts.
	Count int
}

// kindsFingerprintKey is the cluster_meta key the sweep's last answer is fingerprinted
// under — its own namespace beside the cookies'. An absent key is a wipe, which is the
// state a fresh file is in; no migration backfills it.
const kindsFingerprintKey = "kinds/fingerprint"

// SyncKinds reconciles the whole catalog against one sweep's answer, in one transaction.
// prune deletes the rows the answer did not carry, and is the caller's call: a sweep that
// did not reach every api group has not seen the kinds it is missing.
//
// **This table has one writer.** Rows leave it through the prune alone, so a per-kind
// teardown does not take one with it — the kind is still served, and dropping its row
// would take it out of the nav until the next sweep.
//
// **fingerprint rides the rows' own transaction**, which is what lets a reader tell a
// table this sweep wrote from one wiped under it. It names the sweep's answer, not the
// table's contents: a partial answer upserts without pruning, so the table can hold rows
// the fingerprint does not cover.
func (s *Store) SyncKinds(ctx context.Context, rows []KindRow, prune bool, fingerprint uint64) error {
	f, done, err := s.use()
	if err != nil {
		return err
	}
	defer done()
	err = f.set.InTx(ctx, func(st stmts) error {
		for _, r := range rows {
			// Delete then upsert, because the table has two unique keys and SQLite takes
			// one ON CONFLICT target: a Kind renamed under an unchanged plural conflicts on
			// the index rather than the primary key, which an upsert keyed on the latter
			// cannot resolve. This clears the rename's loser first.
			if _, err := st.Exec(ctx, stmtResolveKindRename,
				r.APIVersion, r.Resource, r.Kind); err != nil {
				return fmt.Errorf("resolve %s/%s: %w", r.APIVersion, r.Resource, err)
			}
			if _, err := st.Exec(ctx, stmtUpsertKind,
				r.APIVersion, r.Kind, r.Resource, r.Scope, r.IsCRD, nullIfEmpty(r.PrinterColumns)); err != nil {
				return fmt.Errorf("write %s/%s: %w", r.APIVersion, r.Kind, err)
			}
		}

		if prune {
			if err := pruneKinds(ctx, st, rows); err != nil {
				return fmt.Errorf("prune: %w", err)
			}
		}
		if err := setMeta(ctx, st, kindsFingerprintKey, strconv.FormatUint(fingerprint, 10)); err != nil {
			return fmt.Errorf("fingerprint: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("sync kinds: %w", err)
	}
	// Unconditionally, without asking whether the rows moved: a kind appearing is what the
	// nav is waiting for, and the reader answers a ping by re-reading and diffing, so a
	// sweep that changed nothing costs one idempotent read rather than a wrong frame.
	f.notify(KindsKey)
	return nil
}

// pruneKinds deletes every row the answer did not carry. The keep-set is built into the
// statement rather than compared row by row: a catalog is order-of-hundreds, so one pass
// over it costs less than reading it back.
func pruneKinds(ctx context.Context, st stmts, rows []KindRow) error {
	if len(rows) == 0 {
		_, err := st.Exec(ctx, stmtDeleteAllKinds)
		return err
	}
	// Tuples, so the pair comes out of the element's positions — the same shape the
	// edge-table inserts bind, and for the same reason: one argument, one statement text.
	keep := make([][2]any, len(rows))
	for i, r := range rows {
		keep[i] = [2]any{r.APIVersion, r.Kind}
	}
	keepJSON, err := json.Marshal(keep)
	if err != nil { // coverage:ignore pairs of strings always marshal
		return err
	}
	_, err = st.Exec(ctx, stmtPruneKinds, string(keepJSON))
	return err
}

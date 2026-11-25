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

// The deletes log: what a delete leaves behind for a reader that has not read since.
// A write's position is on its row, and the row is there for as long as the object is, so
// only deletes need logging.
package kubestore

import (
	"context"
	"fmt"
	"strconv"
)

// logDeletes copies the rows a delete is about to take into the log, at the transaction's
// own position. Every path calls it with its own statement, whose SELECT carries the
// delete's own predicate — so the two cannot disagree about which rows went — and always
// BEFORE the delete, while the rows are still there to copy. args are the predicate's,
// after the position and the timestamp the statement binds first.
func logDeletes(ctx context.Context, st stmts, id stmtID, stamp writeStamp, args ...any) error {
	_, err := st.Exec(ctx, id, append([]any{stamp.seq, stamp.at}, args...)...)
	return err
}

// The identity every event delete is logged under. The events table conflates core v1 and
// events.k8s.io/v1 into one row shape, so all of them roll into the one key the count
// triggers already use — which the schema spells for itself, in SQL this package cannot
// reach.
const (
	eventsLogAPIVersion = "v1"
	eventsLogKind       = "Event"
)

// deletesTrimmedKey is the cluster_meta key one kind's trim mark lives under. Per kind
// because cursors are: a single mark would have one busy kind's deletes push every quiet
// kind's cursor past it within minutes.
func deletesTrimmedKey(apiVersion, kind string) string {
	return "deletes/trimmed/" + apiVersion + "/" + kind
}

// trimmed is how far one kind's log has been trimmed — the highest position no longer in
// it. A kind that has never been trimmed reads 0: nothing above there has gone.
//
// A mark that will not parse is an error, not a zero. The mark is there because entries
// went, so reading it as "none did" would tell a reader every cursor it holds is valid —
// the silently missed delete this log exists to prevent.
func trimmed(ctx context.Context, st stmts, apiVersion, kind string) (int64, error) {
	v, ok, err := getMeta(ctx, st, deletesTrimmedKey(apiVersion, kind))
	if err != nil || !ok {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// trimDeletes drops every entry older than cutoff and records how far each kind was
// trimmed, in one transaction — a reader that takes a mark and the entries above it in one
// transaction of its own then cannot be trimmed between the two. The marks are written
// first, while the rows they are read from are still there.
func trimDeletes(ctx context.Context, f *file, cutoff int64) error {
	err := f.set.InTx(ctx, func(st stmts) error {
		if _, err := st.Exec(ctx, stmtMarkTrimmedDeletes, cutoff); err != nil {
			return fmt.Errorf("mark: %w", err)
		}
		_, err := st.Exec(ctx, stmtTrimDeletes, cutoff)
		return err
	})
	if err != nil {
		return fmt.Errorf("trim deletes: %w", err)
	}
	return nil
}

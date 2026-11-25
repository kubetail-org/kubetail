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

// The file's write counter: the position a row is stamped with, and the marks that say
// how far the deletes log has been trimmed.
package kubestore

import (
	"context"
	"fmt"
	"strconv"
)

// seqKey is the cluster_meta key the counter lives under. In the file rather than in
// memory, so there is nothing to initialize at open and nothing to reason about between
// goroutines — the writer pool's single connection serializes it.
const seqKey = "seq"

// nextSeq takes the next position. One per write transaction, taken before anything it
// stamps or logs, so rows committed together share a number.
//
// It is ours, and it is **not** resource_version: that is Kubernetes' own string from the
// cluster's etcd, neither ours nor ordered.
func nextSeq(ctx context.Context, st stmts) (int64, error) {
	var v string
	if err := st.QueryRow(ctx, stmtNextSeq).Scan(&v); err != nil {
		return 0, fmt.Errorf("take write position: %w", err)
	}
	return strconv.ParseInt(v, 10, 64)
}

// head is the counter itself — the position everything committed is at or below. A
// rolled-back transaction leaves a gap below it, which costs a reader nothing: it asks
// for positions ABOVE its cursor, and nothing is ever stamped above the head.
//
// A missing or unparseable counter is an error, not a zero. The migration seeds it, so
// absence means the file is not one we wrote — and a head of 0 says nothing has ever been
// written, which leaves a reader asking for nothing and serving a stale table forever.
func head(ctx context.Context, st stmts) (int64, error) {
	v, ok, err := getMeta(ctx, st, seqKey)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("read write position: %q is missing", seqKey)
	}
	return strconv.ParseInt(v, 10, 64)
}

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

package envvars

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// vars is a select's rows in its order, never nil, each checked as it is read:
// a row an older build kept and Check now refuses carries its Refusal.
func vars(ctx context.Context, st stmts, id stmtID, args ...any) ([]Var, error) {
	rows, err := st.Query(ctx, id, args...)
	if err != nil {
		return nil, fmt.Errorf("select variables: %w", err)
	}
	defer rows.Close()
	out := []Var{}
	for rows.Next() {
		var v Var
		if err := rows.Scan(&v.ClusterID, &v.Name, &v.Value, &v.Target); err != nil {
			return nil, fmt.Errorf("scan variable: %w", err)
		}
		if err := Check(v.Name, v.Value, v.Target); err != nil {
			v.Refusal = err.Error()
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// checkCluster refuses a cluster that is missing or marked for deletion.
// Inside the write transaction, so a write and a cluster's mark are serialized.
func checkCluster(ctx context.Context, st stmts, cluster apimeta.ClusterID) error {
	var one int
	err := st.QueryRow(ctx, stmtSelectClusterAccepts, cluster).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrClusterGone
	}
	if err != nil {
		return fmt.Errorf("select cluster: %w", err)
	}
	return nil
}

// insert adds v at the end of its scope, false when the scope already holds
// maxVars.
func insert(ctx context.Context, st stmts, v Var, at int64) (bool, error) {
	res, err := st.Exec(ctx, stmtInsert, appdb.NewID(), v.ClusterID, v.Name, v.Value, v.Target, at, maxVars)
	if err != nil {
		return false, fmt.Errorf("insert variable: %w", err)
	}
	return affected(res)
}

// update sets the value and target of the variable of v's name in its scope,
// false when the scope has none.
func update(ctx context.Context, st stmts, v Var, at int64) (bool, error) {
	res, err := st.Exec(ctx, stmtUpdate, v.Value, v.Target, at, v.ClusterID, v.Name)
	if err != nil {
		return false, fmt.Errorf("update variable: %w", err)
	}
	return affected(res)
}

// remove deletes a scope's variable, false when none has the name.
func remove(ctx context.Context, st stmts, v Var) (bool, error) {
	res, err := st.Exec(ctx, stmtDelete, v.ClusterID, v.Name)
	if err != nil {
		return false, fmt.Errorf("delete variable: %w", err)
	}
	return affected(res)
}

// affected is whether a write changed a row.
func affected(res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

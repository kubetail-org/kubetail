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

package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

func millis(t time.Time) int64      { return t.UnixMilli() }
func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// scanner is what *sql.Row and *sql.Rows share.
type scanner interface{ Scan(dest ...any) error }

func scanMemory(r scanner) (Memory, error) {
	var (
		m                  Memory
		cluster, uid, chat sql.NullString
		author             string
		created, updated   int64
	)
	if err := r.Scan(&m.ID, &cluster, &uid, &m.Name, &m.Body,
		&author, &chat, &created, &updated); err != nil {
		return Memory{}, err
	}
	if cluster.Valid {
		id := apimeta.ClusterID(cluster.String)
		m.ClusterID = &id
	}
	if uid.Valid {
		m.ServerUID = &uid.String
	}
	if chat.Valid {
		id := apimeta.ChatID(chat.String)
		m.ChatID = &id
	}
	m.WrittenBy = Author(author)
	m.CreatedAt, m.UpdatedAt = fromMillis(created), fromMillis(updated)
	return m, nil
}

// visible is every memory cluster sees, its own first, then the global ones, each
// by name.
func visible(ctx context.Context, st stmts, cluster apimeta.ClusterID) ([]Memory, error) {
	return memories(ctx, st, stmtSelectVisible, string(cluster))
}

// memories is every row the statement finds, never nil.
func memories(ctx context.Context, st stmts, id stmtID, args ...any) ([]Memory, error) {
	rows, err := st.Query(ctx, id, args...)
	if err != nil {
		return nil, fmt.Errorf("select memories: %w", err)
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// oneMemory is the one row the statement finds, nil when it finds none.
func oneMemory(ctx context.Context, st stmts, id stmtID, args ...any) (*Memory, error) {
	m, err := scanMemory(st.QueryRow(ctx, id, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select memory: %w", err)
	}
	return &m, nil
}

// scopeByName is the memory under name in one scope, a cluster's or the global
// ones for nil, and nil when there is none.
func scopeByName(ctx context.Context, st stmts, cluster *apimeta.ClusterID, name string) (*Memory, error) {
	return oneMemory(ctx, st, stmtSelectByNameInScope, scopeKey(cluster), name)
}

// memoryByID is the memory with id, nil when there is none.
func memoryByID(ctx context.Context, st stmts, id MemoryID) (*Memory, error) {
	return oneMemory(ctx, st, stmtSelectByID, string(id))
}

// exists is whether a SELECT 1 statement finds a row.
func exists(ctx context.Context, st stmts, id stmtID, args ...any) (bool, error) {
	var one int
	err := st.QueryRow(ctx, id, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("select: %w", err)
	}
	return true, nil
}

// nullable is a pointer as a column value: nil is NULL.
func nullable[T ~string](p *T) any {
	if p == nil {
		return nil
	}
	return string(*p)
}

// scopeKey is a scope as the name index spells it: a cluster's id, or "" for
// every cluster.
func scopeKey(cluster *apimeta.ClusterID) string {
	if cluster == nil {
		return ""
	}
	return string(*cluster)
}

// scopeNotes is every memory in the scope: a cluster's, or the global ones for nil.
func scopeNotes(ctx context.Context, st stmts, cluster *apimeta.ClusterID) ([]Memory, error) {
	return memories(ctx, st, stmtSelectScope, scopeKey(cluster))
}

// deleteMemory removes the row, false when there was none.
func deleteMemory(ctx context.Context, st stmts, id MemoryID) (bool, error) {
	res, err := st.Exec(ctx, stmtDelete, string(id))
	if err != nil {
		return false, fmt.Errorf("delete memory: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return false, fmt.Errorf("delete memory: %w", err)
	}
	return n > 0, nil
}

func insertMemory(ctx context.Context, st stmts, m Memory) error {
	_, err := st.Exec(ctx, stmtInsert, string(m.ID), nullable(m.ClusterID), nullable(m.ServerUID), m.Name,
		m.Body, string(m.WrittenBy), nullable(m.ChatID), millis(m.CreatedAt), millis(m.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	return nil
}

func updateMemory(ctx context.Context, st stmts, m Memory) error {
	_, err := st.Exec(ctx, stmtUpdate, nullable(m.ClusterID), nullable(m.ServerUID), m.Name,
		m.Body, string(m.WrittenBy), nullable(m.ChatID), millis(m.UpdatedAt), string(m.ID))
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	return nil
}

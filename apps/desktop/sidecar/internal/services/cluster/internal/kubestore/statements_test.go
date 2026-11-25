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
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// The pool is declared by hand beside the text, so a write filed as a read is invisible
// until SQLite answers "attempt to write a readonly database", and a read filed as a write
// queues behind the single write connection. Nothing but the text can say what a statement
// does, so the text is what this checks. No read here runs inside a write transaction, so
// none is OnBoth; adding one is a design decision, not a flag flip.
func TestEveryStatementDeclaresWhatItDoes(t *testing.T) {
	t.Parallel()

	require.Len(t, statements, numStmts)
	for _, st := range statements {
		text := strings.TrimSpace(st.Text)
		// WITH counts: a read written as a CTE is still a read, and misfiling one as a
		// write would prepare it on the writer alone and queue it behind the single
		// write connection — the queuing the reader pool exists to prevent.
		want := sqlstmt.Writer
		if strings.HasPrefix(text, "SELECT") || strings.HasPrefix(text, "WITH") {
			want = sqlstmt.Reader
		}
		assert.Equal(t, want, st.On, "declaration disagrees with the text: %s", text)
	}
}

// Beehive's rule, and the one this whole file exists to serve: a text at a call site is a
// text nothing prepared, so it is compiled on every call however constant it looks. The
// match is case-sensitive: every statement in the table is upper-case, and the helpers'
// error wraps ("delete rows: %w") open with the same verbs in lower case.
func TestNoSQLTextLivesOutsideTheTable(t *testing.T) {
	t.Parallel()

	// Statements that cannot be prepared here, each with its reason.
	exempt := map[string]string{
		"SELECT COALESCE(SUM(count), 0)": "the stats read also serves a CLOSED cache, " +
			"through a per-call read-only open that has no prepared set",
		"WITH q AS MATERIALIZED": "KubeQuery's wrap holds a statement the model wrote, " +
			"a new text on every call",
		"SELECT 1 UNION ALL SELECT 2": "KubeQuery holds it on a query connection, " +
			"which the prepared set does not reach",
	}

	names, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	sqlStart := regexp.MustCompile(`^(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") || name == "statements.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text := strings.TrimSpace(strings.Trim(lit.Value, "`\""))
			if !sqlStart.MatchString(text) {
				return true
			}
			for prefix := range exempt {
				if strings.HasPrefix(text, prefix) {
					return true
				}
			}
			t.Errorf("%s:%d: SQL text outside statements: %.60s…",
				name, fset.Position(lit.Pos()).Line, text)
			return true
		})
	}
}

// The relist prune's cascades used to re-derive the doomed set with the same subquery, so
// the predicate ran once per side table and again for the delete itself.
func TestTheSweepNamesObjectsOnce(t *testing.T) {
	t.Parallel()

	ids := []stmtID{stmtSweepObjects}
	for _, c := range cascadeTables {
		ids = append(ids, c.byUIDs)
	}

	var namesObjects int
	for _, id := range ids {
		if strings.Contains(statements[id].Text, "objects") {
			namesObjects++
		}
	}

	assert.Equal(t, 1, namesObjects, "the cascades take the uids the delete returned")
}

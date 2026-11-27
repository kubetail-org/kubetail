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

package monitor

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

// The pool is declared by hand, so a write filed as a read is invisible until SQLite
// refuses it. Nothing but the text can say what a statement does.
func TestEveryStatementDeclaresWhatItDoes(t *testing.T) {
	require.Len(t, statements, numStmts)
	for _, st := range statements {
		text := strings.TrimSpace(st.Text)
		assert.Equal(t, !strings.HasPrefix(text, "SELECT"), st.On == sqlstmt.Writer, "declaration disagrees with the text: %s", text)
	}
}

// A text at a call site is compiled on every call however constant it looks.
func TestNoSQLTextLivesOutsideStatements(t *testing.T) {
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
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if text := strings.TrimSpace(strings.Trim(lit.Value, "`\"")); sqlStart.MatchString(text) {
					t.Errorf("%s:%d: SQL text outside statements: %.60s…", name, fset.Position(lit.Pos()).Line, text)
				}
			}
			return true
		})
	}
}

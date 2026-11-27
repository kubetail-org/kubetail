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

package cluster

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// The pool is declared by hand, so a write filed as a read is invisible until SQLite
// answers "attempt to write a readonly database". Nothing but the text can say what
// a statement does, so the text is what this checks.
func TestEveryStatementDeclaresWhatItDoes(t *testing.T) {
	t.Parallel()

	require.Len(t, statements, numStmts)
	for _, st := range statements {
		text := strings.TrimSpace(st.Text)
		reads := strings.HasPrefix(text, "SELECT")
		assert.Equal(t, !reads, st.On == sqlstmt.Writer, "declaration disagrees with the text: %s", text)
	}
}

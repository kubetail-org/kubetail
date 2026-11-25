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

package realpath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mkdirs makes each of rels, slash-separated, under base and answers their
// paths.
func mkdirs(t *testing.T, base string, rels ...string) []string {
	t.Helper()
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = filepath.Join(base, filepath.FromSlash(r))
		require.NoError(t, os.MkdirAll(out[i], 0o700))
	}
	return out
}

// canonicalOf is Canonical of paths, which must not fail.
func canonicalOf(t *testing.T, paths ...string) []string {
	t.Helper()
	c, err := Canonical(paths)
	require.NoError(t, err)
	return c
}

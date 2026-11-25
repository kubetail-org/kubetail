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

//go:build !windows

package fileguard

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// A name the walk found missing that becomes a closed file afterwards, here a
// hard link to ~/.netrc, is refused when the File reads it.
func TestAFileThatBecomesAClosedPathIsHidden(t *testing.T) {
	g := newGrantHome(t)
	f, err := g.fence.Walk(session.Folder{Path: g.home}, g.in("code/new"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	require.NoError(t, os.Link(g.in(".netrc"), g.in("code/new")))

	_, err = f.Lstat()
	assert.ErrorIs(t, err, ErrHidden)
	_, err = f.Open()
	assert.ErrorIs(t, err, ErrHidden)
}

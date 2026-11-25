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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// followed is Followed of p, which must not fail.
func followed(t *testing.T, p string) (string, []Link) {
	t.Helper()
	got, links, err := Followed(p)
	require.NoError(t, err)
	return got, links
}

func TestAPathWithNoLinkIsFollowedToItself(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dir := mkdirs(t, base, "a/b")[0]

	got, links := followed(t, dir)

	assert.Equal(t, dir, got)
	assert.Empty(t, links)
}

func TestALinkAtTheLastNameIsFollowedAndRecorded(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	target := mkdirs(t, base, "target")[0]
	name := filepath.Join(base, "link")
	link(t, target, name)

	got, links := followed(t, name)

	assert.Equal(t, target, got)
	assert.Equal(t, []Link{{Name: name, Parent: base}}, links)
}

func TestALinkAboveTheLastNameIsFollowed(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	target := mkdirs(t, base, "target/kubetail")[0]
	name := filepath.Join(base, "link")
	link(t, filepath.Dir(target), name)

	got, links := followed(t, filepath.Join(name, "kubetail"))

	assert.Equal(t, target, got)
	assert.Equal(t, []Link{{Name: name, Parent: base}}, links)
}

// A link met inside another link's target is recorded too, with the
// resolved folder that holds it.
func TestALinkInsideALinksTargetIsRecorded(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dirs := mkdirs(t, base, "outer", "inner")
	outer, inner := filepath.Join(base, "outer-link"), filepath.Join(dirs[0], "inner-link")
	link(t, dirs[0], outer)
	link(t, dirs[1], inner)

	got, links := followed(t, filepath.Join(outer, "inner-link"))

	assert.Equal(t, dirs[1], got)
	assert.Equal(t, []Link{{Name: outer, Parent: base}, {Name: inner, Parent: dirs[0]}}, links)
}

// A name that is missing ends the walk: the rest is kept as given.
func TestAMissingNameEndsTheWalk(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	target := mkdirs(t, base, "target")[0]
	name := filepath.Join(base, "link")
	link(t, target, name)

	got, links := followed(t, filepath.Join(name, "Missing", "kubetail"))

	assert.Equal(t, filepath.Join(target, "Missing", "kubetail"), got)
	assert.Equal(t, []Link{{Name: name, Parent: base}}, links)
}

func TestALinkWhoseTargetIsMissingIsAnError(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	target, name := filepath.Join(base, "gone"), filepath.Join(base, "link")
	link(t, target, name)

	_, _, err := Followed(filepath.Join(name, "kubetail"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), name)
	assert.Contains(t, err.Error(), target)
}

// A cycle of links stops the walk, so a start fails and does not hang.
func TestACycleOfLinksIsAnError(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	link(t, b, a)
	link(t, a, b)

	_, _, err := Followed(filepath.Join(a, "kubetail"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(a, "kubetail"))
}

func TestParentsAreEachLinksParentOnce(t *testing.T) {
	links := []Link{{Name: "/a/b", Parent: "/a"}, {Name: "/a/c", Parent: "/a"}, {Name: "/d", Parent: "/"}}

	assert.Equal(t, []string{"/a", "/"}, Parents(links))
}

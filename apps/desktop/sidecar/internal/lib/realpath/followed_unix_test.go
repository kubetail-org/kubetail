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

package realpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A relative link is followed from the folder that holds it.
func TestARelativeLinkIsFollowedFromItsFolder(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dirs := mkdirs(t, base, "a/target", "b")
	name := filepath.Join(dirs[1], "link")
	link(t, "../a/target", name)

	got, links := followed(t, filepath.Join(name, "kubetail"))

	assert.Equal(t, filepath.Join(dirs[0], "kubetail"), got)
	assert.Equal(t, []Link{{Name: name, Parent: dirs[1]}}, links)
}

// A file is not a folder to walk through: the rest is kept as given, as
// canonical keeps it.
func TestAPathThroughAFileKeepsTheRest(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, links := followed(t, filepath.Join(file, "kubetail"))

	assert.Equal(t, filepath.Join(file, "kubetail"), got)
	assert.Empty(t, links)
}

// The kernel follows a link before a .., so the walk does too.
func TestADotDotAfterALinkIsTheParentOfItsTarget(t *testing.T) {
	base := canonicalOf(t, t.TempDir())[0]
	dirs := mkdirs(t, base, "private/var", "other")
	name := filepath.Join(dirs[1], "var")
	link(t, dirs[0], name)

	got, _ := followed(t, name+"/../kubetail")

	assert.Equal(t, filepath.Join(base, "private", "kubetail"), got)
}

// A name the kernel refuses to look up, here one too long, is an error, not
// the end of the walk.
func TestANameTheKernelRefusesIsAnError(t *testing.T) {
	_, _, err := Followed(filepath.Join(t.TempDir(), strings.Repeat("x", 300), "kubetail"))

	assert.Error(t, err)
}

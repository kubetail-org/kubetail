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

package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Resolve is l with every folder's links followed (realpath.Followed), and
// each link met on the way.
func Resolve(l Layout) (Layout, []realpath.Link, error) {
	var links []realpath.Link
	for _, dir := range l.fields() {
		if *dir == "" {
			continue
		}
		resolved, met, err := realpath.Followed(*dir)
		if err != nil {
			return Layout{}, nil, err
		}
		*dir = resolved
		links = append(links, met...)
	}
	return l, links, nil
}

// ResolveRuntime is the Unix runtime folder dir under its parent's resolved
// path, and the links met above it. Its parent can be shared between users,
// any of whom could plant a link at dir, so dir itself must be a folder or
// missing.
func ResolveRuntime(dir string) (string, []realpath.Link, error) {
	info, err := os.Lstat(dir)
	switch {
	case err == nil && realpath.IsLink(info):
		return "", nil, fmt.Errorf("the runtime directory %s is a link", dir)
	case err == nil && !info.IsDir():
		return "", nil, fmt.Errorf("the runtime directory %s is not a folder", dir)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", nil, err
	}
	parent, links, err := realpath.Followed(filepath.Dir(dir))
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(parent, filepath.Base(dir)), links, nil
}

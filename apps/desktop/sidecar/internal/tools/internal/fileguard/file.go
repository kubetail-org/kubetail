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

package fileguard

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// ErrFenced is a path under Kubetail's directories, and outside the chat's
// workspace.
var ErrFenced = errors.New("files: under Kubetail's directories")

// NamedOutsideWorkspace reports whether path is under the fence by name and
// not under the chat's workspace, the fence's one opening. It touches nothing
// on disk.
func (f Fence) NamedOutsideWorkspace(dir tools.ChatDir, path string) bool {
	_, inWorkspace := Under(tools.WorkspacePath(dir), path)
	return f.Named(path) && !inWorkspace
}

// File is a file a tool changes: a name under the chat's workspace, reached
// through the workspace's root; a name in a granted folder, reached through a
// walk's handle; or a path outside Kubetail's directories.
type File struct {
	path string
	root *os.Root // nil for a path outside the workspace and a granted folder
	name string   // under root
	// hidden is the closed paths a walk found, which the file must not be;
	// none for the workspace's root.
	hidden []os.FileInfo
	// ancestorMissing is a walked name whose folder did not exist when the
	// walk ended: Lstat and Open answer ErrMissing without opening it, since
	// the root would follow a link made there since, past the walk's checks;
	// Create makes the folders on the handle and refuses a link in their way.
	ancestorMissing bool
}

// File is path as a tool changes it. A path under the workspace by name opens
// the workspace, made first when create is set; a missing one is ErrMissing. A
// path that reaches the workspace by another spelling is not under it by name,
// so the fence's check on disk refuses it with the rest of Kubetail's directories
// (ErrFenced). The caller closes the File.
func (f Fence) File(dir tools.ChatDir, path string, create bool) (File, error) {
	if name, ok := Under(tools.WorkspacePath(dir), path); ok {
		root, err := tools.OpenWorkspace(dir, create)
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, ErrMissing
		}
		if err != nil {
			return File{}, err
		}
		return File{path: path, root: root, name: name}, nil
	}
	held, err := f.Holds(path)
	if err != nil {
		return File{}, err
	}
	if held {
		return File{}, ErrFenced
	}
	return File{path: path}, nil
}

// Close closes the workspace's root, if the File holds one.
func (f File) Close() error {
	if f.root == nil {
		return nil
	}
	return f.root.Close()
}

// Lstat is Lstat or LstatIn, ErrHidden for a closed path.
func (f File) Lstat() (os.FileInfo, error) {
	if f.root == nil {
		return Lstat(f.path)
	}
	if f.ancestorMissing {
		return nil, ErrMissing
	}
	info, err := LstatIn(f.root, f.name)
	if err == nil && f.isHidden(info) {
		return nil, ErrHidden
	}
	return info, err
}

// Open is Open or OpenIn, ErrHidden for a closed path: a Never path can be a
// file.
func (f File) Open() (*os.File, error) {
	if f.root == nil {
		return Open(f.path)
	}
	if f.ancestorMissing {
		return nil, ErrMissing
	}
	file, err := OpenIn(f.root, f.name)
	if err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err != nil || f.isHidden(info) {
		_ = file.Close()
		return nil, ErrHidden
	}
	return file, nil
}

// isHidden reports whether info is one of the closed paths.
func (f File) isHidden(info os.FileInfo) bool {
	return slices.ContainsFunc(f.hidden, func(h os.FileInfo) bool { return os.SameFile(h, info) })
}

// Replaceable is Replaceable or ReplaceableIn.
func (f File) Replaceable(info os.FileInfo) error {
	if f.root == nil {
		return Replaceable(f.path, info)
	}
	return ReplaceableIn(f.root, f.name, info)
}

// Replace is Replace or ReplaceIn.
func (f File) Replace(ctx context.Context, content []byte, old os.FileInfo) error {
	if f.root == nil {
		return Replace(ctx, f.path, content, old)
	}
	return ReplaceIn(ctx, f.root, f.name, content, old)
}

// Creatable is Creatable or CreatableIn.
func (f File) Creatable() error {
	if f.root == nil {
		return Creatable(f.path)
	}
	return CreatableIn(f.root, f.name)
}

// Create is Create or CreateIn.
func (f File) Create(ctx context.Context, content []byte, umask fs.FileMode) error {
	if f.root == nil {
		return Create(ctx, f.path, content, umask)
	}
	return CreateIn(ctx, f.root, f.name, content, umask)
}

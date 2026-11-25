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
	pathpkg "path"
	"syscall"
)

// OpenFlags opens without blocking: a FIFO opened for reading otherwise waits
// for a writer, in a syscall no context reaches.
const OpenFlags = os.O_RDONLY | syscall.O_NONBLOCK

// noFollow fails the open on a link swapped in after the Lstat.
const noFollow = syscall.O_NOFOLLOW

// splitRoot is an absolute path as its volume, which Unix has none of, and the
// rest from the root's separator on.
func splitRoot(path string) (vol, rest string, err error) {
	if path == "" || path[0] != '/' {
		return "", "", ErrNotAbs
	}
	return "", path, nil
}

// native is a plain absolute path as Unix opens it: itself.
func native(path string) (string, error) { return path, nil }

// plainForm is the path cleaned, so the refusal hands back a path Abs takes.
func plainForm(_, rest string) string { return pathpkg.Clean(rest) }

// lstat is path's own info, and whether it is a link.
func lstat(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	return info, info.Mode()&os.ModeSymlink != 0, nil
}

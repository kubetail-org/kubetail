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

package bash

import (
	"os"
	"strconv"
)

// scratchDir is a throwaway chat directory for one run that belongs to no
// chat, under the cache's tmp directory and named for the sidecar's pid, so a
// sweep takes what a crash left.
type scratchDir string

// newScratchDir makes a scratchDir under tmpDir, named for kind, a plain word
// such as "probe": it becomes part of an os.MkdirTemp pattern.
func newScratchDir(tmpDir, kind string) (scratchDir, error) {
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", err
	}
	pid := os.Getpid()
	if err := holdRunLock(tmpDir, pid); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(tmpDir, strconv.Itoa(pid)+"-"+kind+"-*")
	return scratchDir(dir), err
}

func (d scratchDir) Path() string { return string(d) }

func (d scratchDir) Root(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(string(d), 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(string(d))
}

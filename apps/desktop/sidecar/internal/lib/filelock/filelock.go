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

// Package filelock takes an exclusive lock on a file, without waiting. The
// kernel drops the lock when its holder exits, a crash included, so none is
// left stale.
//
// Never delete a lock file. A second process would lock a new file under the
// same name while the first still holds the old one.
package filelock

import (
	"errors"
	"os"
)

// ErrLocked is TryLock's answer when the file is locked, by this process or
// another.
var ErrLocked = errors.New("file is locked")

// TryLock opens path, creating it if missing, and locks it. Closing the file
// releases the lock. No child process inherits it: Go opens files
// close-on-exec.
func TryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

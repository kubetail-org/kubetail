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

//go:build unix

package loginshell

import (
	"context"
	"path/filepath"
)

// accountShell is the login shell the user's account record names, never
// $SHELL, which the process may have inherited from anywhere: the record's
// shell when it names an executable file by absolute path, else the first of
// the platform's own shells that is one.
func accountShell(ctx context.Context) (string, *Fault) {
	if shell := recordShell(ctx); filepath.IsAbs(shell) && executable(shell) {
		return shell, nil
	}
	for _, shell := range defaultShells {
		if executable(shell) {
			return shell, nil
		}
	}
	return "", fault(reasonNoShell)
}

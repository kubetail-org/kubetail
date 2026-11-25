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

package main

import (
	"io/fs"
	"syscall"
)

// setOwnerOnlyUmask makes every file this process creates owner-only from birth, closing
// the window a later chmod leaves open. Inherited by child processes (kubeconfig `exec`
// credential plugins), whose own caches nothing reads across users. It returns the umask
// it replaced, the one the process started with, which a file the user asks for takes.
//
// ipc.Listen saves and restores the umask around its own bind, so it is unaffected.
func setOwnerOnlyUmask() fs.FileMode {
	return fs.FileMode(syscall.Umask(0o077))
}

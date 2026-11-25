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
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every file the sidecar creates — the SQLite caches above all — should be born 0600
// rather than chmodded there afterwards, which leaves a window at 0644. The umask it
// replaced is handed back, for the files the user asks for.
//
// syscall.Umask is process-global: no t.Parallel() here.
func TestSetOwnerOnlyUmask(t *testing.T) {
	prev := syscall.Umask(0o022)
	defer syscall.Umask(prev)

	started := setOwnerOnlyUmask()

	got := syscall.Umask(0o022)
	assert.Equal(t, 0o077, got)
	assert.Equal(t, fs.FileMode(0o022), started)
}

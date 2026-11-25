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

package bash

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A kubectl cache that cannot be opened fails the run that asks for it, never
// the tool's construction: it is opened on each call.
func TestAKubectlCacheThatCannotBeOpenedFailsTheCall(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root opens an unreadable directory")
	}
	dir := t.TempDir()
	assert.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := makeKubectlCache(dir, "7", "uid")

	assert.ErrorContains(t, err, "open the kubectl cache")
}

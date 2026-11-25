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

package appdbtest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// With no temporary directory to migrate in, the template is an error, which every
// Open then fails its test with. Serial: it sets TMPDIR, which only Unix reads.
func TestTheTemplateNeedsATemporaryDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	_, err := migrateTemplate()

	assert.Error(t, err)
}

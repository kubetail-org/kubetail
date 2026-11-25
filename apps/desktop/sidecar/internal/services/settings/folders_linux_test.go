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

package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTmpIsAFixedMountOnLinux(t *testing.T) {
	f := newGrantFixture(t)
	for _, p := range []string{"/tmp", "/tmp/x", "/proc", "/proc/1/root", "/dev", "/dev/pts"} {
		for _, write := range []bool{false, true} {
			assert.Equal(t, "fixed", ruleOf(t, CheckFolder(p, write, f.zones, nil)), p)
		}
	}
	assert.EqualError(t, CheckFolder("/proc/1/root", false, f.zones, nil),
		"Every sandboxed command has its own /proc; it cannot be granted.")
}

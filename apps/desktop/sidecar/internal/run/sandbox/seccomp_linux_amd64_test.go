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

package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

// An x32 call carries the build's architecture with the x32 bit in its
// number, so it would pass every rule below under another number: refused.
func TestTheFilterRefusesX32(t *testing.T) {
	t.Parallel()

	assert.Equal(t, errno(unix.ENOSYS), verdict(t, auditArch, x32Bit|unix.SYS_SOCKET, unix.AF_UNIX))
}

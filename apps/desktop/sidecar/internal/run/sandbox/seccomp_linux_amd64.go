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
	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// auditArch is the architecture the filter lets through.
const auditArch = unix.AUDIT_ARCH_X86_64

// x32Bit marks an x32 syscall, which reports x86-64 as its architecture.
const x32Bit = 0x40000000

// archRules refuse an x32 call, whose numbers are not the ones the filter
// reads. They run with the syscall's number loaded.
var archRules = []bpf.Instruction{
	bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: x32Bit, SkipFalse: 1},
	enosys,
}

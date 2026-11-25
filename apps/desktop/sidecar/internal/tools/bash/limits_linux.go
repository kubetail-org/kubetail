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

// limitMemory is a sandboxed process's address space: room for node's
// WebAssembly reservation and a JVM's default heap.
const limitMemory = 16 << 30

// processMargin is how many tasks a sandboxed run may start over what the
// kernel already counts against it. Linux counts every thread a task, and a
// Go program starts about one per CPU and a few more, so it grows with cpus.
func processMargin(cpus int) int {
	return max(1024, 128*cpus)
}

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

//go:build !linux

package bash

// limitMemory is 0: no macOS process can set a useful address-space limit.
const limitMemory = 0

// processMargin is how many processes a sandboxed run may start over what the
// kernel already counts against it: a shell, its tools and a parallel xargs
// are a few dozen.
func processMargin(int) int {
	return 512
}

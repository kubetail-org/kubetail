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
	"os"

	"golang.org/x/sys/unix"
)

// CountedProcesses is how many processes the kernel already counts against a
// run that starts now, with the internet or without: every process of the
// user's real uid on the machine.
func (s *Sandbox) CountedProcesses(bool) (int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.ruid", os.Getuid())
	return len(procs), err
}

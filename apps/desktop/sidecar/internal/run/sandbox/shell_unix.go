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

package sandbox

import "golang.org/x/sys/unix"

// setTimeAndFiles sets each of a's CPU and open-files limits that is set. The
// CPU limit's hard one is a.cpuGrace above its soft one. Setting the open-files
// limit also keeps exec from restoring the one the Go runtime raised.
func setTimeAndFiles(a shellArgs) error {
	if a.cpu > 0 {
		if err := setClamped(unix.RLIMIT_CPU, a.cpu, a.cpuGrace); err != nil { // coverage:ignore setClamped fails only on a resource the kernel does not know, and RLIMIT_CPU is one it knows
			return err
		}
	}
	if a.files > 0 {
		return setClamped(unix.RLIMIT_NOFILE, a.files, 0)
	}
	return nil
}

// setClamped sets resource's soft limit to value and its hard one to value
// plus grace, each lowered to the process's own hard limit, which an
// unprivileged process cannot raise: a machine whose limit is stricter stays
// stricter.
func setClamped(resource, value, grace int) error {
	l, err := clamped(resource, value, grace)
	if err != nil {
		return err
	}
	return unix.Setrlimit(resource, &l)
}

// clamped is the limit setClamped sets.
func clamped(resource, value, grace int) (unix.Rlimit, error) {
	var own unix.Rlimit
	if err := unix.Getrlimit(resource, &own); err != nil {
		return unix.Rlimit{}, err
	}
	return unix.Rlimit{Cur: min(uint64(value), own.Max), Max: min(uint64(value+grace), own.Max)}, nil
}

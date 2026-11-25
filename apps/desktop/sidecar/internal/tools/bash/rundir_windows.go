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

// sweepRunDirs has nothing to sweep: a run's directory exists only where a
// sandbox does, and Windows has none yet.
func sweepRunDirs(string) {}

// holdRunLock is never asked on Windows, for the same reason.
func holdRunLock(string, int) error { return nil }

// checkPrivate has nothing to check: the runtime directory is under the data
// directory, which the profile ACL covers.
func checkPrivate(string) error { return nil }

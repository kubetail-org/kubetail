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

//go:build !race

package testutil

import "os"

// ExitHelper ends a test binary started as a helper (a sandboxed run's
// subcommand, a child a test starts) with code. Under -race it skips the race
// detector's exit sleep (exit_race.go).
func ExitHelper(code int) { os.Exit(code) } // coverage:ignore runs only in a helper process, whose coverage no run collects

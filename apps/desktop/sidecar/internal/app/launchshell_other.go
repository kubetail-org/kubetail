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

//go:build unix && !darwin

package app

// setShellEnv does nothing off macOS. Only a macOS GUI launch is handed an
// environment that lacks what the user's shell builds; everywhere else the
// inherited one is already right.
func setShellEnv(map[string]string) {}

// skipResolution reports whether the launch resolution is skipped: with no
// sandbox, nothing reads its PATH, and setShellEnv sets nothing here. A desktop
// entry's launch then leaves the provider keys to the launch environment.
func skipResolution(sb sandboxer) bool { return sb == nil }

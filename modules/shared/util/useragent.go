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

package util

import (
	"fmt"
	"runtime"
	"strings"
)

// UserAgent generates a user-agent string for outgoing HTTP requests following the format:
//
//	kubetail/<version> (<component>; env=<env>; <os>; <arch>) Go/<go-version>
func UserAgent(version, component, env string) string {
	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]

	arch := runtime.GOARCH

	goVersion := strings.TrimPrefix(runtime.Version(), "go")

	return fmt.Sprintf("kubetail/%s (%s; env=%s; %s; %s) Go/%s",
		version, component, env, osName, arch, goVersion,
	)
}

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
//	<product>/<version> (<os>; <arch>) Go/<go-version>
func UserAgent(product, version string) string {
	osName := strings.ToUpper(runtime.GOOS[:1]) + runtime.GOOS[1:]
	goVersion := strings.TrimPrefix(runtime.Version(), "go")

	return fmt.Sprintf("%s/%s (%s; %s) Go/%s",
		product, version, osName, runtime.GOARCH, goVersion,
	)
}

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

//go:build windows

package tools

import (
	"path/filepath"
	"regexp"
	"strings"
)

// gitBashDrive is Git Bash's form of a drive path: /c or /c/…, either case.
var gitBashDrive = regexp.MustCompile(`^/([A-Za-z])(/.*)?$`)

// GitBashDrive is the native form of Git Bash's drive path p (/c/…); ok is false
// when p is not one.
func GitBashDrive(p string) (native string, ok bool) {
	m := gitBashDrive.FindStringSubmatch(p)
	if m == nil {
		return "", false
	}
	return filepath.Clean(strings.ToUpper(m[1]) + `:\` + m[2]), true
}

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

package main

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// previousTag returns the tag a release's range starts after, or "" when the
// component has none. A stable release starts after the component's last
// stable tag, so its rcs fold into it; an rc starts after the component's
// previous tag of any kind.
//
// Tags order as semver does: -rc.9 precedes -rc.10, but -rc9 follows -rc10,
// so a tenth rc needs the dot.
func previousTag(tags []string, component, tag string) (string, error) {
	v, ok := version(component, tag)
	if !ok {
		return "", fmt.Errorf("tag %q is not %s/v<major>.<minor>.<patch>[-<prerelease>]", tag, component)
	}
	stable := semver.Prerelease(v) == ""
	var prev, prevV string
	for _, t := range tags {
		tv, ok := version(component, t)
		if !ok || semver.Compare(tv, v) >= 0 {
			continue
		}
		if stable && semver.Prerelease(tv) != "" {
			continue
		}
		if prev == "" || semver.Compare(tv, prevV) > 0 {
			prev, prevV = t, tv
		}
	}
	return prev, nil
}

// version returns the tag's version when it is the component's and canonical.
func version(component, tag string) (string, bool) {
	v, ok := strings.CutPrefix(tag, component+"/")
	return v, ok && semver.IsValid(v) && v == semver.Canonical(v)
}

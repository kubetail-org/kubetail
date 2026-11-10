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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

// filtersFile holds each component's paths, beside CI's language filters.
const filtersFile = ".github/ci-config/path-filters.yml"

// componentPaths returns the globs of the component's release-<component>
// filter as git pathspecs.
func (t tool) componentPaths(ctx context.Context, component string) ([]string, error) {
	top, err := t.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(top, filtersFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var filters map[string]any
	if err := yaml.Unmarshal(data, &filters); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	key := "release-" + component
	globs, _ := filters[key].([]any)
	if len(globs) == 0 {
		return nil, fmt.Errorf("%s has no %s filter", path, key)
	}
	var paths []string
	for _, g := range globs {
		glob, ok := g.(string)
		if !ok {
			return nil, fmt.Errorf("%s: %s: %v is not a glob", path, key, g)
		}
		paths = append(paths, ":(top,glob)"+glob)
	}
	return paths, nil
}

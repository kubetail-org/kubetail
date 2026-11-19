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

// Command cicallers checks that ci.yml and coverage.yml call every area
// workflow beside them (ci-*.yml, coverage-*.yml). An area file runs on a pull
// request only when its caller calls it. `make lint-repo-yaml` runs it.
//
//	go run ./cicallers ../.github/workflows
package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// callers maps each caller to the area workflows it calls.
var callers = map[string]string{
	"ci.yml":       "ci-*.yml",
	"coverage.yml": "coverage-*.yml",
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cicallers <workflows dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir string) error {
	var errs []error
	for _, caller := range slices.Sorted(maps.Keys(callers)) {
		path := filepath.Join(dir, caller)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		areas, err := filepath.Glob(filepath.Join(dir, callers[caller]))
		if err != nil {
			return err
		}
		for i, a := range areas {
			areas[i] = filepath.Base(a)
		}
		if err := check(data, areas); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

// check returns the first area a caller does not call, or nil.
func check(data []byte, areas []string) error {
	var doc struct {
		Jobs map[string]struct {
			Uses string `yaml:"uses"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Jobs) == 0 {
		return errors.New("no jobs")
	}
	var called []string
	for _, j := range doc.Jobs {
		called = append(called, strings.TrimPrefix(j.Uses, "./.github/workflows/"))
	}
	for _, a := range areas {
		if !slices.Contains(called, a) {
			return fmt.Errorf("no job calls %s", a)
		}
	}
	return nil
}

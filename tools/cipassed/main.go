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

// Command cipassed checks the two workflows the main ruleset requires, ci.yml
// and coverage.yml. Each must call every area workflow beside it (ci-*.yml,
// coverage-*.yml), so a new area is gated as soon as it exists, and end in a
// job named `<Name> · Passed` that needs every other job and runs
// `if: always()`. `make lint-repo-yaml` runs it.
//
//	go run ./cipassed ../.github/workflows
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

// callers maps each required workflow to the area workflows it calls.
var callers = map[string]string{
	"ci.yml":       "ci-*.yml",
	"coverage.yml": "coverage-*.yml",
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cipassed <workflows dir>")
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

type job struct {
	Name  string `yaml:"name"`
	If    string `yaml:"if"`
	Uses  string `yaml:"uses"`
	Needs any    `yaml:"needs"`
}

// check returns why a caller does not call every area or end in its Passed
// job, or nil.
func check(data []byte, areas []string) error {
	var doc struct {
		// A node, since the order of the jobs matters.
		Jobs yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Jobs.Kind != yaml.MappingNode || len(doc.Jobs.Content) == 0 {
		return errors.New("no jobs")
	}
	var ids, called []string
	var last job
	for i := 0; i < len(doc.Jobs.Content); i += 2 {
		id := doc.Jobs.Content[i].Value
		var j job
		if err := doc.Jobs.Content[i+1].Decode(&j); err != nil {
			return fmt.Errorf("job %s: %w", id, err)
		}
		ids = append(ids, id)
		called = append(called, strings.TrimPrefix(j.Uses, "./.github/workflows/"))
		last = j
	}
	for _, a := range areas {
		if !slices.Contains(called, a) {
			return fmt.Errorf("no job calls %s", a)
		}
	}

	id := ids[len(ids)-1]
	if !strings.HasSuffix(last.Name, " · Passed") {
		return fmt.Errorf("the last job, %s, is not named `<Name> · Passed`", id)
	}
	if strings.TrimSpace(last.If) != "always()" {
		return fmt.Errorf("job %s does not run `if: always()`", id)
	}
	needs, err := jobIDs(last.Needs)
	if err != nil {
		return fmt.Errorf("job %s: needs: %w", id, err)
	}
	for _, other := range ids[:len(ids)-1] {
		if !slices.Contains(needs, other) {
			return fmt.Errorf("job %s does not need %s", id, other)
		}
	}
	return nil
}

// jobIDs reads `needs:`, a job id or a list of them.
func jobIDs(v any) ([]string, error) {
	switch v := v.(type) {
	case string:
		return []string{v}, nil
	case []any:
		var out []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("%v is not a job id", e)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("none")
}

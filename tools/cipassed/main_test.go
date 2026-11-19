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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	areas := []string{"ci-a.yml", "ci-b.yml"}
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"passes", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  b: {uses: ./.github/workflows/ci-b.yml}
  passed:
    name: CI · Passed
    needs: [a, b]
    if: always()
`, ""},
		{"misses an area", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  passed: {name: CI · Passed, needs: a, if: always()}
`, "no job calls ci-b.yml"},
		{"not last", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  passed: {name: CI · Passed, needs: [a, b], if: always()}
  b: {uses: ./.github/workflows/ci-b.yml}
`, "the last job, b, is not named"},
		{"misses a job", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  b: {uses: ./.github/workflows/ci-b.yml}
  passed: {name: CI · Passed, needs: [a], if: always()}
`, "does not need b"},
		{"no always", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  b: {uses: ./.github/workflows/ci-b.yml}
  passed: {name: CI · Passed, needs: [a, b]}
`, "does not run `if: always()`"},
		{"no needs", `
jobs:
  a: {uses: ./.github/workflows/ci-a.yml}
  b: {uses: ./.github/workflows/ci-b.yml}
  passed: {name: CI · Passed, if: always()}
`, "needs: none"},
		{"no jobs", `on: push`, "no jobs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := check([]byte(tt.yaml), areas)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRun_NamesTheCaller(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"ci.yml":         "jobs:\n  passed: {name: CI · Passed, needs: [], if: always()}\n",
		"ci-a.yml":       "on: workflow_call\n",
		"coverage.yml":   "jobs:\n  a: {uses: ./.github/workflows/coverage-a.yml}\n  passed: {name: Coverage · Passed, needs: a, if: always()}\n",
		"coverage-a.yml": "on: workflow_call\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := run(dir)
	if err == nil || !strings.Contains(err.Error(), "ci.yml: no job calls ci-a.yml") {
		t.Fatalf("err = %v, want ci.yml to miss ci-a.yml", err)
	}
	if strings.Contains(err.Error(), "coverage.yml") {
		t.Errorf("blames coverage.yml: %v", err)
	}
}

// The repo's own workflows pass.
func TestRun_Workflows(t *testing.T) {
	if err := run("../../.github/workflows"); err != nil {
		t.Error(err)
	}
}

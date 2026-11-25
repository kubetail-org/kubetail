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
	"reflect"
	"testing"
)

func TestReadTreeFindsEachKindOfTest(t *testing.T) {
	code, err := readTree(checkout(t, nil).root, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"TestALinkedSubfolderFailsTheStart": true,
		"TestInADependency":                 false, // under node_modules
		"TestNoSuchTest":                    false,
		"a_linked_dir_is_refused":           true,
		"no_such_fn":                        false,
		"subfolders_test.go":                true,
		"app/subfolders_test.go":            true,
		"other_test.go":                     false,
		"refuses a linked folder":           true,
		"refuses nothing":                   false,
	} {
		if got := code.hasTest(name); got != want {
			t.Errorf("hasTest(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestReadTreeFailsOnARootThatIsNotThere(t *testing.T) {
	if _, err := readTree(t.TempDir()+"/gone", ""); err == nil {
		t.Error("no error for a missing root")
	}
}

func TestHasPathTakesFilesFoldersAndPatterns(t *testing.T) {
	code, err := readTree(checkout(t, nil).root, "")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"sidecar/app/subfolders.go":          true,
		"sidecar/app":                        true,
		"sidecar/app/":                       true,
		"sidecar/app/{subfolders,layout}.go": true,
		"sidecar/app/{subfolders,gone}.go":   false,
		"sidecar/app/sub*.go":                true,
		"app/subfolders.go":                  false, // not from the root
		"sidecar/app/[":                      false, // no pattern at all
	} {
		if got := code.hasPath(path); got != want {
			t.Errorf("hasPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExpandBracesWritesOutEachAlternative(t *testing.T) {
	got := expandBraces("a/{b,c}/{d,e}.go")
	if want := []string{"a/b/d.go", "a/b/e.go", "a/c/d.go", "a/c/e.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expandBraces = %v, want %v", got, want)
	}
}

func TestReadTreeCitesTheReposSharedFolders(t *testing.T) {
	root := checkout(t, nil).root
	writeFile(t, root, "scripts/build.sh", "#!/bin/sh\n")
	repo := t.TempDir()
	writeFile(t, repo, "tools/releasenotes/check_test.go", "package main\n\nfunc TestCheckFails(t *testing.T) {}\n")
	writeFile(t, repo, "scripts/audit.sh", "#!/bin/sh\n")
	writeFile(t, repo, "cli/main.go", "package main\n")

	code, err := readTree(root, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !code.hasTest("TestCheckFails") {
		t.Error("hasTest misses a test under the repo's tools/")
	}
	if !code.hasTest("tools/releasenotes/check_test.go") {
		t.Error("hasTest misses a test file by its path from the repo's root")
	}
	for path, want := range map[string]bool{
		"tools/releasenotes":         true,
		"tools/releasenotes/gone.go": false,
		"scripts/audit.sh":           true, // the repo's
		"scripts/build.sh":           true, // the app's
		"scripts/{audit,build}.sh":   true,
		"cli/main.go":                false, // not a shared folder
		"sidecar/app/subfolders.go":  true,
	} {
		if got := code.hasPath(path); got != want {
			t.Errorf("hasPath(%q) = %v, want %v", path, got, want)
		}
	}

	if _, err := readTree(root, repo+"/gone"); err == nil {
		t.Error("no error for a repo with no shared folders")
	}
}

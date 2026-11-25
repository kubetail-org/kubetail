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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sharedDirs are the repo's folders the desktop's model may cite from the
// repo's root: the tooling the desktop shares with the other components.
var sharedDirs = []string{"tools", "scripts"}

// A tree is what the model can cite: the app's files, the repo's shared
// folders and the tests in them.
type tree struct {
	root    string
	repo    string          // the repo's root, for sharedDirs; empty for none
	files   []string        // every file, from the root, with forward slashes
	goTests map[string]bool // a Go test, fuzz, benchmark or example function
	rustFns map[string]bool // a Rust function
	tsTests string          // the text of every TypeScript test file
}

// Build outputs and dependencies: no file in them is the repository's.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, "target": true, "dist": true, ".sandbox-linux": true}

var (
	goTestRE = regexp.MustCompile(`(?m)^func ((?:Test|Fuzz|Benchmark|Example)\w*)\(`)
	rustFnRE = regexp.MustCompile(`\bfn (\w+)`)
)

func readTree(root, repo string) (tree, error) {
	t := tree{root: root, repo: repo, goTests: map[string]bool{}, rustFns: map[string]bool{}}
	var ts strings.Builder
	err := t.walk(root, "", &ts)
	for _, dir := range sharedDirs {
		if err != nil || repo == "" {
			break
		}
		err = t.walk(filepath.Join(repo, dir), dir+"/", &ts)
	}
	t.tsTests = ts.String()
	return t, err
}

// walk reads the files under dir into t, each named prefix plus its path from
// dir.
func (t *tree) walk(dir, prefix string, ts *strings.Builder) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil { // coverage:ignore WalkDir's paths are under dir
			return err
		}
		rel = prefix + filepath.ToSlash(rel)
		t.files = append(t.files, rel)

		var re *regexp.Regexp
		var into map[string]bool
		switch {
		case strings.HasSuffix(rel, "_test.go"):
			re, into = goTestRE, t.goTests
		case strings.HasSuffix(rel, ".rs"):
			re, into = rustFnRE, t.rustFns
		case strings.HasSuffix(rel, ".test.ts"), strings.HasSuffix(rel, ".test.tsx"):
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil { // coverage:ignore only an unreadable checkout fails here
			return err
		}
		if re == nil {
			ts.Write(data)
			return nil
		}
		for _, m := range re.FindAllSubmatch(data, -1) {
			into[string(m[1])] = true
		}
		return nil
	})
}

var (
	goTestNameRE = regexp.MustCompile(`^(Test|Fuzz|Benchmark|Example)\w*$`)
	rustNameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	fileNameRE   = regexp.MustCompile(`\.[a-z0-9]+$`)
)

// hasTest reports whether a citation names a test: a Go test function, a Rust
// function, a file by the end of its path, or a TypeScript test by its title.
func (t tree) hasTest(name string) bool {
	switch {
	case goTestNameRE.MatchString(name):
		return t.goTests[name]
	case rustNameRE.MatchString(name):
		return t.rustFns[name]
	case fileNameRE.MatchString(name) && !strings.Contains(name, " "):
		for _, f := range t.files {
			if f == name || strings.HasSuffix(f, "/"+name) {
				return true
			}
		}
		return false
	}
	return strings.Contains(t.tsTests, name)
}

// hasPath reports whether a path written from the root, or else from the
// repo's root under one of sharedDirs, names a file or a folder. A path may
// hold {a,b} alternatives and * wildcards.
func (t tree) hasPath(path string) bool {
	for _, p := range expandBraces(path) {
		if !t.glob(t.root, p) && !(t.shared(p) && t.glob(t.repo, p)) {
			return false
		}
	}
	return true
}

func (t tree) glob(dir, path string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(path, "/"))))
	return err == nil && len(matches) > 0
}

// shared reports whether a path lies in one of the repo's sharedDirs.
func (t tree) shared(path string) bool {
	for _, dir := range sharedDirs {
		if t.repo != "" && strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

var braceRE = regexp.MustCompile(`\{([^{}]*)\}`)

// expandBraces writes out a path's {a,b} alternatives.
func expandBraces(path string) []string {
	loc := braceRE.FindStringSubmatchIndex(path)
	if loc == nil {
		return []string{path}
	}
	var out []string
	for alt := range strings.SplitSeq(path[loc[2]:loc[3]], ",") {
		out = append(out, expandBraces(path[:loc[0]]+alt+path[loc[1]:])...)
	}
	return out
}

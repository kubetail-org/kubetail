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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/cover"

	"github.com/kubetail-org/kubetail/tools/internal/gomodules"
)

// readGo adds the Go profile's blocks to cov. A block measures every line it
// spans, so a line two blocks share, one run and one not, is partly covered.
func readGo(cov coverage, root, dir, profile string) error {
	mods, err := gomodules.Load(filepath.Join(root, dir))
	if err != nil {
		return err
	}
	profiles, err := cover.ParseProfiles(profile)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		onDisk, err := mods.File(p.FileName)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, onDisk)
		if err != nil { // coverage:ignore both paths are under root
			return err
		}
		file := filepath.ToSlash(rel)
		for _, b := range p.Blocks {
			for line := b.StartLine; line <= b.EndLine; line++ {
				cov.add(file, line, b.Count > 0)
			}
		}
	}
	return nil
}

// readLcov adds the lcov report's DA records to cov, and marks a run line
// partly covered when one of its branches (BRDA) was never taken.
func readLcov(cov coverage, root, report string) error {
	f, err := os.Open(report)
	if err != nil {
		return err
	}
	defer f.Close()

	file := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		kind, value, _ := strings.Cut(line, ":")
		switch kind {
		case "SF":
			if file, err = relative(root, value); err != nil {
				return err
			}
		case "DA": // DA:<line>,<count>[,<checksum>]
			at, rest, err := record(line, value)
			if err != nil {
				return err
			}
			count, _, _ := strings.Cut(rest, ",")
			cov.add(file, at, count != "0")
		case "BRDA": // BRDA:<line>,<block>,<branch>,<taken>; taken is "-" when the line never ran
			at, rest, err := record(line, value)
			if err != nil {
				return err
			}
			taken := rest[strings.LastIndex(rest, ",")+1:]
			if taken == "-" || taken == "0" {
				cov.add(file, at, false)
			}
		}
	}
	return sc.Err()
}

// record splits a value of the form <number>,<rest>.
func record(line, value string) (int, string, error) {
	first, rest, _ := strings.Cut(value, ",")
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0, "", fmt.Errorf("%q: %w", line, err)
	}
	return n, rest, nil
}

// relative maps a report's path to one relative to root, with forward slashes.
func relative(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path)), nil
	}
	abs, err := filepath.Abs(root)
	if err != nil { // coverage:ignore only a missing working directory fails here
		return "", err
	}
	rel, err := filepath.Rel(abs, path)
	if err != nil { // coverage:ignore only a path on another Windows volume fails here
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside %s", path, abs)
	}
	return filepath.ToSlash(rel), nil
}

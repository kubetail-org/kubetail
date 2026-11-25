package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/cover"
)

// readGo adds the Go profile's blocks to cov. A block measures every line it
// spans, so a line two blocks share, one run and one not, is partly covered.
func readGo(cov coverage, root, dir, profile string) error {
	data, err := os.ReadFile(filepath.Join(root, dir, "go.mod"))
	if err != nil {
		return err
	}
	module := modfile.ModulePath(data)
	if module == "" {
		return fmt.Errorf("%s: no module line", filepath.Join(root, dir, "go.mod"))
	}
	profiles, err := cover.ParseProfiles(profile)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		rel, ok := strings.CutPrefix(p.FileName, module+"/")
		if !ok {
			return fmt.Errorf("%s is outside module %s", p.FileName, module)
		}
		file := path.Join(filepath.ToSlash(dir), rel)
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

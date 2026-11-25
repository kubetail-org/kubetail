//go:build ignore

// Sets every tracked file's modification time to the time of the commit that
// last changed it, and every directory's to the newest file under it. Run from
// the repo root after a checkout, before the Go tests:
//
//	go run scripts/restore-mtime.go
//
// Go's test cache keys a test on the files it opened by size and modification
// time, and a checkout stamps every file with the current time, so a test that
// reads a repo file would never replay from a restored cache. Commit times are
// the same on every run and move when a file changes.
//
// Only the first-parent chain is walked, with each merge diffed against its
// first parent: a PR's merge commit then dates every file the PR changed,
// including a file both sides changed, whose merged content no single parent
// holds. It is Go rather than shell so it runs on the Windows runners too.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "restore-mtime:", err)
		os.Exit(1)
	}
}

func run() error {
	// A shallow clone dates every file at its oldest commit, which is wrong
	// rather than missing, so it is refused.
	shallow, err := git("rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(shallow)) == "true" {
		return fmt.Errorf("the clone is shallow; check out with fetch-depth: 0")
	}

	tracked, err := git("ls-files", "-z")
	if err != nil {
		return err
	}
	pending := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSuffix(string(tracked), "\x00"), "\x00") {
		pending[f] = true
	}

	// Each commit is a NUL, its committer time, then the paths it changed, so a
	// path can never be read as a time.
	log, err := git("-c", "core.quotePath=false", "log", "--first-parent", "--diff-merges=first-parent",
		"--no-renames", "--name-only", "--format=%x00%ct")
	if err != nil {
		return err
	}
	files := map[string]time.Time{}
	var at time.Time
	sc := bufio.NewScanner(bytes.NewReader(log))
	for sc.Scan() {
		line := sc.Text()
		if ts, ok := strings.CutPrefix(line, "\x00"); ok {
			sec, err := strconv.ParseInt(ts, 10, 64)
			if err != nil {
				return fmt.Errorf("reading git log: %w", err)
			}
			at = time.Unix(sec, 0)
			continue
		}
		// The log is newest first, so a path's first commit is its last change.
		if pending[line] {
			files[line] = at
			delete(pending, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d tracked files have no commit in the history", len(pending))
	}

	dirs := map[string]time.Time{}
	for f, t := range files {
		// A symlink's time is its target's, and a submodule is a directory.
		info, err := os.Lstat(f)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Chtimes(f, t, t); err != nil {
			return err
		}
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			if t.After(dirs[d]) {
				dirs[d] = t
			}
		}
	}
	// A test that lists a directory is keyed on the directory's time too.
	for d, t := range dirs {
		if err := os.Chtimes(d, t, t); err != nil {
			return err
		}
	}
	return nil
}

func git(args ...string) ([]byte, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

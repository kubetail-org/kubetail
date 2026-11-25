package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	idRE   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(/[a-z0-9]+(-[a-z0-9]+)*)+$`)
	nodeRE = regexp.MustCompile(`^\d+:\d+$`)
	blobRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

func (t tool) checkCmd(out io.Writer) error {
	m, data, err := t.loadManifest()
	if err != nil {
		return err
	}
	problems := t.check(m)
	if !bytes.Equal(data, encode(m)) {
		problems = append(problems, manifestPath+" is not in the tool's format: run `designsync format`")
	}
	for _, p := range problems {
		fmt.Fprintln(out, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problems in %s", len(problems), manifestPath)
	}
	fmt.Fprintf(out, "%s: %d components and %d screens pass\n", manifestPath, len(m.Components), len(m.Screens))
	return nil
}

// check lists what is wrong with the manifest: what its schema forbids, an id
// or node used twice, and a source whose file or symbol is not there.
func (t tool) check(m manifest) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	if m.Version != 1 {
		add("version is %d, want 1", m.Version)
	}

	ids, nodes := map[string]bool{}, map[string]string{}
	lists := []struct {
		name    string
		entries []component
	}{{"components", m.Components}, {"screens", m.Screens}}
	for _, list := range lists {
		screen := list.name == "screens"
		for i, c := range list.entries {
			if !idRE.MatchString(c.ID) {
				add("%s[%d]: id %q is not kebab-case segments joined by /", list.name, i, c.ID)
				continue
			}
			if screen != strings.HasPrefix(c.ID, screenPrefix) {
				add("%s: a screen's id starts with %s, and no component's does", c.ID, screenPrefix)
			}
			if ids[c.ID] {
				add("%s: id is used twice", c.ID)
			}
			ids[c.ID] = true

			if !nodeRE.MatchString(c.Figma.Node) {
				add("%s: figma.node %q is not a node id", c.ID, c.Figma.Node)
			}
			if c.Figma.Name == "" {
				add("%s: figma.name is empty", c.ID)
			}
			if c.Figma.Hash != "" && !hashRE.MatchString(c.Figma.Hash) {
				add("%s: figma.hash %q is not a designsync hash", c.ID, c.Figma.Hash)
			}
			if other, ok := nodes[c.Figma.Node]; ok {
				add("%s: figma.node %s is also %s's", c.ID, c.Figma.Node, other)
			}
			nodes[c.Figma.Node] = c.ID

			for _, p := range t.checkSources(c.Code) {
				add("%s: %s", c.ID, p)
			}
		}
	}
	return problems
}

func (t tool) checkSources(sources []source) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	paths := map[string]bool{}
	for _, s := range sources {
		if paths[s.Path] {
			add("%s is listed twice", s.Path)
		}
		paths[s.Path] = true
		if !blobRE.MatchString(s.Blob) {
			add("%s: blob %q is not a git blob hash", s.Path, s.Blob)
		}
		data, err := t.read(s.Path)
		if err != nil {
			add("%v", err)
			continue
		}
		if s.Symbol != "" && !regexp.MustCompile(`\b`+regexp.QuoteMeta(s.Symbol)+`\b`).Match(data) {
			add("%s has no %s", s.Path, s.Symbol)
		}
	}
	return problems
}

package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
)

// The states a component is in, in the order status lists them.
const (
	diverged    = "diverged"
	codeAhead   = "code ahead"
	designAhead = "design ahead"
	notBuilt    = "not built"
	unmatched   = "unmatched"
	inSync      = "in sync"
)

var stateOrder = []string{diverged, codeAhead, designAhead, notBuilt, unmatched, inSync}

type row struct{ state, name, detail string }

// statusCmd prints every component and screen that is not in sync, then a
// count of each state. Without a token it reads the code side alone.
func (t tool) statusCmd(out io.Writer) error {
	m, _, err := t.loadManifest()
	if err != nil {
		return err
	}
	stamps, err := t.readDesign()
	if err != nil {
		return err
	}
	rows := t.status(m, stamps)

	slices.SortStableFunc(rows, func(a, b row) int {
		return slices.Index(stateOrder, a.state) - slices.Index(stateOrder, b.state)
	})
	counts := map[string]int{}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		counts[r.state]++
		if r.state != inSync {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", r.state, r.name, r.detail)
		}
	}
	if err := tw.Flush(); err != nil { // coverage:ignore only a failing writer fails here
		return err
	}
	var summary []string
	for _, s := range stateOrder {
		if counts[s] > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	fmt.Fprintln(out, strings.Join(summary, ", "))
	if t.design == nil {
		fmt.Fprintln(out, "Code side only: set FIGMA_TOKEN to read the design side.")
	}
	return nil
}

// status gives each manifest entry its state, then lists what the stamps and
// the manifest disagree on.
func (t tool) status(m manifest, stamps []stamped) []row {
	byID := map[string]stamped{}
	var rows []row
	for _, s := range stamps {
		at := fmt.Sprintf("%s (%s)", s.Name, s.Node)
		switch {
		case s.Invalid:
			rows = append(rows, row{unmatched, at, "its stamp is not valid JSON"})
		case s.Stamp == nil:
			rows = append(rows, row{unmatched, at, "has no stamp"})
		default:
			if other, ok := byID[s.Stamp.ID]; ok {
				rows = append(rows, row{unmatched, at, fmt.Sprintf("carries %s's stamp too: %s", other.Name, s.Stamp.ID)})
				continue
			}
			byID[s.Stamp.ID] = s
		}
	}

	for _, e := range m.entries() {
		c := *e
		moved := t.codeMoved(c)
		code, fig := len(moved) > 0, false
		if t.design != nil {
			s, ok := byID[c.ID]
			delete(byID, c.ID)
			switch {
			case !ok:
				rows = append(rows, row{unmatched, c.ID, "nothing in Figma carries its stamp"})
			case s.Node != c.Figma.Node:
				rows = append(rows, row{unmatched, c.ID, fmt.Sprintf("its stamp is on node %s, not %s", s.Node, c.Figma.Node)})
			case s.Name != c.Figma.Name:
				rows = append(rows, row{unmatched, c.ID, fmt.Sprintf("renamed in Figma: %s is now %s", c.Figma.Name, s.Name)})
			}
			if d := designMoved(c, s); d != "" {
				moved = append(moved, d)
				fig = true
			}
		}

		state := inSync
		switch {
		case !c.built():
			state = notBuilt
		case code && fig:
			state = diverged
		case fig:
			state = designAhead
		case code:
			state = codeAhead
		}
		rows = append(rows, row{state, c.ID, strings.Join(moved, ", ")})
	}

	for _, id := range slices.Sorted(maps.Keys(byID)) {
		s := byID[id]
		rows = append(rows, row{unmatched, fmt.Sprintf("%s (%s)", s.Name, s.Node), "its stamp's id is not in the manifest: " + id})
	}
	return rows
}

// codeMoved lists the sources that differ from what the entry recorded.
func (t tool) codeMoved(c component) []string {
	var moved []string
	for _, s := range c.Code {
		data, err := t.read(s.Path)
		if err != nil {
			moved = append(moved, s.Path+" is gone")
		} else if blobHash(data) != s.Blob {
			moved = append(moved, s.Path)
		}
	}
	return moved
}

// designMoved says how the design differs from what the entry recorded, or
// nothing: its stamp lists changes, or it hashes differently with none
// listed, an edit nobody wrote down. s is zero when no component carries the
// entry's stamp.
func designMoved(c component, s stamped) string {
	if s.Stamp == nil {
		return ""
	}
	switch n := len(s.Stamp.Changes); {
	case n == 1:
		return "1 design change"
	case n > 1:
		return fmt.Sprintf("%d design changes", n)
	case c.Figma.Hash != "" && s.Hash != c.Figma.Hash:
		return "edited in Figma, with no changes written on its stamp"
	}
	return ""
}

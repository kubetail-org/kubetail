package main

import (
	"fmt"
	"io"
)

// recordCmd sets the named entries' fingerprints to what the working tree
// holds: the last step of a sync, once the design and the code match. With a
// token it sets the design's too: each entry's node, name and hash.
func (t tool) recordCmd(ids []string, out io.Writer) error {
	m, _, err := t.loadManifest()
	if err != nil {
		return err
	}
	stamps, err := t.readDesign()
	if err != nil {
		return err
	}
	design := map[string]stamped{}
	for _, s := range stamps {
		if s.Stamp != nil {
			design[s.Stamp.ID] = s
		}
	}
	byID := map[string]*component{}
	for _, c := range m.entries() {
		byID[c.ID] = c
	}

	for _, id := range ids {
		c := byID[id]
		if c == nil {
			return fmt.Errorf("%s is not in %s", id, manifestPath)
		}
		if t.design != nil {
			s, ok := design[id]
			if !ok {
				return fmt.Errorf("%s: nothing in Figma carries its stamp", id)
			}
			c.Figma.Node, c.Figma.Name, c.Figma.Hash = s.Node, s.Name, s.Hash
			// A component no code draws has only its design to record.
			if !c.built() {
				continue
			}
		}
		if err := t.recordCode(c); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	if err := t.saveManifest(m); err != nil { // coverage:ignore only an unwritable checkout fails here
		return err
	}
	for _, id := range ids {
		fmt.Fprintln(out, "recorded", id)
	}
	if t.design == nil {
		fmt.Fprintln(out, "Code side only: set FIGMA_TOKEN to record the design too.")
	}
	return nil
}

// recordCode sets the entry's blobs to its files'.
func (t tool) recordCode(c *component) error {
	if !c.built() {
		return fmt.Errorf("no code draws it yet: add its code to %s first", manifestPath)
	}
	for i := range c.Code {
		data, err := t.read(c.Code[i].Path)
		if err != nil {
			return err
		}
		c.Code[i].Blob = blobHash(data)
	}
	return nil
}

package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// manifestPath is relative to the repository's root, as every path here is.
const manifestPath = "design/components.json"

// The manifest's shape is design/components.schema.json. Field order is the
// order the file is written in.
type manifest struct {
	Schema     string      `json:"$schema,omitempty"`
	Version    int         `json:"version"`
	Components []component `json:"components"`
	Screens    []component `json:"screens,omitempty"`
}

// screenPrefix opens every screen's id, and no component's.
const screenPrefix = "screens/"

// entries returns every entry, the components and then the screens.
func (m *manifest) entries() []*component {
	var all []*component
	for i := range m.Components {
		all = append(all, &m.Components[i])
	}
	for i := range m.Screens {
		all = append(all, &m.Screens[i])
	}
	return all
}

type component struct {
	ID       string    `json:"id"`
	Figma    figmaNode `json:"figma"`
	Code     []source  `json:"code,omitempty"`
	Security bool      `json:"security,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
}

// built reports whether any code draws the component.
func (c component) built() bool { return len(c.Code) > 0 }

type figmaNode struct {
	Node string `json:"node"`
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

type source struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol,omitempty"`
	Blob   string `json:"blob"`
}

// loadManifest returns the manifest and the bytes it was read from.
func (t tool) loadManifest() (manifest, []byte, error) {
	var m manifest
	data, err := t.read(manifestPath)
	if err != nil {
		return m, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	return m, data, nil
}

func (t tool) saveManifest(m manifest) error {
	return os.WriteFile(filepath.Join(t.root, manifestPath), encode(m), 0o644)
}

// encode is the one format the manifest is kept in, so a recorded fingerprint
// is the only line its diff shows.
func encode(m manifest) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil { // coverage:ignore a manifest holds only strings, numbers and booleans
		panic(err)
	}
	return buf.Bytes()
}

// read reads a file by its repository path, refusing one that leaves the root.
func (t tool) read(path string) ([]byte, error) {
	if !fs.ValidPath(path) {
		return nil, fmt.Errorf("%s: not a path inside the repository", path)
	}
	return os.ReadFile(filepath.Join(t.root, filepath.FromSlash(path)))
}

// blobHash is the hash git gives a file with these contents
// (git hash-object).
func blobHash(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (t tool) formatCmd() error {
	m, _, err := t.loadManifest()
	if err != nil {
		return err
	}
	return t.saveManifest(m)
}

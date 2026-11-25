// Package atomicjson reads and writes a JSON document crash-safely: writes go to a temp
// file in the same directory and are renamed into place, so a crash mid-write can't leave
// a torn document. A missing file reads as the zero value.
//
// NOT internally synchronized — callers with concurrent writers must serialize them; the
// temp+rename then makes every on-disk state a complete document, last writer winning.
package atomicjson

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Temp names are random per write, so concurrent Saves never collide before the rename.
const tmpPattern = ".atomicjson-*.json"

// Load unmarshals path into T. A missing file yields the zero T and nil; a corrupt one
// yields the zero T and the unmarshal error — never silently valid.
func Load[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, nil
		}
		return zero, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, err
	}
	return out, nil
}

// Save writes v to path atomically as compact JSON; the parent dir is created 0700, the
// file lands 0600.
func Save[T any](path string, v T) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return write(path, data)
}

// SaveIndent is Save for a file people read and edit: two-space indents and a trailing
// newline.
func SaveIndent[T any](path string, v T) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return write(path, append(data, '\n'))
}

func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails.
	defer func() { _ = os.Remove(tmpName) }()

	// coverage:ignore-start a file this call just created fails only on a full disk or an I/O error, which no test can force
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// coverage:ignore-stop
	return os.Rename(tmpName, path)
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

// filtersFile holds each product's paths, beside CI's language filters.
const filtersFile = ".github/ci-config/path-filters.yml"

// productPaths returns the globs of the product's release-<product> filter as
// git pathspecs.
func (t tool) productPaths(ctx context.Context, product string) ([]string, error) {
	top, err := t.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(top, filtersFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var filters map[string]any
	if err := yaml.Unmarshal(data, &filters); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	key := "release-" + product
	globs, _ := filters[key].([]any)
	if len(globs) == 0 {
		return nil, fmt.Errorf("%s has no %s filter", path, key)
	}
	var paths []string
	for _, g := range globs {
		glob, ok := g.(string)
		if !ok {
			return nil, fmt.Errorf("%s: %s: %v is not a glob", path, key, g)
		}
		paths = append(paths, ":(top,glob)"+glob)
	}
	return paths, nil
}

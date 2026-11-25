package main

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// previousTag returns the tag a release's range starts after, or "" when the
// product has none. A stable release starts after the product's last stable
// tag, so its rcs fold into it; an rc starts after the product's previous tag
// of any kind.
//
// Tags order as semver does: -rc.9 precedes -rc.10, but -rc9 follows -rc10,
// so a tenth rc needs the dot.
func previousTag(tags []string, product, tag string) (string, error) {
	v, ok := version(product, tag)
	if !ok {
		return "", fmt.Errorf("tag %q is not %s/v<major>.<minor>.<patch>[-<prerelease>]", tag, product)
	}
	stable := semver.Prerelease(v) == ""
	var prev, prevV string
	for _, t := range tags {
		tv, ok := version(product, t)
		if !ok || semver.Compare(tv, v) >= 0 {
			continue
		}
		if stable && semver.Prerelease(tv) != "" {
			continue
		}
		if prev == "" || semver.Compare(tv, prevV) > 0 {
			prev, prevV = t, tv
		}
	}
	return prev, nil
}

// version returns the tag's version when it is the product's and canonical.
func version(product, tag string) (string, bool) {
	v, ok := strings.CutPrefix(tag, product+"/")
	return v, ok && semver.IsValid(v) && v == semver.Canonical(v)
}

package main

import "testing"

func TestPreviousTag(t *testing.T) {
	tags := []string{
		"cli/v0.1.0", "cli/v0.2.0-rc.1", "cli/v0.2.0-rc.2", "cli/v0.2.0-rc.10", "cli/v0.2.0", "cli/v0.3.0-rc.1",
		"desktop/v0.2.1", "cli/v0.2", "cli/vX", "other",
	}
	for _, tc := range []struct {
		product, tag, want string
	}{
		{"cli", "cli/v0.1.0", ""},
		{"cli", "cli/v0.2.0-rc.1", "cli/v0.1.0"},
		{"cli", "cli/v0.2.0-rc.2", "cli/v0.2.0-rc.1"},
		{"cli", "cli/v0.2.0-rc.10", "cli/v0.2.0-rc.2"}, // a number after a dot compares as one
		{"cli", "cli/v0.2.0", "cli/v0.1.0"},            // a stable release folds its rcs
		{"cli", "cli/v0.3.0-rc.1", "cli/v0.2.0"},
		{"cli", "cli/v0.3.0", "cli/v0.2.0"},
		{"desktop", "desktop/v0.3.0", "desktop/v0.2.1"}, // another product's tags are not its own
		{"desktop", "desktop/v0.2.1", ""},
	} {
		got, err := previousTag(tags, tc.product, tc.tag)
		if err != nil {
			t.Fatalf("%s: %v", tc.tag, err)
		}
		if got != tc.want {
			t.Errorf("previousTag(%s) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestPreviousTagRejectsAnotherProductsOrAMalformedTag(t *testing.T) {
	for _, tag := range []string{"desktop/v0.1.0", "cli/v0.1", "cli/0.1.0", "cli/v0.1.0+build"} {
		if _, err := previousTag(nil, "cli", tag); err == nil {
			t.Errorf("previousTag(%s) accepted it", tag)
		}
	}
}

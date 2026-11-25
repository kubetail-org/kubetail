package main

import (
	"io"
	"strings"
	"testing"
)

func TestCommandRejectsBadUsage(t *testing.T) {
	tl := fixtureTool(t)
	for _, args := range [][]string{nil, {"sync"}, {"check", "x"}, {"status", "a"}, {"record"}, {"format", "x"}} {
		wantErr(t, tl.command(args, io.Discard), "usage: designsync")
	}
}

func TestCommandDispatches(t *testing.T) {
	tl := fixtureTool(t)
	for _, tc := range []struct {
		args []string
		out  string
	}{
		{[]string{"check"}, "3 components and 0 screens pass"},
		{[]string{"status"}, "Code side only"},
		{[]string{"record", "dashboard/status-pill"}, "Code side only: set FIGMA_TOKEN to record the design too"},
		{[]string{"format"}, ""},
	} {
		var out strings.Builder
		if err := tl.command(tc.args, &out); err != nil {
			t.Errorf("%q: %v", tc.args, err)
		}
		if !strings.Contains(out.String(), tc.out) {
			t.Errorf("%q: output lacks %q:\n%s", tc.args, tc.out, out.String())
		}
	}
}

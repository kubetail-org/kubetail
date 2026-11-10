// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publishFixture's terminal answers with answer.
func publishFixture(t *testing.T, answer string) (tool, *fakeGH, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("The notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeGH{replies: map[string]string{"release edit cli/v1.0.0 --notes-file " + path + " --repo o/r": ""}}
	return tool{repo: testRepo, run: f.run, terminal: answering(answer)}, f, path
}

func TestPublishWritesOnAYes(t *testing.T) {
	for _, answer := range []string{"y\n", "YES\n", " yes "} {
		tl, f, path := publishFixture(t, answer)
		var out strings.Builder
		if err := tl.publishCmd(context.Background(), "cli/v1.0.0", path, &out); err != nil {
			t.Fatalf("%q: %v", answer, err)
		}
		if len(f.calls) != 1 {
			t.Errorf("%q: calls = %q", answer, f.calls)
		}
		if !strings.HasPrefix(out.String(), "The notes.\n\n---\nWrite these notes to the cli/v1.0.0 release on o/r? [y/N] ") {
			t.Errorf("out = %q", out.String())
		}
	}
}

func TestPublishWritesNothingOtherwise(t *testing.T) {
	ctx := context.Background()
	for _, answer := range []string{"", "\n", "n\n", "sure\n"} {
		tl, f, path := publishFixture(t, answer)
		err := tl.publishCmd(ctx, "cli/v1.0.0", path, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "nothing written") || len(f.calls) != 0 {
			t.Errorf("%q: err = %v, calls = %q", answer, err, f.calls)
		}
	}

	tl, f, path := publishFixture(t, "y\n")
	if err := tl.publishCmd(ctx, "cli/v1.0.0", path+".missing", io.Discard); err == nil {
		t.Error("published a missing file")
	}
	if err := tl.publishCmd(ctx, "cli/v2.0.0", path, io.Discard); err == nil {
		t.Error("a failed edit succeeded")
	}
	tl.terminal = func() (io.ReadCloser, error) { return nil, errors.New("no such device") }
	f.calls = nil
	err := tl.publishCmd(ctx, "cli/v1.0.0", path, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "run it in a terminal of your own") || len(f.calls) != 0 {
		t.Errorf("without a terminal: err = %v, calls = %q", err, f.calls)
	}
}

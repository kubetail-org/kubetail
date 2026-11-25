// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"io"
	"strings"
	"testing"
)

func TestCommandRunsCheckAndWrite(t *testing.T) {
	tl := checkout(t, nil)
	var out strings.Builder
	if err := tl.command([]string{"check"}, &out); err != nil {
		t.Fatalf("check: %v\n%s", err, out.String())
	}
	if want := "docs/security-model.md: 4 rows in 1 subjects pass\n"; out.String() != want {
		t.Errorf("check printed %q, want %q", out.String(), want)
	}
	if err := tl.command([]string{"write"}, io.Discard); err != nil {
		t.Errorf("write: %v", err)
	}
}

func TestCommandRefusesAnythingElse(t *testing.T) {
	for _, args := range [][]string{nil, {"status"}, {"check", "now"}} {
		wantErr(t, tool{root: t.TempDir()}.command(args, io.Discard), "usage: securitymodel")
	}
}

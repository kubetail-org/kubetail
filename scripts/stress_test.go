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

//go:build ignore

// Run from the repo root:
//
//	go test scripts/stress.go scripts/stress_test.go
package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const pkg = workspacePath + "/libs/go-common/logs"

// stream encodes events as go test -json writes them, one per line.
func stream(t *testing.T, events ...event) *strings.Reader {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return strings.NewReader(b.String())
}

func out(test, s string) event { return event{Action: "output", Package: pkg, Test: test, Output: s} }
func act(test, a string) event { return event{Action: a, Package: pkg, Test: test} }

func readStream(t *testing.T, events ...event) report {
	t.Helper()
	r, err := read(stream(t, events...), 4, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func equal[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func TestReadTestFailure(t *testing.T) {
	r := readStream(t,
		act("", "start"),
		out("", "-test.shuffle 42\n"),
		act("TestA", "run"),
		act("TestA/sub", "run"),
		act("TestA/sub", "fail"),
		act("TestA", "fail"),
		out("", "FAIL\t"+pkg+"\t0.1s\n"),
		act("", "fail"),
	)
	equal(t, r.Failures, []failure{{
		Package: pkg, Test: "TestA", CPU: 4, Seed: "42",
		Command: "go test -race -cpu 4 -shuffle=42 -count=3 ./libs/go-common/logs",
	}})
	equal(t, r.Outside, []outside{})
}

func TestReadTimeoutBlamesRunningTests(t *testing.T) {
	r := readStream(t,
		out("", "-test.shuffle 42\n"),
		act("TestA", "run"),
		out("TestA", "panic: test timed out after 90m0s\n"),
		act("", "fail"),
	)
	equal(t, len(r.Failures), 1)
	equal(t, r.Failures[0].TimedOut, true)
	equal(t, r.Outside, []outside{})
}

func TestReadCrashInRunningTest(t *testing.T) {
	r := readStream(t,
		out("", "-test.shuffle 42\n"),
		act("TestOK", "run"),
		out("TestOK", "=== RUN   TestOK\n"),
		out("TestOK", "    ok_test.go:9: noise from a test that passed\n"),
		out("TestOK", "--- PASS: TestOK (0.00s)\n"),
		act("TestOK", "pass"),
		act("TestA", "run"),
		out("TestA", "=== RUN   TestA\n"),
		act("TestA/sub", "run"),
		out("TestA/sub", "=== RUN   TestA/sub\n"),
		out("TestA/sub", "Exception 0xc0000005 0x8 0x0 0x0\n"),
		out("TestA/sub", "PC=0x0\n"),
		out("", "FAIL\t"+pkg+"\t0.2s\n"),
		act("", "fail"),
	)
	equal(t, r.Failures, []failure{})
	equal(t, r.Outside, []outside{{
		Package: pkg, CPU: 4, Seed: "42",
		Running: []string{"TestA", "TestA/sub"},
		Output:  "Exception 0xc0000005 0x8 0x0 0x0\nPC=0x0\n",
		Command: "go test -race -cpu 4 -shuffle=42 -count=3 ./libs/go-common/logs",
	}})
}

// test2json sends a test's pass only when the next test starts, so a crash
// right after a test ends is printed under that test's name.
func TestReadCrashAfterTestEnded(t *testing.T) {
	r := readStream(t,
		out("", "-test.shuffle 42\n"),
		act("TestA", "run"),
		act("TestA/sub", "run"),
		out("TestA/sub", "    --- PASS: TestA/sub (0.00s)\n"),
		out("TestA/sub", "fatal error: unknown caller pc\n"),
		act("TestA/sub", "pass"),
		act("", "fail"),
	)
	equal(t, len(r.Outside), 1)
	equal(t, r.Outside[0].Running, []string{"TestA"})
	equal(t, r.Outside[0].Output, "fatal error: unknown caller pc\n")
}

func TestReadPanicInTestMain(t *testing.T) {
	r := readStream(t,
		act("", "start"),
		out("", "panic: boom\n"),
		out("", "\n"),
		out("", "goroutine 1 [running]:\n"),
		out("", "FAIL\t"+pkg+"\t0.004s\n"),
		act("", "fail"),
	)
	equal(t, r.Outside, []outside{{
		Package: pkg, CPU: 4,
		Running: []string{},
		Output:  "panic: boom\n\ngoroutine 1 [running]:\n",
		Command: "go test -race -cpu 4 -shuffle=on -count=3 ./libs/go-common/logs",
	}})
}

func TestReadBuildFailure(t *testing.T) {
	build := pkg + " [" + pkg + ".test]"
	r := readStream(t,
		event{Action: "build-output", ImportPath: build, Output: "# " + build + "\n"},
		event{Action: "build-output", ImportPath: build, Output: "./a.go:2:12: undefined: x\n"},
		event{Action: "build-fail", ImportPath: build},
		event{Action: "build-output", ImportPath: "other [other.test]", Output: "not this one\n"},
		act("", "start"),
		out("", "FAIL\t"+pkg+" [build failed]\n"),
		event{Action: "fail", Package: pkg, FailedBuild: build},
	)
	equal(t, len(r.Outside), 1)
	equal(t, r.Outside[0].Output, "# "+build+"\n./a.go:2:12: undefined: x\n")
}

func TestReadPassingPackage(t *testing.T) {
	r := readStream(t,
		act("TestA", "run"),
		out("TestA", "some output\n"),
		act("TestA", "pass"),
		act("", "pass"),
	)
	equal(t, r, report{Failures: []failure{}, Outside: []outside{}})
}

func TestExcerpt(t *testing.T) {
	lines := func(n int, text string) *buffer {
		b := &buffer{}
		for i := range n {
			b.add(line{i, text})
		}
		return b
	}

	t.Run("bounds the lines", func(t *testing.T) {
		got, omitted := excerpt([]*buffer{lines(excerptLines+10, "x\n")})
		equal(t, got, strings.Repeat("x\n", excerptLines))
		equal(t, omitted, 10)
	})

	t.Run("bounds the bytes", func(t *testing.T) {
		long := strings.Repeat("y", 999) + "\n"
		got, omitted := excerpt([]*buffer{lines(10, long)})
		equal(t, got, strings.Repeat(long, excerptBytes/len(long)))
		equal(t, omitted, 10-excerptBytes/len(long))
	})

	t.Run("cuts a first line too long", func(t *testing.T) {
		got, omitted := excerpt([]*buffer{lines(2, strings.Repeat("z", 2*excerptBytes))})
		equal(t, got, strings.Repeat("z", excerptBytes)+"\n")
		equal(t, omitted, 1)
	})

	t.Run("merges buffers in output order", func(t *testing.T) {
		a := &buffer{lines: []line{{1, "a1\n"}, {3, "a3\n"}}}
		b := &buffer{lines: []line{{2, "b2\n"}}, more: 4}
		got, omitted := excerpt([]*buffer{a, b})
		equal(t, got, "a1\nb2\na3\n")
		equal(t, omitted, 4)
	})

	t.Run("ends with a newline", func(t *testing.T) {
		got, _ := excerpt([]*buffer{{lines: []line{{1, "no newline"}}}})
		equal(t, got, "no newline\n")
	})
}

func TestFence(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "```"},
		{"a ` b `` c", "```"},
		{"```", "````"},
		{"a ````` b", "``````"},
	} {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			equal(t, fence(tc.in), tc.want)
		})
	}
}

func TestMarkdownFencesOutput(t *testing.T) {
	md := markdown(report{Failures: []failure{}, Outside: []outside{{
		Package: pkg, Running: []string{"TestA"}, Output: "```\nescape\n", Omitted: 2, Command: "go test",
	}}})
	for _, want := range []string{
		pkg + " failed outside a test while TestA ran.",
		"````text\n```\nescape\n````\n",
		"2 more lines",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

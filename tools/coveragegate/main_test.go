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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	sha    = "head"
	runURL = "https://github.com/kubetail-org/kubetail/actions/runs/1"
)

// A fake Codecov. Each request URI answers from its queue and the last answer
// repeats; a URI with no queue is a 404. A status answer sets the code.
type fake struct {
	mu      sync.Mutex
	answers map[string][]any
	seen    []string
}

type status struct {
	code int
	body any
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	uri := r.URL.RequestURI()
	f.seen = append(f.seen, uri)
	queue, ok := f.answers[uri]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if len(queue) > 1 {
		f.answers[uri] = queue[1:]
	}
	answer := queue[0]
	if s, ok := answer.(status); ok {
		w.WriteHeader(s.code)
		answer = s.body
	}
	json.NewEncoder(w).Encode(answer)
}

// git runs git in dir, isolated from the machine's configuration.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
}

const file = "dashboard-ui/src/a, b.ts"

// repo is a git repository whose branches base and parent hold file with two
// lines, and whose branch head adds n lines above them.
func repo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q", "-b", "base")
	write("kept1\nkept2\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "branch", "parent")
	git(t, dir, "checkout", "-q", "-b", "head")
	write(strings.Repeat("added\n", n) + "kept1\nkept2\n")
	git(t, dir, "commit", "-q", "-am", "head")
	return dir
}

// gate runs the tool in dir against f with cfg's base, flags and timeout.
func gate(t *testing.T, dir string, cfg config, f *fake) (out, summary string, err error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfg.sha = sha
	cfg.run = runURL
	cfg.dir = dir
	cfg.threshold = 0.1
	cfg.interval = time.Millisecond
	if cfg.timeout == 0 {
		cfg.timeout = time.Minute
	}
	var o, s bytes.Buffer
	cc := &codecov{api: srv.URL, client: srv.Client()}
	err = run(context.Background(), cfg, cc, &o, &s)
	return o.String(), s.String(), err
}

var even = &totals{Lines: 1000, Hits: 900}

// ready is a Codecov that has processed go-linux, with even totals at the
// head and at base and parent, and rep as the head's report.
func ready(rep report) *fake {
	return &fake{answers: map[string][]any{
		"/commits/head/uploads/?page_size=100": {uploads(processed("go-linux"))},
		"/commits/head/":                       {commit{State: "complete", Parent: "parent", Totals: even}},
		"/commits/base/":                       {commit{State: "complete", Totals: even}},
		"/commits/parent/":                     {commit{State: "complete", Totals: even}},
		"/report/?sha=head":                    {rep},
	}}
}

func uploads(us ...upload) any { return map[string]any{"results": us} }

func processed(flag string) upload {
	return upload{Flags: []string{flag}, State: "processed", BuildURL: runURL}
}

// unmeasured stands in measuring's arguments for a line no report measured.
const unmeasured = -1

// measuring is the head's report for repo(t, len(coverage)): line i+1 has
// coverage[i]. The first unchanged line is measured and missed, so it is not
// the patch's.
func measuring(coverage ...int) report {
	f := reportFile{Name: file}
	for i, c := range coverage {
		if c != unmeasured {
			f.LineCoverage = append(f.LineCoverage, [2]int{i + 1, c})
		}
	}
	f.LineCoverage = append(f.LineCoverage, [2]int{len(coverage) + 1, miss})
	return report{Files: []reportFile{f}}
}

// goLinux gates a pull request into base after one upload.
var goLinux = config{base: "base", flags: []string{"go-linux"}}

func TestPassesWhenEveryChangedLineIsCovered(t *testing.T) {
	f := ready(measuring(hit, unmeasured, hit))
	out, summary, err := gate(t, repo(t, 3), goLinux, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "patch: 2 of 2 changed lines covered") {
		t.Errorf("out:\n%s", out)
	}
	if strings.Contains(out, "::error") {
		t.Errorf("annotated a covered change:\n%s", out)
	}
	if !strings.Contains(summary, "**patch**: 2 of 2") {
		t.Errorf("summary:\n%s", summary)
	}
}

func TestFailsOnAnUncoveredOrPartialChangedLine(t *testing.T) {
	out, summary, err := gate(t, repo(t, 3), goLinux, ready(measuring(hit, miss, partial)))
	if !errors.Is(err, errFailed) {
		t.Fatalf("err = %v, want errFailed\n%s", err, out)
	}
	for _, want := range []string{
		"patch: 1 of 3 changed lines covered",
		"::error file=dashboard-ui/src/a%2C b.ts,line=2,title=Uncovered line::This changed line is not covered.",
		"::error file=dashboard-ui/src/a%2C b.ts,line=3,title=Uncovered line::This changed line is partially covered.",
		"  dashboard-ui/src/a, b.ts:2 not covered",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line=4") {
		t.Errorf("annotated an unchanged line:\n%s", out)
	}
	if !strings.Contains(summary, "- `dashboard-ui/src/a, b.ts:3` partially covered") {
		t.Errorf("summary:\n%s", summary)
	}
}

func TestFailsWhenTheTotalDropsPastTheThreshold(t *testing.T) {
	// 90.00% to 89.85%: 0.15 points.
	f := ready(measuring(hit))
	f.answers["/commits/base/"] = []any{commit{State: "complete", Totals: &totals{Lines: 2000, Hits: 1800}}}
	f.answers["/commits/head/"] = []any{commit{State: "complete", Totals: &totals{Lines: 2000, Hits: 1797}}}
	out, _, err := gate(t, repo(t, 1), goLinux, f)
	if !errors.Is(err, errFailed) {
		t.Fatalf("err = %v, want errFailed\n%s", err, out)
	}
	if !strings.Contains(out, "project: 89.85% at the head, 90.00% at base base: FAILED") {
		t.Errorf("out:\n%s", out)
	}
}

func TestPassesADropWithinTheThreshold(t *testing.T) {
	// 90.00% to 89.95%: 0.05 points.
	f := ready(measuring(hit))
	f.answers["/commits/base/"] = []any{commit{State: "complete", Totals: &totals{Lines: 2000, Hits: 1800}}}
	f.answers["/commits/head/"] = []any{commit{State: "complete", Totals: &totals{Lines: 2000, Hits: 1799}}}
	out, _, err := gate(t, repo(t, 1), goLinux, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "at base base: ok") {
		t.Errorf("out:\n%s", out)
	}
}

func TestDoesNotJudgeTheProjectWithoutBaseTotals(t *testing.T) {
	f := ready(measuring(hit))
	f.answers["/commits/base/"] = []any{commit{State: "complete"}}
	out, summary, err := gate(t, repo(t, 1), goLinux, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "project: no base report") || !strings.Contains(summary, "not judged") {
		t.Errorf("out:\n%s\nsummary:\n%s", out, summary)
	}
}

func TestPercentOfAnEmptyReportIsZero(t *testing.T) {
	if got := percent(&totals{}); got != 0 {
		t.Errorf("percent = %v", got)
	}
}

func TestWaitsForEveryUploadAndTheCommit(t *testing.T) {
	pending := processed("typescript")
	pending.State = "uploaded"
	anotherRun := upload{Flags: []string{"rust"}, State: "uploaded", BuildURL: "https://github.com/kubetail-org/kubetail/actions/runs/0"}
	f := ready(measuring(hit))
	f.answers["/commits/head/uploads/?page_size=100"] = []any{
		uploads(),
		uploads(processed("go-linux")),
		uploads(processed("go-linux"), pending),
		// Another run's pending upload is not waited for.
		uploads(processed("go-linux"), processed("typescript"), anotherRun),
	}
	f.answers["/commits/head/"] = []any{commit{State: "pending"}, commit{State: "complete", Totals: even}}
	out, _, err := gate(t, repo(t, 1), config{base: "base", flags: []string{"go-linux", "typescript"}}, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	want := []string{
		"/commits/head/uploads/?page_size=100",
		"/commits/head/uploads/?page_size=100",
		"/commits/head/uploads/?page_size=100",
		"/commits/head/uploads/?page_size=100",
		"/commits/head/",
		"/commits/head/uploads/?page_size=100",
		"/commits/head/",
		"/commits/base/",
		"/report/?sha=head",
	}
	if got := strings.Join(f.seen, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	// Each reason it waited is printed once, as it changes.
	for _, line := range []string{
		"waiting: upload go-linux is not listed",
		"waiting: upload typescript is not listed",
		"waiting: upload typescript is uploaded",
		"waiting: commit is pending",
	} {
		if strings.Count(out, line+"\n") != 1 {
			t.Errorf("out has %q %d times:\n%s", line, strings.Count(out, line+"\n"), out)
		}
	}
}

func TestComparesAPushWithItsParent(t *testing.T) {
	f := ready(measuring(miss))
	delete(f.answers, "/commits/base/")
	out, _, err := gate(t, repo(t, 1), config{flags: []string{"go-linux"}}, f)
	if !errors.Is(err, errFailed) {
		t.Fatalf("err = %v, want errFailed\n%s\n%v", err, out, f.seen)
	}
	if !strings.Contains(out, "dashboard-ui/src/a, b.ts:1 not covered") || !strings.Contains(out, "at base parent: ok") {
		t.Errorf("out:\n%s", out)
	}
}

func TestWaitsForAPushsParent(t *testing.T) {
	f := ready(measuring(hit))
	f.answers["/commits/head/"] = []any{commit{State: "complete"}, commit{State: "complete", Parent: "parent", Totals: even}}
	out, _, err := gate(t, repo(t, 1), config{flags: []string{"go-linux"}}, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "waiting: Codecov has no parent for head") {
		t.Errorf("out:\n%s", out)
	}
}

func TestFailsWhenGitCannotDiff(t *testing.T) {
	f := ready(measuring(hit))
	f.answers["/commits/missing/"] = []any{commit{State: "complete", Totals: even}}
	cfg := goLinux
	cfg.base = "missing"
	_, _, err := gate(t, repo(t, 1), cfg, f)
	if err == nil || errors.Is(err, errFailed) || !strings.Contains(err.Error(), "git diff missing...head") {
		t.Fatalf("err = %v, want git's error", err)
	}
}

func TestATimeoutIsNotACoverageFailure(t *testing.T) {
	cfg := goLinux
	cfg.timeout = 20 * time.Millisecond
	out, _, err := gate(t, "", cfg, &fake{})
	if err == nil || errors.Is(err, errFailed) {
		t.Fatalf("err = %v, want a timeout", err)
	}
	for _, want := range []string{"did not finish processing head within 20ms", "not a coverage failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err lacks %q: %v", want, err)
		}
	}
	// What it waited on is in the log before the timeout's error.
	if !strings.Contains(out, "waiting: GET /commits/head/uploads/?page_size=100: 404 Not Found\n") {
		t.Errorf("out:\n%s", out)
	}
}

func TestFailsAtOnceWhenCodecovIsRateLimited(t *testing.T) {
	f := ready(measuring(hit))
	f.answers["/report/?sha=head"] = []any{status{http.StatusForbidden, map[string]string{"detail": "Github API rate limit error: Forbidden"}}}
	_, _, err := gate(t, repo(t, 1), goLinux, f)
	if err == nil || errors.Is(err, errFailed) {
		t.Fatalf("err = %v, want a rate limit", err)
	}
	for _, want := range []string{"rate-limited", "Github API rate limit error: Forbidden", "re-run the job"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err lacks %q: %v", want, err)
		}
	}
	if n := strings.Count(strings.Join(f.seen, "\n"), "/report/"); n != 1 {
		t.Errorf("requested the report %d times, want 1", n)
	}
}

func TestRetriesAnotherForbidden(t *testing.T) {
	f := ready(measuring(hit))
	f.answers["/report/?sha=head"] = []any{status{http.StatusForbidden, map[string]string{"detail": "Not yet"}}, measuring(hit)}
	out, _, err := gate(t, repo(t, 1), goLinux, f)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "waiting: GET /report/?sha=head: 403 Forbidden: Not yet\n") {
		t.Errorf("out:\n%s", out)
	}
}

func TestRetriesAFailedCommitRequest(t *testing.T) {
	for _, uri := range []string{"/commits/head/", "/commits/base/"} {
		f := ready(measuring(hit))
		f.answers[uri] = append([]any{status{http.StatusBadGateway, nil}}, f.answers[uri]...)
		out, _, err := gate(t, repo(t, 1), goLinux, f)
		if err != nil {
			t.Fatalf("%s: err = %v\n%s", uri, err, out)
		}
		if !strings.Contains(out, "waiting: GET "+uri+": 502 Bad Gateway\n") {
			t.Errorf("%s: out:\n%s", uri, out)
		}
	}
}

func TestFailsOnAnUploadCodecovCouldNotProcess(t *testing.T) {
	broken := processed("go-linux")
	broken.State = "error"
	f := &fake{answers: map[string][]any{
		"/commits/head/uploads/?page_size=100": {uploads(broken)},
	}}
	_, _, err := gate(t, "", goLinux, f)
	if err == nil || errors.Is(err, errFailed) || !strings.Contains(err.Error(), "failed to process upload go-linux") {
		t.Fatalf("err = %v", err)
	}
}

func TestFailsAtOnceWhenTheBaseHasNoReport(t *testing.T) {
	f := ready(measuring(hit))
	cfg := goLinux
	cfg.base = "wiptip"
	_, _, err := gate(t, "", cfg, f)
	if err == nil || !strings.Contains(err.Error(), "no report for base commit wiptip") {
		t.Fatalf("err = %v, want no report for wiptip", err)
	}
	if strings.Contains(err.Error(), "Codecov delay") {
		t.Errorf("blames a delay: %v", err)
	}
	if got := f.seen[len(f.seen)-1]; got != "/commits/wiptip/" || len(f.seen) != 3 {
		t.Errorf("requests = %v, want one poll ending at the base", f.seen)
	}
}

func TestFailsAtOnceWhenAPushsParentHasNoReport(t *testing.T) {
	f := ready(measuring(hit))
	delete(f.answers, "/commits/parent/")
	_, _, err := gate(t, "", config{flags: []string{"go-linux"}}, f)
	if err == nil || !strings.Contains(err.Error(), "no report for base commit parent") {
		t.Fatalf("err = %v, want no report for parent", err)
	}
}

func TestGatesNothingWhenNoJobUploaded(t *testing.T) {
	f := &fake{}
	out, _, err := gate(t, "", config{}, f)
	if err != nil || !strings.Contains(out, "nothing to gate") || len(f.seen) != 0 {
		t.Fatalf("err = %v, out = %q, requests = %v", err, out, f.seen)
	}
}

func TestEscapesAWorkflowCommandProperty(t *testing.T) {
	if got := escapeProperty("a:b,c%d\r\ne"); got != "a%3Ab%2Cc%25d%0D%0Ae" {
		t.Errorf("escapeProperty = %q", got)
	}
}

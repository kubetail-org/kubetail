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

// Command coveragegate applies the coverage gate to Codecov's merged result of
// a commit, once every upload a CI run made has been processed:
//
//   - patch: every changed line a report measured is covered;
//   - project: the total is no more than -threshold points below the base.
//
// CI's Coverage · Gate job (.github/workflows/coverage.yml) runs it after every
// coverage job, in a checkout holding both commits' history:
//
//	go run ./coveragegate -sha <head> -base <sha> -run <run url> -flags go-linux,typescript
//
// -flags names the uploads this run made; each is waited for. The changed
// lines are those `git diff <base>...<head>` adds. Codecov supplies the head's
// line coverage and both commits' totals. Without -base, as on a push, the
// base is the head's parent as Codecov records it. A base Codecov has no
// report for fails at once, since no wait gives it one. Codecov's public API
// needs no token, so a fork's pull request runs it too.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/tools/internal/unidiff"
)

type config struct {
	sha       string
	base      string // empty takes the head's parent, as on a push
	run       string // the CI run's URL, as Codecov records it on each upload
	flags     []string
	dir       string // the git checkout the diff is taken in; empty is the working directory
	threshold float64
	timeout   time.Duration
	interval  time.Duration
}

func main() {
	var cfg config
	var api, flags, summaryPath string
	flag.StringVar(&api, "api", "https://api.codecov.io/api/v2/github/kubetail-org/repos/kubetail", "Codecov API root of the repository")
	flag.StringVar(&cfg.sha, "sha", "", "head commit")
	flag.StringVar(&cfg.base, "base", "", "base commit; empty takes the head's parent")
	flag.StringVar(&cfg.run, "run", "", "URL of the CI run that made the uploads")
	flag.StringVar(&flags, "flags", "", "comma-separated flags the run uploaded")
	flag.Float64Var(&cfg.threshold, "threshold", 0.1, "points the total may fall below the base")
	flag.DurationVar(&cfg.timeout, "timeout", 15*time.Minute, "how long to wait for Codecov")
	flag.DurationVar(&cfg.interval, "interval", 15*time.Second, "time between polls")
	flag.StringVar(&summaryPath, "summary", os.Getenv("GITHUB_STEP_SUMMARY"), "file the Markdown summary is appended to")
	flag.Parse()
	if flags != "" {
		cfg.flags = strings.Split(flags, ",")
	}

	var summary io.Writer = io.Discard
	if summaryPath != "" {
		f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "coveragegate:", err)
			os.Exit(1)
		}
		defer f.Close()
		summary = f
	}

	cc := &codecov{api: api, client: http.DefaultClient}
	if err := run(context.Background(), cfg, cc, os.Stdout, summary); err != nil {
		fmt.Fprintln(os.Stderr, "coveragegate:", err)
		os.Exit(1)
	}
}

// errFailed is the gate's verdict; the lines that earned it are already printed.
var errFailed = errors.New("coverage gate failed")

func run(ctx context.Context, cfg config, cc *codecov, out, summary io.Writer) error {
	if len(cfg.flags) == 0 {
		fmt.Fprintln(out, "No coverage job uploaded a report in this run; nothing to gate.")
		return nil
	}
	cmp, err := wait(ctx, cfg, cc, out)
	if err != nil {
		return err
	}
	changed, err := changedLines(ctx, cfg.dir, cmp.base, cfg.sha)
	if err != nil {
		return err
	}
	v := judge(cmp, changed, cfg.threshold)
	v.report(out, summary)
	if v.failed {
		return errFailed
	}
	return nil
}

// Codecov's API, the parts read here.

type upload struct {
	Flags    []string `json:"flags"`
	State    string   `json:"state"`
	BuildURL string   `json:"build_url"`
}

type commit struct {
	State  string  `json:"state"`
	Parent string  `json:"parent"`
	Totals *totals `json:"totals"` // nil while the commit has no report
}

type totals struct {
	Lines int `json:"lines"`
	Hits  int `json:"hits"`
}

// A report lists each file's measured lines as [line, coverage] pairs.
type report struct {
	Files []reportFile `json:"files"`
}

type reportFile struct {
	Name         string   `json:"name"`
	LineCoverage [][2]int `json:"line_coverage"`
}

// Codecov's coverage of a line.
const (
	hit     = 0
	miss    = 1
	partial = 2
)

// A codecov reads one repository from Codecov's API.
type codecov struct {
	api    string // the repository's root, without a trailing slash
	client *http.Client
}

func (c *codecov) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return json.NewDecoder(resp.Body).Decode(v)
	}

	var body struct {
		Detail string `json:"detail"` // Codecov's reason, when it gives one
	}
	// A body that is not Codecov's JSON leaves the detail empty.
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	msg := "GET " + path + ": " + resp.Status
	if body.Detail != "" {
		msg += ": " + body.Detail
	}
	// Codecov's limit is shared by every anonymous caller, so it outlasts the wait.
	if resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(body.Detail), "rate limit") {
		return fatalError("rate-limited by Codecov (" + msg + "); " +
			"this is not a coverage failure: re-run the job once the limit resets")
	}
	return &statusError{code: resp.StatusCode, msg: msg}
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

func notFound(err error) bool {
	var s *statusError
	return errors.As(err, &s) && s.code == http.StatusNotFound
}

// A fatalError is a failure no wait changes.
type fatalError string

func (e fatalError) Error() string { return string(e) }

// A comparison is what Codecov stores of the head and the commit it is
// compared with.
type comparison struct {
	base       string
	baseTotals *totals
	headTotals *totals
	lines      map[string]map[int]int // the head's coverage of each measured line, by file
}

// wait polls until fetch has the comparison, for at most cfg.timeout, and
// prints each new reason it is still waiting.
func wait(ctx context.Context, cfg config, cc *codecov, out io.Writer) (*comparison, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	reason := "no request answered"
	for {
		cmp, err := fetch(ctx, cfg, cc)
		var fatal fatalError
		if err == nil || errors.As(err, &fatal) {
			return cmp, err
		}
		// A request the deadline cut short says nothing about Codecov.
		if ctx.Err() == nil && err.Error() != reason {
			reason = err.Error()
			fmt.Fprintln(out, "waiting:", reason)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out: Codecov did not finish processing %s within %s (%s); "+
				"this is a Codecov delay, not a coverage failure: re-run the job", cfg.sha, cfg.timeout, reason)
		case <-time.After(cfg.interval):
		}
	}
}

// fetch returns the comparison once every upload the run made is processed
// and the commit is complete. Until then its error says what Codecov is still
// working on.
func fetch(ctx context.Context, cfg config, cc *codecov) (*comparison, error) {
	if err := uploadsProcessed(ctx, cfg, cc); err != nil {
		return nil, err
	}

	var head commit
	if err := cc.get(ctx, "/commits/"+cfg.sha+"/", &head); err != nil {
		return nil, err
	}
	if head.State != "complete" {
		return nil, errors.New("commit is " + head.State)
	}

	cmp := &comparison{base: cfg.base, headTotals: head.Totals, lines: map[string]map[int]int{}}
	if cmp.base == "" {
		cmp.base = head.Parent
	}
	if cmp.base == "" {
		return nil, errors.New("Codecov has no parent for " + cfg.sha)
	}
	var base commit
	if err := cc.get(ctx, "/commits/"+cmp.base+"/", &base); err != nil {
		if notFound(err) {
			return nil, fatalError("Codecov has no report for base commit " + cmp.base +
				", so it cannot compare this commit with it. " +
				"Only CI's push runs (main and wip/*) upload a base's coverage: " +
				"re-run this job once the base's run has uploaded")
		}
		return nil, err
	}
	cmp.baseTotals = base.Totals

	var rep report
	if err := cc.get(ctx, "/report/?"+url.Values{"sha": {cfg.sha}}.Encode(), &rep); err != nil {
		return nil, err
	}
	for _, f := range rep.Files {
		lines := map[int]int{}
		for _, lc := range f.LineCoverage {
			lines[lc[0]] = lc[1]
		}
		cmp.lines[f.Name] = lines
	}
	return cmp, nil
}

// uploadsProcessed is nil once Codecov has processed every flag in cfg.flags
// from this run. Another run's uploads are not waited for.
func uploadsProcessed(ctx context.Context, cfg config, cc *codecov) error {
	var page struct {
		Results []upload `json:"results"`
	}
	if err := cc.get(ctx, "/commits/"+cfg.sha+"/uploads/?page_size=100", &page); err != nil {
		return err
	}
	processed := map[string]bool{}
	for _, u := range page.Results {
		if u.BuildURL != cfg.run {
			continue
		}
		name := strings.Join(u.Flags, ",")
		switch u.State {
		case "error":
			return fatalError("Codecov failed to process upload " + name + "; re-run its coverage job")
		case "uploaded", "started":
			return fmt.Errorf("upload %s is %s", name, u.State)
		}
		for _, f := range u.Flags {
			processed[f] = true
		}
	}
	for _, f := range cfg.flags {
		if !processed[f] {
			return errors.New("upload " + f + " is not listed")
		}
	}
	return nil
}

// changedLines returns the lines `git diff base...head` adds in dir, by file.
// The options fix the diff's format whatever git's configuration.
func changedLines(ctx context.Context, dir, base, head string) (map[string][]int, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.quotePath=false", "diff",
		"-U0", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", base+"..."+head)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	diff, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s...%s: %v: %s", base, head, err, strings.TrimSpace(stderr.String()))
	}
	return unidiff.Added(bytes.NewReader(diff))
}

type uncovered struct {
	file  string
	line  int
	state string // "not covered" or "partially covered"
}

// A verdict is the gate's reading of a comparison, worded for the report.
type verdict struct {
	patch     string
	uncovered []uncovered
	project   string
	failed    bool
}

func judge(cmp *comparison, changed map[string][]int, threshold float64) verdict {
	var v verdict

	measured := 0
	for _, file := range slices.Sorted(maps.Keys(changed)) {
		for _, line := range changed[file] {
			coverage, ok := cmp.lines[file][line]
			if !ok {
				continue
			}
			measured++
			switch coverage {
			case miss:
				v.uncovered = append(v.uncovered, uncovered{file, line, "not covered"})
			case partial:
				v.uncovered = append(v.uncovered, uncovered{file, line, "partially covered"})
			}
		}
	}
	v.patch = fmt.Sprintf("%d of %d changed lines covered", measured-len(v.uncovered), measured)
	v.failed = len(v.uncovered) > 0

	base, head := cmp.baseTotals, cmp.headTotals
	if base == nil || head == nil {
		v.project = "no base report to compare against; not judged"
		return v
	}
	// Codecov's percentages are rounded; these are not.
	outcome := "ok"
	if percent(head) < percent(base)-threshold {
		outcome = fmt.Sprintf("FAILED: more than %.2f points below the base", threshold)
		v.failed = true
	}
	v.project = fmt.Sprintf("%.2f%% at the head, %.2f%% at base %.7s: %s",
		percent(head), percent(base), cmp.base, outcome)
	return v
}

func percent(t *totals) float64 {
	if t.Lines == 0 {
		return 0
	}
	return 100 * float64(t.Hits) / float64(t.Lines)
}

// report writes the log, with an annotation per uncovered line, and the
// Markdown step summary.
func (v verdict) report(out, summary io.Writer) {
	fmt.Fprintln(out, "patch:", v.patch)
	for _, u := range v.uncovered {
		fmt.Fprintf(out, "::error file=%s,line=%d,title=Uncovered line::This changed line is %s.\n",
			escapeProperty(u.file), u.line, u.state)
		fmt.Fprintf(out, "  %s:%d %s\n", u.file, u.line, u.state)
	}
	fmt.Fprintln(out, "project:", v.project)

	fmt.Fprintf(summary, "## Coverage gate\n\n**patch**: %s\n\n", v.patch)
	for _, u := range v.uncovered {
		fmt.Fprintf(summary, "- `%s:%d` %s\n", u.file, u.line, u.state)
	}
	if len(v.uncovered) > 0 {
		fmt.Fprintln(summary)
	}
	fmt.Fprintf(summary, "**project**: %s\n", v.project)
}

// escapeProperty escapes a workflow command's property as GitHub's toolkit does.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

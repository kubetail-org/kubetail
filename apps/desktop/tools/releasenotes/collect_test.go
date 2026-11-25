package main

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

func prs(c collection) []int {
	var out []int
	for _, ch := range c.Changes {
		out = append(out, ch.PR)
	}
	return out
}

func TestCollectRanges(t *testing.T) {
	tl, _ := fixtureTool(t)
	for _, tc := range []struct {
		product, tag, prev string
		prs                []int // 0 is the commit with no pull request
	}{
		{"cli", "cli/v0.1.0", "", []int{1}},
		{"cli", "cli/v0.2.0-rc.1", "cli/v0.1.0", []int{3}},
		{"cli", "cli/v0.2.0-rc.2", "cli/v0.2.0-rc.1", []int{0, 4}},
		{"cli", "cli/v0.2.0", "cli/v0.1.0", []int{5, 0, 4, 3}}, // the rcs fold in
		{"desktop", "desktop/v0.1.0", "", []int{2}},
	} {
		c, err := tl.collect(context.Background(), tc.product, tc.tag)
		if err != nil {
			t.Fatalf("%s: %v", tc.tag, err)
		}
		if c.Previous != tc.prev {
			t.Errorf("%s: previous = %q, want %q", tc.tag, c.Previous, tc.prev)
		}
		if got := prs(c); !reflect.DeepEqual(got, tc.prs) {
			t.Errorf("%s: prs = %v, want %v", tc.tag, got, tc.prs)
		}
	}
}

func TestCollectDescribesEachChange(t *testing.T) {
	tl, _ := fixtureTool(t)
	c, err := tl.collect(context.Background(), "cli", "cli/v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if c.Repo != testRepo || c.Range != "cli/v0.1.0..cli/v0.2.0" || c.Notice != untrustedNotice {
		t.Errorf("collection = %+v", c)
	}

	byPR := map[int]change{}
	for _, ch := range c.Changes {
		byPR[ch.PR] = ch
	}
	fix := byPR[5]
	if !fix.MustCite || fix.Type != "fix" || fix.Scope != "cli" || fix.Author != "app/dependabot" || !fix.Bot ||
		!reflect.DeepEqual(fix.Labels, []string{"l5"}) {
		t.Errorf("#5 = %+v", fix)
	}
	breaking := byPR[3]
	if !breaking.MustCite || !breaking.Breaking || breaking.Untrusted.Summary != "Renames it." || breaking.Untrusted.Title != "PR 3" {
		t.Errorf("#3 = %+v %+v", breaking, breaking.Untrusted)
	}
	docs := byPR[4]
	if docs.MustCite || docs.Untrusted.KeyChanges != "- help text" {
		t.Errorf("#4 = %+v %+v", docs, docs.Untrusted)
	}
	direct := byPR[0]
	if direct.MustCite || direct.Untrusted != nil || direct.Subject != "tweak the cli by hand" || len(direct.Commit) != 40 {
		t.Errorf("the direct commit = %+v", direct)
	}
}

func TestCollectListsNewContributors(t *testing.T) {
	tl, f := fixtureTool(t)
	ctx := context.Background()

	// alice merged #1 before cli/v0.1.0's commit; bob had merged nothing; the
	// bot is never listed.
	c, err := tl.collect(ctx, "cli", "cli/v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := []contributor{{"bob", 4}}; !reflect.DeepEqual(c.NewContributors, want) {
		t.Errorf("new contributors = %+v, want %+v", c.NewContributors, want)
	}

	// Every author of a product's first release is new, and GitHub is not asked.
	f.calls = nil
	c, err = tl.collect(ctx, "cli", "cli/v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := []contributor{{"alice", 1}}; !reflect.DeepEqual(c.NewContributors, want) {
		t.Errorf("new contributors = %+v, want %+v", c.NewContributors, want)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "pr list") {
			t.Errorf("asked %s", call)
		}
	}
}

func TestCollectFails(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, product, tag, drop, want string
	}{
		{"unknown product", "web", "web/v1.0.0", "", "no release-web filter"},
		{"another product's tag", "cli", "desktop/v0.1.0", "", "cli has no tag desktop/v0.1.0"},
		{"missing tag", "cli", "cli/v9.0.0", "", "git fetch --tags"},
		{"malformed tag", "cli", "cli/v1", "", "is not cli/v"},
		{"pull request unreadable", "cli", "cli/v0.1.0", "pr view 1", "no reply"},
		{"contributor unknown", "cli", "cli/v0.2.0", "pr list --author bob", "no reply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl, f := fixtureTool(t)
			gitT(t, tl.dir, nil, "tag", "cli/v1")
			for k := range f.replies {
				if tc.drop != "" && strings.HasPrefix(k, tc.drop) {
					delete(f.replies, k)
				}
			}
			err := tl.collectCmd(ctx, tc.product, tc.tag, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCollectCmdPrintsJSON(t *testing.T) {
	tl, _ := fixtureTool(t)
	var out strings.Builder
	if err := tl.collectCmd(context.Background(), "cli", "cli/v0.2.0-rc.1", &out); err != nil {
		t.Fatal(err)
	}
	var c collection
	if err := json.Unmarshal([]byte(out.String()), &c); err != nil {
		t.Fatal(err)
	}
	if c.Tag != "cli/v0.2.0-rc.1" || len(c.Changes) != 1 {
		t.Errorf("collection = %+v", c)
	}
	if !strings.Contains(out.String(), `"untrusted": {`) {
		t.Errorf("no untrusted field in %s", out.String())
	}
}

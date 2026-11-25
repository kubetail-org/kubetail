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

package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchCrossesSlashes(t *testing.T) {
	for _, c := range []struct {
		pattern, value string
		want           bool
	}{
		{"*prod*", "arn:aws:eks:us-east-1:1:cluster/prod-eu", true},
		{"*prod*", "gke_p_z_prod", true},
		{"dev-*", "dev-eks", true},
		{"dev-?ks", "dev-eks", true},
		{"dev-?", "dev-eks", false},
		{"[a]", "[a]", true},
		{"[a]", "a", false},
		{"a.b", "axb", false},
		{"", "anything", false},
		{"", "", true},
		{"dev", "prod", false},
		{"dev", "dev", true},
		{"*", "", true},
		{"a*b*c", "a/x/b:y/c", true},
		{"a*b", "a/x/bc", false},
		{`a\`, `a\`, true},
	} {
		assert.Equal(t, c.want, Match(c.pattern, c.value), "%q against %q", c.pattern, c.value)
	}
}

func TestALiteralMatchesItselfAlone(t *testing.T) {
	assert.Equal(t, `dev\*`, Literal("dev*"))
	assert.True(t, Match(Literal("dev*"), "dev*"))
	assert.False(t, Match(Literal("dev*"), "dev-eks"))
	for _, v := range []string{"a?b", `a\b`, `x\*?y`, ""} {
		assert.True(t, Match(Literal(v), v), "%q", v)
	}
	assert.False(t, Match(Literal("a?b"), "axb"))
}

func TestAScopePatternNeverCrossesASegment(t *testing.T) {
	for _, c := range []struct {
		pattern, value string
		want           bool
	}{
		{"*", "c1", true},
		{"*", "c1/team-a", true},
		{"*", "", true},
		{"c1", "c1", true},
		{"c1", "c1/team-a", false},
		{"c1/team-a", "c1/team-a", true},
		{"c1/team-a", "c1/team-b", false},
		{"c1/*", "c1/team-a", true},
		{"c1/*", "c1", false},
		{"c1/*", "c1/team-a/x", false},
		{"*/*", "c1/team-a", true},
		{"*/*", "c1", false},
		{"c*", "c1/team-a", false},
		{"c1/team-*", "c1/team-a", true},
		{"", "", true},
		{"", "c1", false},
		{"deployments", "deployments/scale", false},
		{"deployments/*", "deployments/scale", true},
	} {
		assert.Equal(t, c.want, MatchPath(c.pattern, c.value), "%q against %q", c.pattern, c.value)
	}
}

func TestATrailingDoubleStarCoversTheAccountAndBelow(t *testing.T) {
	for _, c := range []struct {
		pattern, value string
		want           bool
	}{
		{"c1/**", "c1", true},
		{"c1/**", "c1/team-a", true},
		{"c1/**", "c1/team-a/x", true},
		{"c1/**", "c2/team-a", false},
		{"c1/**", "c10", false},
		{"**", "", true},
		{"**", "c1/team-a", true},
		{"*/**", "c1", true},
		{"c1/team-a/**", "c1", false},
	} {
		assert.Equal(t, c.want, MatchPath(c.pattern, c.value), "%q against %q", c.pattern, c.value)
	}
}

// A path's literal is each segment's: Literal leaves / alone, so it never
// joins or splits a segment.
func TestAPatternBelowAPath(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		pattern, value string
		below          bool
	}{
		{"c1/prod", "c1", true},
		{"*/prod", "c1", true},
		{"c?/prod/x", "c1", true},
		{"c1", "c1", false},
		{"c2/prod", "c1", false},
		{"c1/prod", "c1/prod", false},
		{"*", "c1", false},
	} {
		assert.Equal(t, c.below, MatchPathBelow(c.pattern, c.value), "%+v", c)
	}
}

func TestAPatternAtAndBelowAPath(t *testing.T) {
	t.Parallel()
	for pattern, want := range map[string]bool{"*": true, "**": true, "c1/**": true, "c?/**": true, "c1": false, "c1/*": false, "c2/**": false} {
		assert.Equal(t, want, MatchPathAndBelow(pattern, "c1"), pattern)
	}
}

func TestALiteralPathMatchesItselfAlone(t *testing.T) {
	for _, v := range []string{"c1/team-a", "dev*/a?b", `x\y/**`, "c1"} {
		assert.True(t, MatchPath(Literal(v), v), "%q", v)
	}
	assert.False(t, MatchPath(Literal("dev*/a"), "dev-eks/a"))
	assert.False(t, MatchPath(Literal("c1/**"), "c1/team-a"))
}

func TestLiteralValue(t *testing.T) {
	for p, want := range map[string]bool{"c1": true, `dev\*`: true, "": true, "dev*": false, "a?": false, `a\`: false, "**": false} {
		v, ok := LiteralValue(p)
		assert.Equal(t, want, ok, "%q", p)
		if ok {
			assert.Equal(t, p, Literal(v), "%q", p)
		}
	}
}

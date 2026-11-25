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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patch is the chat's patch of the deployment api in dev / team-a, at level l.
func patch(l AccessLevel) Action {
	return Action{
		Principal: Chat, Target: Kubernetes, Scope: "dev/team-a", AccountName: "dev",
		Service: "apps", Operation: "patch", Resource: "deployments", Name: "api", AccessLevel: l,
	}
}

// rule is a rule of effect and level over everything.
func rule(e Effect, l AccessLevel) Rule {
	return Rule{ID: string(e), Effect: e, AccessLevel: l, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}
}

func TestAccessLevelsAreNamed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []AccessLevel{Read, DataRead, Write, AccessControl, Destructive, Reach}, Levels)
	for name, l := range map[string]AccessLevel{
		"read": Read, "data-read": DataRead, "write": Write, "access-control": AccessControl, "destructive": Destructive, "reach": Reach,
	} {
		assert.Equal(t, name, string(l))
		got, ok := LevelNamed(name)
		assert.True(t, ok, name)
		assert.Equal(t, l, got)
	}
	for _, name := range []string{"", "Write", "upstream-write", "secret-read"} {
		_, ok := LevelNamed(name)
		assert.False(t, ok, name)
	}
}

func TestCoversIsData(t *testing.T) {
	t.Parallel()
	for _, l := range Levels {
		for _, o := range Levels {
			want := l == o || l == Write && (o == Destructive || o == AccessControl)
			assert.Equal(t, want, l.Covers(o), "%s covers %s", l, o)
		}
	}
}

func TestPrincipalsAreEveryPrincipal(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []Principal{Chat, Subagent, Monitor, Console}, Principals)
}

func TestTargetsAreNamed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []Target{Kubernetes, AWS, Azure, GCP, GitHub}, Targets)
	assert.Equal(t, Target("kubernetes"), Kubernetes)
	for _, tg := range Targets {
		got, ok := TargetNamed(string(tg))
		assert.True(t, ok)
		assert.Equal(t, tg, got)
	}
	var none Target
	_, ok := TargetNamed(string(none))
	assert.False(t, ok, "the zero value is no target")
	_, ok = TargetNamed("cluster")
	assert.False(t, ok)
}

func TestARuleMatchesByLevelPrincipalTargetAndPatterns(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	for name, c := range map[string]struct {
		edit func(*Rule)
		want bool
	}{
		"everything":            {func(*Rule) {}, true},
		"its principal":         {func(r *Rule) { r.Principal = "chat" }, true},
		"another principal":     {func(r *Rule) { r.Principal = "subagent" }, false},
		"its target":            {func(r *Rule) { r.Target = "kubernetes" }, true},
		"another target":        {func(r *Rule) { r.Target = "aws" }, false},
		"its scope":             {func(r *Rule) { r.Scope = "dev/team-a" }, true},
		"its account below":     {func(r *Rule) { r.Scope = "dev/**" }, true},
		"its account alone":     {func(r *Rule) { r.Scope = "dev" }, false},
		"another namespace":     {func(r *Rule) { r.Scope = "dev/team-b" }, false},
		"its service":           {func(r *Rule) { r.Service = "apps" }, true},
		"the core group":        {func(r *Rule) { r.Service = "" }, false},
		"its operation":         {func(r *Rule) { r.Operation = "patch" }, true},
		"another operation":     {func(r *Rule) { r.Operation = "delete" }, false},
		"its resource":          {func(r *Rule) { r.Resource = "deployments" }, true},
		"another resource":      {func(r *Rule) { r.Resource = "pods" }, false},
		"a resource's subparts": {func(r *Rule) { r.Resource = "deployments/*" }, false},
		"another level":         {func(r *Rule) { r.AccessLevel = DataRead }, false},
	} {
		r := rule(Deny, Write)
		c.edit(&r)
		assert.Equal(t, c.want, r.Matches(act), name)
	}
	core := patch(Write)
	core.Service = ""
	r := rule(Deny, Write)
	r.Service = ""
	assert.True(t, r.Matches(core), "an empty service is the core group")
}

func TestAnAllowRuleMatchesItsOwnLevelAlone(t *testing.T) {
	t.Parallel()
	for _, l := range Levels {
		for _, o := range Levels {
			assert.Equal(t, l == o, rule(Allow, l).Matches(patch(o)), "allow %s on %s", l, o)
		}
	}
}

// A list across namespaces reaches each of them: a deny or an ask for any one
// decides it, and only an allow reaching them all allows it.
func TestARuleForAScopeBelowDecidesASpanningAction(t *testing.T) {
	t.Parallel()
	list := Action{Principal: Chat, Target: Kubernetes, Scope: "c1", Service: "", Operation: "list", Resource: "secrets",
		AccessLevel: DataRead, Spans: true}
	at := func(effect Effect, scope string) Rule {
		return Rule{ID: "r", Effect: effect, AccessLevel: DataRead, Principal: "*", Target: "kubernetes",
			Scope: scope, Service: "*", Operation: "*", Resource: "*"}
	}
	for _, c := range []struct {
		rule Rule
		want Decision
	}{
		{at(Deny, "c1/prod"), Denied},
		{at(Ask, "c1/prod"), Prompted},
		{at(Deny, "c2/prod"), Allowed},
		{at(Allow, "c1/dev"), Allowed},
	} {
		v, _ := Policy{Mode: ModeAuto, Rules: []Rule{c.rule}}.Authorize(list)
		assert.Equal(t, c.want, v.Outcome(), "%+v", c.rule)
	}
	v, _ := Policy{Mode: ModeAsk, Rules: []Rule{at(Allow, "c1/dev")}}.Authorize(list)
	assert.Equal(t, Prompted, v.Outcome(), "an allow for one namespace does not allow them all")
	v, _ = Policy{Mode: ModeAsk, Rules: []Rule{at(Allow, "c1")}}.Authorize(list)
	assert.Equal(t, Prompted, v.Outcome(), "nor does one for the cluster's own objects alone")
	v, _ = Policy{Mode: ModeAsk, Rules: []Rule{at(Allow, "c1/**")}}.Authorize(list)
	assert.Equal(t, Allowed, v.Outcome(), "an allow for every namespace does")
	v, _ = Policy{Mode: ModeAsk, Rules: []Rule{GrantRule(list)}}.Authorize(list)
	assert.Equal(t, Allowed, v.Outcome(), "and so does the rule an answer to it writes")
	assert.Equal(t, "c1/**", GrantRule(list).Scope)
	list.Spans = false
	v, _ = Policy{Mode: ModeAuto, Rules: []Rule{at(Deny, "c1/prod")}}.Authorize(list)
	assert.Equal(t, Allowed, v.Outcome(), "an action at the cluster alone is not below")
}

func TestAWriteRuleCoversDestructiveAndAccessControl(t *testing.T) {
	t.Parallel()
	for _, e := range []Effect{Deny, Ask} {
		for _, o := range []AccessLevel{Write, Destructive, AccessControl} {
			assert.True(t, rule(e, Write).Matches(patch(o)), "%s write on %s", e, o)
		}
		for _, o := range []AccessLevel{Read, DataRead, Reach} {
			assert.False(t, rule(e, Write).Matches(patch(o)), "%s write on %s", e, o)
		}
	}
	assert.False(t, rule(Deny, Destructive).Matches(patch(Write)), "a destructive rule leaves a write alone")
}

func TestANamespacesRuleDoesNotCoverTheNamespaceObject(t *testing.T) {
	t.Parallel()
	ns := Action{Principal: Chat, Target: Kubernetes, Scope: "dev", Service: "", Operation: "patch", Resource: "namespaces", Name: "team-a", AccessLevel: Write}
	for _, scope := range []string{"dev/team-a", "dev/*", "*/*"} {
		r := rule(Deny, Write)
		r.Scope = scope
		assert.False(t, r.Matches(ns), scope)
		assert.True(t, r.Matches(patch(Write)), scope)
	}
	scale := patch(Write)
	scale.Resource = "deployments/scale"
	r := rule(Deny, Write)
	r.Resource = "deployments"
	assert.False(t, r.Matches(scale), "a resource does not cover its scale")
	r.Resource = "deployments/scale"
	assert.True(t, r.Matches(scale))
}

func TestEveryPrincipalHasEveryLevel(t *testing.T) {
	t.Parallel()
	for _, p := range Principals {
		for _, l := range Levels {
			_, ok := boundary[p][l]
			assert.True(t, ok, "%s %s", p, l)
		}
		_, ok := asks[p]
		assert.True(t, ok, "%s asks", p)
	}
}

func TestEveryModeHasEveryLevel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []Mode{ModeReadOnly, ModeAsk, ModeAuto}, Modes)
	for _, m := range Modes {
		for _, l := range Levels {
			_, ok := modes[m][l]
			assert.True(t, ok, "%s %s", m, l)
		}
	}
}

func TestAPrincipalOrLevelWithNoRowIsNever(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	act.Principal = ""
	assert.Equal(t, Never, limitOf(act))
	act = patch("upstream-write")
	assert.Equal(t, Never, limitOf(act))
	v, why := Policy{Mode: ModeAuto}.Authorize(act)
	assert.Equal(t, Verdict{Effect: Deny}, v)
	assert.Equal(t, "it is never allowed", why)
}

// orderedTable states the spec's order as a priority list: the first step
// that applies decides. It is a second statement of verdict, so a change to
// one changes the other in the same commit.
func orderedTable(p Policy, act Action) (Verdict, string) {
	limit := map[Principal]map[AccessLevel]Limit{
		Chat:     {Read: Free, DataRead: Free, Write: Free, AccessControl: Scoped, Destructive: Once, Reach: Never},
		Subagent: {Read: Free, DataRead: Free, Write: Free, AccessControl: Scoped, Destructive: Once, Reach: Never},
		Monitor:  {Read: Free, DataRead: Never, Write: Never, AccessControl: Never, Destructive: Never, Reach: Never},
		Console:  {Read: Free, DataRead: Never, Write: Never, AccessControl: Never, Destructive: Never, Reach: Never},
	}[act.Principal][act.AccessLevel]
	if limit == Scoped {
		limit = Once
		if act.Confined {
			limit = Free
		}
	}
	if (act.Once || act.Scope == "") && limit < Once {
		limit = Once
	}
	mode := map[Mode]map[AccessLevel]Effect{
		ModeReadOnly: {Read: Allow, DataRead: Ask, Write: Deny, AccessControl: Deny, Destructive: Deny, Reach: Deny},
		ModeAsk:      {Read: Allow, DataRead: Ask, Write: Ask, AccessControl: Ask, Destructive: Ask, Reach: Deny},
		ModeAuto:     {Read: Allow, DataRead: Allow, Write: Allow, AccessControl: Ask, Destructive: Ask, Reach: Deny},
	}[p.Mode][act.AccessLevel]
	matching := func(e Effect) (Rule, bool) {
		for _, r := range p.Rules {
			if r.Effect == e && r.Matches(act) {
				return r, true
			}
		}
		return Rule{}, false
	}
	switch r, deny := matching(Deny); {
	case limit == Never:
		return Verdict{Effect: Deny}, "it is never allowed"
	case deny:
		return Verdict{Effect: Deny}, "a rule denies it: " + r.Line(act.AccountName)
	case mode == Deny:
		return Verdict{Effect: Deny}, "this account is read-only"
	}
	if r, ok := matching(Ask); ok {
		return Verdict{Effect: Ask}, "a rule asks for it: " + r.Line(act.AccountName)
	}
	if limit == Once {
		return Verdict{Effect: Ask}, "it always asks"
	}
	if r, ok := matching(Allow); ok {
		return Verdict{Effect: Allow}, "a rule allows it: " + r.Line(act.AccountName)
	}
	if mode == Allow {
		return Verdict{Effect: Allow}, string(p.Mode) + " mode"
	}
	return Verdict{Effect: Ask, Grantable: !act.DryRun}, string(p.Mode) + " mode"
}

// tableAuthorize is Authorize restated over orderedTable: a principal that
// does not ask is denied what would ask, and a subagent takes the stricter
// of its verdict and the chat's.
func tableAuthorize(p Policy, act Action) (Verdict, string) {
	once := func(act Action) (Verdict, string) {
		v, why := orderedTable(p, act)
		if v.Effect == Ask && (act.Principal == Monitor || act.Principal == Console) {
			return Verdict{Effect: Deny}, "this session never asks: " + why
		}
		return v, why
	}
	v, why := once(act)
	if act.Principal == Subagent {
		parent := act
		parent.Principal = Chat
		pv, pwhy := once(parent)
		rank := func(v Verdict) int {
			switch {
			case v.Effect == Deny:
				return 0
			case v.Effect == Ask && !v.Grantable:
				return 1
			case v.Effect == Ask:
				return 2
			}
			return 3
		}
		if rank(pv) < rank(v) {
			return pv, pwhy
		}
	}
	return v, why
}

// ruleChoices are what a test policy holds for one effect: none, or one rule
// at a level a rule may name, for every principal or one.
var ruleChoices = []*Rule{
	nil,
	{AccessLevel: Write, Principal: "*"},
	{AccessLevel: Destructive, Principal: "*"},
	{AccessLevel: DataRead, Principal: "*"},
	{AccessLevel: Write, Principal: "chat"},
}

// eachCase calls f with every policy and action the ordered table covers.
func eachCase(t *testing.T, f func(p Policy, act Action)) {
	t.Helper()
	for _, mode := range Modes {
		for _, deny := range ruleChoices {
			for _, ask := range ruleChoices {
				for _, allow := range ruleChoices {
					var rules []Rule
					for e, c := range map[Effect]*Rule{Deny: deny, Ask: ask, Allow: allow} {
						if c != nil {
							r := rule(e, c.AccessLevel)
							r.Principal = c.Principal
							rules = append(rules, r)
						}
					}
					p := Policy{Mode: mode, Rules: rules}
					for _, principal := range Principals {
						for _, level := range Levels {
							for bits := range 16 {
								act := patch(level)
								act.Principal = principal
								act.Once, act.Confined, act.DryRun = bits&1 != 0, bits&2 != 0, bits&8 != 0
								if bits&4 != 0 {
									act.Scope = ""
								}
								f(p, act)
							}
						}
					}
				}
			}
		}
	}
}

func TestTheVerdictMatchesTheOrderedTable(t *testing.T) {
	t.Parallel()
	eachCase(t, func(p Policy, act Action) {
		want, wantWhy := tableAuthorize(p, act)
		got, why := p.Authorize(act)
		if want != got || wantWhy != why {
			t.Errorf("%s, %+v, rules %v: got %+v %q, want %+v %q", p.Mode, act, p.Rules, got, why, want, wantWhy)
		}
	})
}

func TestAVerdictIsOrderIndependent(t *testing.T) {
	t.Parallel()
	for _, rules := range [][]Rule{
		{rule(Deny, Write), rule(Ask, Write), rule(Allow, Write)},
		{rule(Deny, Write), rule(Allow, Write), rule(Ask, Write)},
		{rule(Ask, Write), rule(Deny, Write), rule(Allow, Write)},
		{rule(Ask, Write), rule(Allow, Write), rule(Deny, Write)},
		{rule(Allow, Write), rule(Deny, Write), rule(Ask, Write)},
		{rule(Allow, Write), rule(Ask, Write), rule(Deny, Write)},
	} {
		got, why := Policy{Mode: ModeAuto, Rules: rules}.Authorize(patch(Write))
		assert.Equal(t, Verdict{Effect: Deny}, got, "%v", rules)
		assert.Equal(t, "a rule denies it: Deny writes everywhere", why, "%v", rules)
	}
}

func TestDecideFollowsTheModeTable(t *testing.T) {
	t.Parallel()
	for mode, want := range map[Mode]map[AccessLevel]Decision{
		ModeReadOnly: {Read: Allowed, DataRead: Prompted, Write: Denied, AccessControl: Denied, Destructive: Denied, Reach: Denied},
		ModeAsk:      {Read: Allowed, DataRead: Prompted, Write: Prompted, AccessControl: Prompted, Destructive: Prompted, Reach: Denied},
		ModeAuto:     {Read: Allowed, DataRead: Allowed, Write: Allowed, AccessControl: Prompted, Destructive: Prompted, Reach: Denied},
	} {
		for l, d := range want {
			got, _ := Policy{Mode: mode}.Decide(patch(l))
			assert.Equal(t, d, got, "%s %s", mode, l)
		}
	}
}

func TestOutcomeOfEveryEffect(t *testing.T) {
	t.Parallel()
	for v, want := range map[Verdict]Decision{
		{Effect: Allow}:                  Allowed,
		{Effect: Ask}:                    Prompted,
		{Effect: Ask, Grantable: true}:   Prompted,
		{Effect: Deny}:                   Denied,
		{Effect: "something"}:            Denied,
		{Effect: Deny, Grantable: false}: Denied,
	} {
		assert.Equal(t, want, v.Outcome(), "%+v", v)
	}
}

func TestReachIsNeverAllowed(t *testing.T) {
	t.Parallel()
	eachCase(t, func(p Policy, act Action) {
		if act.AccessLevel != Reach {
			return
		}
		allow := rule(Allow, Reach)
		p.Rules = append(p.Rules, allow)
		v, why := p.Authorize(act)
		if v.Effect != Deny || why != "it is never allowed" {
			t.Errorf("%s, %+v: %+v %q", p.Mode, act, v, why)
		}
	})
}

func TestAnUnnamedOperationAsksOnceAndIsNotGranted(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	act.Once = true
	for _, mode := range Modes {
		for _, rules := range [][]Rule{nil, {rule(Allow, Write)}, {CommandRule(act)}, {GrantRule(act)}} {
			v, why := Policy{Mode: mode, Rules: rules}.Authorize(act)
			if mode == ModeReadOnly {
				assert.Equal(t, Verdict{Effect: Deny}, v)
				assert.Equal(t, "this account is read-only", why)
				continue
			}
			assert.Equal(t, Verdict{Effect: Ask}, v, "%s %v", mode, rules)
			assert.Equal(t, "it always asks", why)
		}
	}
	v, _ := Policy{Mode: ModeAuto, Rules: []Rule{rule(Deny, Write)}}.Authorize(act)
	assert.Equal(t, Deny, v.Effect, "a deny rule stops it as a write")
}

func TestAnActionWithNoAccountAsksOnce(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	act.Scope = ""
	for _, mode := range []Mode{ModeAsk, ModeAuto} {
		for _, rules := range [][]Rule{nil, {rule(Allow, Write)}} {
			v, why := Policy{Mode: mode, Rules: rules}.Authorize(act)
			assert.Equal(t, Verdict{Effect: Ask}, v, "%s", mode)
			assert.Equal(t, "it always asks", why)
		}
	}
	data := patch(DataRead)
	data.Scope = ""
	v, _ := Policy{Mode: ModeAuto}.Authorize(data)
	assert.Equal(t, Verdict{Effect: Ask}, v, "a data read with no account asks too")
}

func TestOnlyAConfinedAccessControlActionIsGrantable(t *testing.T) {
	t.Parallel()
	iam := Action{Principal: Chat, Target: AWS, Scope: "123456789012/global", AccountName: "123456789012", Service: "iam", Operation: "AttachRolePolicy", AccessLevel: AccessControl}
	setIam := Action{Principal: Chat, Target: GCP, Scope: "my-project", AccountName: "my-project", Service: "cloudresourcemanager", Operation: "setiampolicy", Resource: "projects", AccessLevel: AccessControl}
	for _, act := range []Action{iam, setIam} {
		for _, mode := range []Mode{ModeAsk, ModeAuto} {
			for _, rules := range [][]Rule{nil, {rule(Allow, AccessControl)}, {GrantRule(act)}} {
				v, why := Policy{Mode: mode, Rules: rules}.Authorize(act)
				assert.Equal(t, Verdict{Effect: Ask}, v, "%s %s", act.Target, mode)
				assert.Equal(t, "it always asks", why)
			}
		}
		v, _ := Policy{Mode: ModeReadOnly}.Authorize(act)
		assert.Equal(t, Deny, v.Effect)
	}
	confined := iam
	confined.Confined = true
	v, _ := Policy{Mode: ModeAsk}.Authorize(confined)
	assert.Equal(t, Verdict{Effect: Ask, Grantable: true}, v)
	v, _ = Policy{Mode: ModeAsk, Rules: []Rule{GrantRule(confined)}}.Authorize(confined)
	assert.Equal(t, Verdict{Effect: Allow}, v)
}

func TestAnAccessControlWriteIsGrantedInsideAScopeAlone(t *testing.T) {
	t.Parallel()
	role := Action{Principal: Chat, Target: Kubernetes, Scope: "dev/team-a", AccountName: "dev", Service: "rbac.authorization.k8s.io", Operation: "create", Resource: "roles", AccessLevel: AccessControl, Confined: true}
	v, _ := Policy{Mode: ModeAsk}.Authorize(role)
	assert.Equal(t, Verdict{Effect: Ask, Grantable: true}, v, "a Role in a namespace")
	v, _ = Policy{Mode: ModeAsk, Rules: []Rule{GrantRule(role)}}.Authorize(role)
	assert.Equal(t, Allow, v.Effect)
	v, _ = Policy{Mode: ModeAuto}.Authorize(role)
	assert.Equal(t, Verdict{Effect: Ask, Grantable: true}, v, "auto asks for permissions")

	clusterRole := Action{Principal: Chat, Target: Kubernetes, Scope: "dev", AccountName: "dev", Service: "rbac.authorization.k8s.io", Operation: "create", Resource: "clusterroles", AccessLevel: AccessControl}
	v, why := Policy{Mode: ModeAsk, Rules: []Rule{rule(Allow, AccessControl)}}.Authorize(clusterRole)
	assert.Equal(t, Verdict{Effect: Ask}, v, "a ClusterRole asks once")
	assert.Equal(t, "it always asks", why)
}

func TestTheMonitorNeverWritesOrReadsData(t *testing.T) {
	t.Parallel()
	for _, l := range []AccessLevel{DataRead, Write, AccessControl, Destructive, Reach} {
		assert.Equal(t, Never, boundary[Monitor][l], l)
		act := patch(l)
		act.Principal = Monitor
		for _, mode := range Modes {
			v, why := Policy{Mode: mode, Rules: []Rule{rule(Allow, l)}}.Authorize(act)
			assert.Equal(t, Verdict{Effect: Deny}, v, "%s %s", mode, l)
			assert.Equal(t, "it is never allowed", why)
		}
	}
	assert.Equal(t, Free, boundary[Monitor][Read])
}

func TestTheConsoleNeverAsks(t *testing.T) {
	t.Parallel()
	assert.False(t, asks[Console])
	assert.False(t, asks[Monitor])
	assert.True(t, asks[Chat])
	assert.True(t, asks[Subagent])
	for _, l := range []AccessLevel{DataRead, Write, AccessControl, Destructive, Reach} {
		assert.Equal(t, Never, boundary[Console][l], l)
	}
	// A level the boundary frees for the console that still asks is denied.
	act := patch(Read)
	act.Principal, act.Scope = Console, ""
	v, why := Policy{Mode: ModeAuto}.Authorize(act)
	assert.Equal(t, Verdict{Effect: Deny}, v)
	assert.Equal(t, "this session never asks: it always asks", why)
}

func TestASubagentIsNeverWiderThanItsParent(t *testing.T) {
	t.Parallel()
	rank := func(v Verdict) int {
		switch {
		case v.Effect == Deny:
			return 0
		case v.Effect == Ask && !v.Grantable:
			return 1
		case v.Effect == Ask:
			return 2
		}
		return 3
	}
	eachCase(t, func(p Policy, act Action) {
		if act.Principal != Subagent {
			return
		}
		sub, _ := p.Authorize(act)
		act.Principal = Chat
		chat, _ := p.Authorize(act)
		if rank(sub) > rank(chat) {
			t.Errorf("%s, %+v, rules %v: subagent %+v wider than chat %+v", p.Mode, act, p.Rules, sub, chat)
		}
	})
	// A deny for the chat reaches its subagents.
	deny := rule(Deny, Write)
	deny.Principal = "chat"
	act := patch(Write)
	act.Principal = Subagent
	v, why := Policy{Mode: ModeAuto, Rules: []Rule{deny}}.Authorize(act)
	assert.Equal(t, Verdict{Effect: Deny}, v)
	assert.Equal(t, "a rule denies it: Deny writes by chat everywhere", why)
}

func TestADryRunIsNotGrantable(t *testing.T) {
	t.Parallel()
	write := patch(Write)
	v, _ := Policy{Mode: ModeAsk}.Authorize(write)
	assert.True(t, v.Grantable)
	write.DryRun = true
	v, _ = Policy{Mode: ModeAsk}.Authorize(write)
	assert.Equal(t, Verdict{Effect: Ask}, v)
}

func TestAnActionThatNamesNoRuleIsNotGrantable(t *testing.T) {
	t.Parallel()
	// A one-segment scope's rule names the resource; one with none cannot be
	// stored, so nothing may be written for it.
	act := Action{Principal: Chat, Target: Azure, Scope: "sub-1", AccountName: "sub-1", Service: "microsoft.compute", Operation: "write", AccessLevel: Write}
	v, _ := Policy{Mode: ModeAsk}.Authorize(act)
	assert.Equal(t, Verdict{Effect: Ask}, v)
	act.Resource = "virtualmachines"
	v, _ = Policy{Mode: ModeAsk}.Authorize(act)
	assert.Equal(t, Verdict{Effect: Ask, Grantable: true}, v)
}

func TestAGrantRuleNamesTheActionsScope(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	r := GrantRule(act)
	assert.Equal(t, Rule{Effect: Allow, AccessLevel: Write, Principal: "*", Target: "kubernetes", Scope: "dev/team-a", Service: "*", Operation: "*", Resource: "*"}, r)
	assert.True(t, r.Matches(act))
	sub := act
	sub.Principal = Subagent
	assert.True(t, r.Matches(sub), "a chat's grant reaches its subagents")
	other := act
	other.Scope = "dev/team-b"
	assert.False(t, r.Matches(other))

	ns := Action{Principal: Chat, Target: Kubernetes, Scope: "dev*", AccountName: "dev", Service: "", Operation: "patch", Resource: "nodes", AccessLevel: Write}
	r = GrantRule(ns)
	assert.Equal(t, Rule{Effect: Allow, AccessLevel: Write, Principal: "*", Target: "kubernetes", Scope: `dev\*`, Service: "", Operation: "*", Resource: "nodes"}, r)
	pod := ns
	pod.Resource = "pods"
	assert.False(t, r.Matches(pod), "a one-segment scope's grant names its resource")

	scale := patch(Write)
	scale.Resource = "deployments/scale"
	assert.True(t, GrantRule(patch(Write)).Matches(scale), "a namespace's grant is every write in it")
}

func TestACommandRuleNamesTheChange(t *testing.T) {
	t.Parallel()
	del := Action{Principal: Chat, Target: Kubernetes, Scope: "c1/web", AccountName: "dev*", Service: "", Operation: "delete", Resource: "pods", Name: "a", AccessLevel: Write}
	r := CommandRule(del)
	assert.True(t, r.Command)
	other := del
	other.Name = "b"
	assert.True(t, r.Matches(other), "another pod's delete")
	for name, edit := range map[string]func(*Action){
		"a deployment's delete":           func(a *Action) { a.Service, a.Resource = "apps", "deployments" },
		"a patch of a pod":                func(a *Action) { a.Operation = "patch" },
		"a pod delete on another cluster": func(a *Action) { a.Scope = "c2/web" },
	} {
		act := del
		edit(&act)
		assert.False(t, r.Matches(act), name)
	}
	assert.Equal(t, `Allow delete of core pods in "dev*" / web for this command`, r.Line(del.AccountName))

	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "ommand")
	var back Rule
	require.NoError(t, json.Unmarshal([]byte(`{"id":"r","effect":"allow","command":true,"Command":true}`), &back))
	assert.False(t, back.Command, "a stored rule is never a command's")
}

func TestUnreadRulesRefuseWritesAndDataReads(t *testing.T) {
	t.Parallel()
	p := Policy{Mode: ModeAuto, Rules: Unread()}
	for _, l := range []AccessLevel{Write, Destructive, AccessControl, DataRead} {
		for _, target := range Targets {
			act := patch(l)
			act.Target = target
			got, _ := p.Authorize(act)
			assert.Equal(t, Deny, got.Effect, "%s %s", target, l)
		}
	}
	assert.Equal(t, DataRead, RefusedData.AccessLevel)
	assert.False(t, Write.Covers(DataRead))
}

func TestRuleSame(t *testing.T) {
	t.Parallel()
	a := rule(Allow, Write)
	b := a
	b.ID = "b"
	assert.True(t, a.Same(b))
	b.Scope = "web"
	assert.False(t, a.Same(b))
}

func TestTheLineOfARule(t *testing.T) {
	t.Parallel()
	r := func(e Effect, l AccessLevel, target, scope string, edit func(*Rule)) Rule {
		x := Rule{Effect: e, AccessLevel: l, Principal: "*", Target: target, Scope: scope, Service: "*", Operation: "*", Resource: "*"}
		if edit != nil {
			edit(&x)
		}
		return x
	}
	for _, c := range []struct {
		want, account string
		rule          Rule
	}{
		{"Allow writes in dev-eks / team-a", "dev-eks", r(Allow, Write, "kubernetes", "c1/team-a", nil)},
		{"Deny destructive delete of core namespaces in dev-eks", "dev-eks", r(Deny, Destructive, "kubernetes", "c1", func(x *Rule) { x.Service, x.Operation, x.Resource = "", "delete", "namespaces" })},
		{"Ask for writes by subagent everywhere", "", r(Ask, Write, "*", "*", func(x *Rule) { x.Principal = "subagent" })},
		{"Allow writes in AWS 123456789012 / eu-west-1", "123456789012", r(Allow, Write, "aws", "123456789012/eu-west-1", nil)},
		{"Allow patch of apps deployments in dev-eks / team-a for this command", "dev-eks", r(Allow, Write, "kubernetes", "c1/team-a", func(x *Rule) {
			x.Service, x.Operation, x.Resource, x.Command = "apps", "patch", "deployments", true
		})},
		{"Allow writes anywhere in dev-eks", "dev-eks", r(Allow, Write, "kubernetes", "c1/**", nil)},
		{"Deny writes in any cluster / kube-system", "", r(Deny, Write, "kubernetes", "*/kube-system", nil)},
		{"Deny data reads in every cluster", "", r(Deny, DataRead, "kubernetes", "*", nil)},
		{"Deny data reads in every Azure account", "", r(Deny, DataRead, "azure", "**", nil)},
		{"Allow writes in Azure sub-1 / rg", "sub-1", r(Allow, Write, "azure", "sub-1/rg", nil)},
		{"Allow writes in Google Cloud my-project", "my-project", r(Allow, Write, "gcp", "my-project", nil)},
		{"Allow writes in GitHub kubetail-org / kubetail", "kubetail-org", r(Allow, Write, "github", "kubetail-org/kubetail", nil)},
		{"Deny access control changes everywhere", "", r(Deny, AccessControl, "*", "*", nil)},
		{"Deny writes of apps resources everywhere", "", r(Deny, Write, "*", "*", func(x *Rule) { x.Service = "apps" })},
		{"Deny delete of anything everywhere", "", r(Deny, Write, "*", "*", func(x *Rule) { x.Operation = "delete" })},
		{"Deny writes of deployments everywhere", "", r(Deny, Write, "*", "*", func(x *Rule) { x.Resource = "deployments" })},
		{"Allow data reads of core secrets in dev-eks", "dev-eks", r(Allow, DataRead, "kubernetes", "c1", func(x *Rule) { x.Service, x.Resource = "", "secrets" })},
		{`Allow writes in "dev*"`, "dev*", r(Allow, Write, "kubernetes", "c1", nil)},
		{`Allow writes in "a / b"`, "a / b", r(Allow, Write, "kubernetes", "c1", nil)},
		{`Allow writes in "say \"hi\""`, `say "hi"`, r(Allow, Write, "kubernetes", "c1", nil)},
		{`Allow writes in "back\\slash"`, `back\slash`, r(Allow, Write, "kubernetes", "c1", nil)},
		{`Allow delete of core "my pods" in dev / web`, "dev", r(Allow, Write, "kubernetes", "c1/web", func(x *Rule) { x.Service, x.Operation, x.Resource = "", "delete", Literal("my pods") })},
		{`Allow writes in dev / matching "a b?"`, "dev", r(Allow, Write, "kubernetes", "c1/a b?", nil)},
		{`Allow writes in c1`, "", r(Allow, Write, "kubernetes", "c1", nil)},
		{"Deny writes in any account / x", "", r(Deny, Write, "*", "*/x", nil)},
		{"Deny writes in any AWS account / eu-west-1", "", r(Deny, Write, "aws", "*/eu-west-1", nil)},
		{"Deny writes in any cluster matching prod-* / web", "", r(Deny, Write, "kubernetes", "prod-*/web", nil)},
		{"Ask for writes in any GitHub account matching kubetail?", "", r(Ask, Write, "github", "kubetail?", nil)},
		{`Deny writes in any account matching "a b*" / x`, "", r(Deny, Write, "*", "a b*/x", nil)},
	} {
		assert.Equal(t, c.want, c.rule.Line(c.account))
	}
}

func TestAnActionRoundTripsAsJSON(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	act.Summary, act.DryRun, act.Confined, act.Once, act.Spans = "Patch deployments/api in team-a on dev", true, true, true, true
	b, err := json.Marshal(act)
	require.NoError(t, err)
	assert.JSONEq(t, `{"principal":"chat","target":"kubernetes","scope":"dev/team-a","accountName":"dev","service":"apps",
		"operation":"patch","resource":"deployments","name":"api","accessLevel":"write","confined":true,"once":true,"spans":true,
		"summary":"Patch deployments/api in team-a on dev","dryRun":true}`, string(b))
	var back Action
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, act, back)
}

func TestAReasonSaysWhatDecided(t *testing.T) {
	t.Parallel()
	for want, c := range map[string]struct {
		policy Policy
		act    Action
	}{
		"it always asks":            {Policy{Mode: ModeAuto}, patch(Destructive)},
		"auto mode":                 {Policy{Mode: ModeAuto}, patch(Write)},
		"ask mode":                  {Policy{Mode: ModeAsk}, patch(Write)},
		"this account is read-only": {Policy{Mode: ModeReadOnly}, patch(Write)},
		"it is never allowed":       {Policy{Mode: ModeAuto}, patch(Reach)},
	} {
		_, why := c.policy.Decide(c.act)
		assert.Equal(t, want, why, fmt.Sprint(c.act.AccessLevel))
	}
}

func TestAnActionsAccountIsItsFirstScopeSegment(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "dev", Action{Scope: "dev/team-a"}.Account())
	assert.Equal(t, "dev", Action{Scope: "dev"}.Account())
	assert.Empty(t, Action{}.Account())
}

func TestGrantLinesAreTheTwoRulesInWords(t *testing.T) {
	t.Parallel()
	act := patch(Write)
	command, chat := GrantLines(act)
	assert.Equal(t, CommandRule(act).Line(act.AccountName), command)
	assert.Equal(t, GrantRule(act).Line(act.AccountName), chat)
	assert.NotEqual(t, command, chat)
}

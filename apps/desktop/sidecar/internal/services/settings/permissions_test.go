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

package settings

import (
	"testing"
	"time"

	"github.com/amorey/gobus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// newStore is a store over a fresh app.db, closed when the test ends.
func newStore(t *testing.T) *Store {
	t.Helper()
	return openStore(t, appdbtest.Open(t))
}

// openStore is a store over db, closed when the test ends.
func openStore(t *testing.T, db *appdb.DB) *Store {
	t.Helper()
	s, err := Open(db)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

// exec runs q on db's writer.
func exec(t *testing.T, db *appdb.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Write.Exec(q, args...)
	require.NoError(t, err)
}

// seedCluster writes a clusters row: from the kubeconfig, under context, or a
// cloud cluster with none when context is "".
func seedCluster(t *testing.T, db *appdb.DB, id apimeta.ClusterID, context string) {
	t.Helper()
	source, key := "kubeconfig", any(context)
	if context == "" {
		source, key = "cloud", string(id)
	}
	exec(t, db, `INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES (?, ?, ?, 0, 0)`, id, source, key)
}

// storeWithClusters is a store over a fresh app.db holding the clusters dev,
// prod and a cloud cluster with no kube-context.
func storeWithClusters(t *testing.T) (*Store, *appdb.DB) {
	t.Helper()
	db := appdbtest.Open(t)
	seedCluster(t, db, "dev", "dev-eks")
	seedCluster(t, db, "prod", "prod-eu")
	seedCluster(t, db, "cloud", "")
	return openStore(t, db), db
}

func modeOf(t *testing.T, s *Store, target permissions.Target, account string) ModeState {
	t.Helper()
	st, err := s.ModeFor(t.Context(), target, account)
	require.NoError(t, err)
	return st
}

const k8s = permissions.Kubernetes

// An account's mode is its own row, else its target's default row, else ask.
func TestAModeIsItsAccountsThenItsTargetsThenAsk(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()

	assert.Equal(t, ModeState{Mode: permissions.ModeAsk, Source: SourceDefault}, modeOf(t, s, k8s, "dev"))
	assert.Equal(t, ModeState{Mode: permissions.ModeAsk, Source: SourceDefault}, modeOf(t, s, permissions.AWS, "123456789012"))

	require.NoError(t, s.SetMode(ctx, k8s, "*", permissions.ModeAuto))
	require.NoError(t, s.SetMode(ctx, k8s, "*", permissions.ModeAuto), "a second write keeps one default")
	assert.Equal(t, ModeState{Mode: permissions.ModeAuto, Source: SourceDefault}, modeOf(t, s, k8s, "dev"))
	assert.Equal(t, ModeState{Mode: permissions.ModeAuto, Source: SourceDefault}, modeOf(t, s, k8s, "cloud"),
		"a cluster with no kube-context takes the default")
	assert.Equal(t, permissions.ModeAsk, modeOf(t, s, permissions.AWS, "123456789012").Mode, "another target's default is its own")

	require.NoError(t, s.SetMode(ctx, k8s, "prod", permissions.ModeReadOnly))
	require.NoError(t, s.SetMode(ctx, k8s, "prod", permissions.ModeAsk), "a second write changes the one row")
	assert.Equal(t, ModeState{Mode: permissions.ModeAsk, Source: SourceEntry}, modeOf(t, s, k8s, "prod"))
	assert.Equal(t, permissions.ModeAuto, modeOf(t, s, k8s, "dev").Mode, "another cluster keeps the default")

	require.NoError(t, s.SetMode(ctx, permissions.AWS, "123456789012", permissions.ModeAuto))
	assert.Equal(t, ModeState{Mode: permissions.ModeAuto, Source: SourceEntry}, modeOf(t, s, permissions.AWS, "123456789012"))
	assert.Equal(t, ModeState{Mode: permissions.ModeAuto, Source: SourceDefault}, modeOf(t, s, k8s, "123456789012"), "an account is its target's")

	rows, err := s.Modes(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []ModeRow{
		{Target: k8s, Account: "*", Mode: permissions.ModeAuto},
		{Target: k8s, Account: "prod", Mode: permissions.ModeAsk},
		{Target: permissions.AWS, Account: "123456789012", Mode: permissions.ModeAuto},
	}, rows)

	require.NoError(t, s.ClearMode(ctx, k8s, "prod"))
	assert.Equal(t, ModeState{Mode: permissions.ModeAuto, Source: SourceDefault}, modeOf(t, s, k8s, "prod"))
	require.NoError(t, s.ClearMode(ctx, k8s, "prod"), "clearing a mode the account does not have")
	require.NoError(t, s.ClearMode(ctx, k8s, "*"))
	assert.Equal(t, permissions.ModeAsk, modeOf(t, s, k8s, "dev").Mode)
	require.NoError(t, s.ClearMode(ctx, permissions.AWS, "123456789012"))
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM permission_modes`).Scan(&n))
	assert.Zero(t, n, "a cleared mode is no row")
}

// A cloud account is stored and read in lower case, as its classifier spells
// it, so a mode typed in another case still applies. A Kubernetes account is
// a cluster's id, kept as it is.
func TestACloudAccountIsSpelledAsItsClassifierSpellsIt(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	require.NoError(t, s.SetMode(ctx, permissions.GitHub, "Kubetail-Org", permissions.ModeReadOnly))
	assert.Equal(t, ModeState{Mode: permissions.ModeReadOnly, Source: SourceEntry}, modeOf(t, s, permissions.GitHub, "kubetail-org"),
		"the lookup the proxy makes")
	rows, err := s.Modes(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ModeRow{{Target: permissions.GitHub, Account: "kubetail-org", Mode: permissions.ModeReadOnly}}, rows)
	require.NoError(t, s.ClearMode(ctx, permissions.GitHub, "KUBETAIL-ORG"))
	assert.Equal(t, SourceDefault, modeOf(t, s, permissions.GitHub, "kubetail-org").Source)

	seedCluster(t, db, "Mixed", "mixed-ctx")
	require.NoError(t, s.SetMode(ctx, k8s, "Mixed", permissions.ModeAuto))
	assert.Equal(t, SourceEntry, modeOf(t, s, k8s, "Mixed").Source)
	assert.Equal(t, SourceDefault, modeOf(t, s, k8s, "mixed").Source, "a cluster's id is opaque")
}

// A mode Kubetail cannot use is read-only: in an account's row for that
// account, in the default's for every account that takes it.
func TestAModeKubetailCannotUseIsReadOnly(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	exec(t, db, `INSERT INTO permission_modes (target, account, mode, created_at, updated_at) VALUES ('kubernetes', 'prod', 'sometimes', 0, 0)`)
	assert.Equal(t, ModeState{Mode: permissions.ModeReadOnly, Source: SourceEntry}, modeOf(t, s, k8s, "prod"))
	assert.Equal(t, permissions.ModeAsk, modeOf(t, s, k8s, "dev").Mode, "another cluster keeps the default")

	exec(t, db, `INSERT INTO permission_modes (target, account, mode, created_at, updated_at) VALUES ('aws', '*', '', 0, 0)`)
	assert.Equal(t, ModeState{Mode: permissions.ModeReadOnly, Source: SourceDefault}, modeOf(t, s, permissions.AWS, "123456789012"))
	rows, err := s.Modes(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		assert.Equal(t, permissions.ModeReadOnly, r.Mode, "%+v", r)
	}
}

// A Kubernetes mode belongs to a cluster Settings lists: one with a
// kube-context that is not awaiting deletion. A cloud account is any
// non-empty literal with no /.
func TestAModeIsRefusedForAnAccountSettingsCannotList(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	exec(t, db, `UPDATE clusters SET delete_requested_at = 1 WHERE id = 'prod'`)

	assert.ErrorIs(t, s.SetMode(ctx, k8s, "gone", permissions.ModeAuto), ErrClusterGone)
	assert.ErrorIs(t, s.SetMode(ctx, k8s, "prod", permissions.ModeAuto), ErrClusterGone, "a cluster awaiting deletion")
	assert.ErrorIs(t, s.SetMode(ctx, k8s, "cloud", permissions.ModeAuto), ErrNoKubeContext)
	assert.ErrorIs(t, s.ClearMode(ctx, k8s, "gone"), ErrClusterGone)
	for name, err := range map[string]error{
		"an empty account":     s.SetMode(ctx, permissions.AWS, "", permissions.ModeAuto),
		"a / in an account":    s.SetMode(ctx, permissions.AWS, "a/b", permissions.ModeAuto),
		"a glob in an account": s.SetMode(ctx, permissions.GitHub, "kubetail*", permissions.ModeAuto),
		"no target":            s.SetMode(ctx, "", "a", permissions.ModeAuto),
		"an unknown target":    s.SetMode(ctx, "cluster", "a", permissions.ModeAuto),
		"an unknown mode":      s.SetMode(ctx, permissions.AWS, "a", "sometimes"),
		"clear no target":      s.ClearMode(ctx, "cluster", "a"),
		"clear no account":     s.ClearMode(ctx, permissions.AWS, ""),
	} {
		var r ModeRefusal
		assert.ErrorAs(t, err, &r, name)
	}
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT count(*) FROM permission_modes`).Scan(&n))
	assert.Zero(t, n)
}

// A read of the modes that fails is an error, which a decision reads as
// read-only.
func TestAModesReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	exec(t, db, `DROP TABLE permission_modes`)

	_, err := s.ModeFor(t.Context(), k8s, "dev")
	assert.Error(t, err)
	_, err = s.Modes(t.Context())
	assert.Error(t, err)
}

func rules(t *testing.T, s *Store) []permissions.Rule {
	t.Helper()
	rules, err := s.Rules(t.Context())
	require.NoError(t, err)
	return rules
}

// rule is an allow of writes in scope on Kubernetes, naming everything else
// by "*".
func rule(scope string) permissions.Rule {
	return permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.Write, Principal: "*", Target: "kubernetes",
		Scope: scope, Service: "*", Operation: "*", Resource: "*"}
}

// The always rules are added in order, every field kept, a rule already held
// is not written twice, and one is removed by id.
func TestAlwaysRulesAreAddedOnceAndRemoved(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	ctx := t.Context()
	assert.Empty(t, rules(t, s))

	deny := permissions.Rule{Effect: permissions.Deny, AccessLevel: permissions.Destructive, Principal: "subagent", Target: "*",
		Scope: "*/team-a", Service: "apps", Operation: "delete", Resource: "deployments"}
	allow := permissions.Rule{ID: "mine", Effect: permissions.Allow, AccessLevel: permissions.DataRead, Principal: "chat", Target: "aws",
		Scope: "123456789012/**", Service: "", Operation: "GetObject", Resource: "object"}
	added, err := s.AddRule(ctx, deny)
	require.NoError(t, err)
	require.NotEmpty(t, added.ID, "a rule with no id gets one")
	deny.ID = added.ID
	_, err = s.AddRule(ctx, allow)
	require.NoError(t, err)
	assert.Equal(t, []permissions.Rule{deny, allow}, rules(t, s))

	again := deny
	again.ID = ""
	held, err := s.AddRule(ctx, again)
	require.NoError(t, err)
	assert.Equal(t, deny, held, "the rule already held answers")
	assert.Len(t, rules(t, s), 2)

	require.NoError(t, s.RemoveRule(ctx, deny.ID))
	assert.Equal(t, []permissions.Rule{allow}, rules(t, s))
	assert.ErrorIs(t, s.RemoveRule(ctx, deny.ID), ErrNoRule)
}

// stamps is the created_at and updated_at of the always rule id.
func stamps(t *testing.T, db *appdb.DB, id string) (created, updated int64) {
	t.Helper()
	require.NoError(t, db.Read.QueryRow(`SELECT created_at, updated_at FROM permission_rules WHERE id = ?`, id).Scan(&created, &updated))
	return created, updated
}

// An edit keeps the rule's id and its place, changes every other field, and
// moves updated_at alone; an edit that changes nothing writes nothing.
func TestAnAlwaysRuleIsEditedInPlace(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	s.now = func() time.Time { return time.UnixMilli(1) }
	first, err := s.AddRule(ctx, rule("dev/team-a"))
	require.NoError(t, err)
	second, err := s.AddRule(ctx, rule("prod/team-b"))
	require.NoError(t, err)

	s.now = func() time.Time { return time.UnixMilli(2) }
	edit := permissions.Rule{Effect: permissions.Ask, AccessLevel: permissions.DataRead, Principal: "chat", Target: "aws",
		Scope: "123456789012/**", Service: "s3", Operation: "GetObject", Resource: "object"}
	edited, err := s.UpdateRule(ctx, first.ID, edit)
	require.NoError(t, err)
	edit.ID = first.ID
	assert.Equal(t, edit, edited)
	assert.Equal(t, []permissions.Rule{edit, second}, rules(t, s), "the edited rule keeps its place")
	created, updated := stamps(t, db, first.ID)
	assert.Equal(t, [2]int64{1, 2}, [2]int64{created, updated})

	s.now = func() time.Time { return time.UnixMilli(3) }
	same, err := s.UpdateRule(ctx, first.ID, edit)
	require.NoError(t, err)
	assert.Equal(t, edit, same)
	_, updated = stamps(t, db, first.ID)
	assert.Equal(t, int64(2), updated, "an edit that changes nothing writes nothing")
}

// An edit into a rule another one already is removes the edited rule and
// answers the one held.
func TestAnEditIntoAHeldRuleLeavesTheHeldOne(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	ctx := t.Context()
	first, err := s.AddRule(ctx, rule("dev/team-a"))
	require.NoError(t, err)
	second, err := s.AddRule(ctx, rule("prod/team-b"))
	require.NoError(t, err)

	held, err := s.UpdateRule(ctx, first.ID, rule("prod/team-b"))
	require.NoError(t, err)
	assert.Equal(t, second, held)
	assert.Equal(t, []permissions.Rule{second}, rules(t, s))
}

// An edit is refused as an add is, and for an id no always rule has; a
// refused edit leaves the rule as it was.
func TestAnEditIsRefusedAsAnAddIs(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	held, err := s.AddRule(ctx, rule("dev/team-a"))
	require.NoError(t, err)

	_, err = s.UpdateRule(ctx, "gone", rule("dev/team-b"))
	assert.ErrorIs(t, err, ErrNoRule)
	_, err = s.UpdateRule(ctx, held.ID, rule("gone/team-a"))
	assert.ErrorIs(t, err, ErrClusterGone)
	_, err = s.UpdateRule(ctx, held.ID, rule("cloud/team-a"))
	assert.ErrorIs(t, err, ErrNoKubeContext)
	exec(t, db, `INSERT INTO chats (id, cluster_id, mode, created_at, updated_at) VALUES ('h1', 'dev', 'chat', 0, 0)`)
	exec(t, db, `INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
		VALUES ('theirs', 'h1', 'allow', 'write', '*', 'kubernetes', 'dev', '*', '*', '*', 0, 0)`)
	_, err = s.UpdateRule(ctx, "theirs", rule("dev/team-b"))
	assert.ErrorIs(t, err, ErrNoRule, "a chat's rule is not an always rule")
	assert.Equal(t, []permissions.Rule{held}, rules(t, s))
}

// A row Go cannot read is edited into a rule it can, by its stand-ins' id.
func TestARowGoCannotReadIsEditedByItsID(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	exec(t, db, `INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
		VALUES ('x1', NULL, 'permit', 'write', '*', 'kubernetes', 'dev', '*', '*', '*', 0, 0)`)
	edited, err := s.UpdateRule(t.Context(), "x1", rule("dev/team-a"))
	require.NoError(t, err)
	assert.Equal(t, []permissions.Rule{edited}, rules(t, s))
}

// A rule's scope is stored as the classifiers spell it: lower case off
// Kubernetes, a cluster's id as it is.
func TestARuleScopeIsSpelledAsItsClassifierSpellsIt(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	seedCluster(t, db, "Mixed", "mixed-ctx")
	deny := permissions.Rule{Effect: permissions.Deny, AccessLevel: permissions.Write, Principal: "*", Target: "github",
		Scope: "Kubetail-Org/**", Service: "*", Operation: "*", Resource: "*"}
	added, err := s.AddRule(ctx, deny)
	require.NoError(t, err)
	assert.Equal(t, "kubetail-org/**", added.Scope)
	kube, err := s.AddRule(ctx, rule("Mixed/web"))
	require.NoError(t, err)
	assert.Equal(t, "Mixed/web", kube.Scope)

	deny.Scope = "Kubetail-Org/Kubetail"
	edited, err := s.UpdateRule(ctx, added.ID, deny)
	require.NoError(t, err)
	assert.Equal(t, "kubetail-org/kubetail", edited.Scope)
	assert.Equal(t, []string{"kubetail-org/kubetail", "Mixed/web"}, []string{rules(t, s)[0].Scope, rules(t, s)[1].Scope})
}

// A Kubernetes rule naming a cluster names one Settings lists, as a mode
// does, and goes with its cluster.
func TestARuleNamesAClusterSettingsListsAndGoesWithIt(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	_, err := s.AddRule(ctx, rule("gone"))
	assert.ErrorIs(t, err, ErrClusterGone)
	_, err = s.AddRule(ctx, rule("cloud/team-a"))
	assert.ErrorIs(t, err, ErrNoKubeContext)
	exec(t, db, `UPDATE clusters SET delete_requested_at = 1 WHERE id = 'prod'`)
	_, err = s.AddRule(ctx, rule("prod/**"))
	assert.ErrorIs(t, err, ErrClusterGone, "a cluster awaiting deletion")
	assert.Empty(t, rules(t, s))

	_, err = s.AddRule(ctx, rule("dev/team-a"))
	require.NoError(t, err)
	deny := rule("*/team-a")
	deny.Effect = permissions.Deny
	_, err = s.AddRule(ctx, deny)
	require.NoError(t, err, "a rule for every cluster names none")
	exec(t, db, `DELETE FROM clusters WHERE id = 'dev'`)
	deny.ID = rules(t, s)[0].ID
	assert.Equal(t, []permissions.Rule{deny}, rules(t, s), "a deleted cluster's rules go with it")
}

// badRules are rules Kubetail cannot apply, each with the reason a write is
// refused with.
var badRules = map[string]struct {
	edit   func(*permissions.Rule)
	reason string
}{
	"the refused id":      {func(r *permissions.Rule) { r.ID, r.Effect = "refused", permissions.Deny }, "has the id Kubetail keeps for its own rule"},
	"the refused-data id": {func(r *permissions.Rule) { r.ID, r.Effect = "refused-data", permissions.Deny }, "has the id Kubetail keeps for its own rule"},
	"an unknown effect":   {func(r *permissions.Rule) { r.Effect = "permit" }, "has an effect other than allow, deny or ask"},
	"an unknown level":    {func(r *permissions.Rule) { r.AccessLevel = "upstream-write" }, "names an access level Kubetail does not know"},
	"no level":            {func(r *permissions.Rule) { r.AccessLevel = "" }, "names an access level Kubetail does not know"},
	"a read":              {func(r *permissions.Rule) { r.AccessLevel = permissions.Read }, "names reads or reach, which no rule decides"},
	"reach":               {func(r *permissions.Rule) { r.Effect, r.AccessLevel = permissions.Deny, permissions.Reach }, "names reads or reach, which no rule decides"},
	"an unknown principal": {func(r *permissions.Rule) { r.Principal = "user" },
		"names a principal other than anyone, chat or subagent: the boundary alone decides the monitor and the console"},
	"the monitor": {func(r *permissions.Rule) { r.Effect, r.Principal = permissions.Deny, "monitor" },
		"names a principal other than anyone, chat or subagent: the boundary alone decides the monitor and the console"},
	"no principal": {func(r *permissions.Rule) { r.Principal = "" },
		"names a principal other than anyone, chat or subagent: the boundary alone decides the monitor and the console"},
	"an unknown target":       {func(r *permissions.Rule) { r.Target = "cluster" }, "names a target Kubetail does not know"},
	"no target":               {func(r *permissions.Rule) { r.Target = "" }, "names a target Kubetail does not know"},
	"an empty scope":          {func(r *permissions.Rule) { r.Scope = "" }, "has an empty scope, operation or resource"},
	"an empty operation":      {func(r *permissions.Rule) { r.Operation = "" }, "has an empty scope, operation or resource"},
	"an empty resource":       {func(r *permissions.Rule) { r.Resource = "" }, "has an empty scope, operation or resource"},
	"a ** inside a scope":     {func(r *permissions.Rule) { r.Scope = "dev/**/x" }, "has a ** that is not a path's last segment"},
	"a ** inside a resource":  {func(r *permissions.Rule) { r.Resource = "**/scale" }, "has a ** that is not a path's last segment"},
	"an allow of destructive": {func(r *permissions.Rule) { r.AccessLevel = permissions.Destructive }, "allows a destructive change, which always asks"},
	"an allow for a subagent": {func(r *permissions.Rule) { r.Principal = "subagent" }, "allows for a subagent, which its parent's verdict would never let win"},
	"an allow on every target": {func(r *permissions.Rule) { r.Target = "*" },
		"allows on every target or account, which only a deny or ask rule may"},
	"an allow on every account": {func(r *permissions.Rule) { r.Scope = "*/team-a" },
		"allows on every target or account, which only a deny or ask rule may"},
	"an allow on accounts by glob": {func(r *permissions.Rule) { r.Scope = "de?/team-a" },
		"allows on every target or account, which only a deny or ask rule may"},
	"an allow everywhere": {func(r *permissions.Rule) { r.Scope = "**" },
		"allows on every target or account, which only a deny or ask rule may"},
}

// badRule is a valid allow, edited by edit.
func badRule(edit func(*permissions.Rule)) permissions.Rule {
	r := rule("dev/team-a")
	r.ID = "b"
	edit(&r)
	return r
}

func TestTheGoCheckRefusesWhatNoRuleMayHold(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	for name, c := range badRules {
		r := badRule(c.edit)
		err := CheckRule(r)
		var refusal RuleRefusal
		require.ErrorAs(t, err, &refusal, name)
		assert.Equal(t, c.reason, string(refusal), name)
		_, err = s.AddRule(t.Context(), r)
		assert.ErrorAs(t, err, &refusal, name)
		_, err = s.UpdateRule(t.Context(), "b", r)
		assert.ErrorAs(t, err, &refusal, name)
	}
	assert.Empty(t, rules(t, s))
	for _, r := range []permissions.Rule{
		rule("dev"), rule("dev/**"), rule(`dev/team-*`),
		{Effect: permissions.Ask, AccessLevel: permissions.AccessControl, Principal: "*", Target: "*", Scope: "**", Service: "*", Operation: "*", Resource: "*"},
		{Effect: permissions.Deny, AccessLevel: permissions.Write, Principal: "subagent", Target: "gcp", Scope: "*", Service: "", Operation: "x", Resource: "a/**"},
		{Effect: permissions.Allow, AccessLevel: permissions.AccessControl, Principal: "chat", Target: "azure", Scope: `sub\*/rg`, Service: "*", Operation: "*", Resource: "*"},
	} {
		assert.NoError(t, CheckRule(r), "%+v", r)
	}
}

// A rule a row holds that the Go check refuses reads as both refusals in its
// place, so a row Kubetail cannot read denies rather than allows.
func TestARowGoCannotReadDenies(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	good, err := s.AddRule(t.Context(), rule("dev/team-a"))
	require.NoError(t, err)
	for _, c := range []string{
		`('x1', NULL, 'permit', 'write', '*', 'kubernetes', 'dev', '*', '*', '*', 9000000000001, 0)`,
		`('x2', NULL, 'allow', 'write', '*', '*', '**', '*', '*', '*', 9000000000002, 0)`,
		`('x3', NULL, 'allow', 'upstream-write', '*', 'kubernetes', 'dev', '*', '*', '*', 9000000000003, 0)`,
	} {
		exec(t, db, `INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at) VALUES `+c)
	}
	want := []permissions.Rule{good}
	for _, id := range []string{"x1", "x2", "x3"} {
		for _, standIn := range permissions.Unread() {
			standIn.ID = id
			want = append(want, standIn)
		}
	}
	assert.Equal(t, want, rules(t, s))

	// The stand-ins carry their row's id, so the user can remove the row.
	for _, id := range []string{"x1", "x2", "x3"} {
		require.NoError(t, s.RemoveRule(t.Context(), id))
	}
	assert.Equal(t, []permissions.Rule{good}, rules(t, s))
}

// A stand-in for a row Kubetail cannot read is no rule the user wrote: a deny
// equal to one is written, and survives the row's removal, and editing the
// row into one repairs it.
func TestAStandInIsNoRuleAWriteMatches(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	bad := func(id string) {
		exec(t, db, `INSERT INTO permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at, updated_at)
			VALUES (?, NULL, 'permit', 'write', '*', '*', '*', '*', '*', '*', 0, 0)`, id)
	}
	denyWrites := permissions.Refused
	denyWrites.ID = ""

	bad("x1")
	added, err := s.AddRule(ctx, denyWrites)
	require.NoError(t, err)
	assert.NotEqual(t, "x1", added.ID, "the deny is written, not matched to the stand-in")
	require.NoError(t, s.RemoveRule(ctx, "x1"))
	assert.Equal(t, []permissions.Rule{added}, rules(t, s), "the deny outlives the row")

	require.NoError(t, s.RemoveRule(ctx, added.ID))
	bad("x2")
	edited, err := s.UpdateRule(ctx, "x2", denyWrites)
	require.NoError(t, err)
	assert.Equal(t, "x2", edited.ID)
	assert.Equal(t, []permissions.Rule{edited}, rules(t, s), "the row is repaired, not left as it was")
}

// A rule round-trips through its row, every field kept.
func TestARuleRoundTripsThroughItsRow(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	r := permissions.Rule{ID: "r1", Effect: permissions.Ask, AccessLevel: permissions.DataRead, Principal: "subagent", Target: "github",
		Scope: `kubetail\*/**`, Service: "api", Operation: "get", Resource: "contents"}
	exec(t, db, `INSERT INTO permission_rules (`+RuleColumns+`, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)`, RuleValues(r)...)
	rows, err := db.Read.Query(`SELECT ` + RuleColumns + ` FROM permission_rules`)
	require.NoError(t, err)
	got, err := ScanRules(rows)
	require.NoError(t, err)
	assert.Equal(t, []permissions.Rule{r}, got)
	assert.Equal(t, []permissions.Rule{r}, rules(t, s))
}

// A read of the rules that fails is an error, which a decision reads as
// permissions.Unread.
func TestARulesReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	exec(t, db, `DROP TABLE permission_rules`)
	_, err := s.Rules(t.Context())
	assert.Error(t, err)
	_, err = s.AddRule(t.Context(), rule("dev"))
	assert.Error(t, err)
	_, err = s.UpdateRule(t.Context(), "r1", rule("dev"))
	assert.Error(t, err)
}

func folderGrants(t *testing.T, s *Store) []FolderGrant {
	t.Helper()
	grants, err := s.FolderGrants(t.Context())
	require.NoError(t, err)
	return grants
}

// An always grant of a folder granted already keeps its id and changes its
// access, and is revoked by id.
func TestAlwaysFolderGrantsAreGrantedChangedAndRevoked(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()

	require.NoError(t, s.GrantFolder(ctx, "/srv/code", false))
	require.NoError(t, s.GrantFolder(ctx, "/srv/data", true))
	grants := folderGrants(t, s)
	require.Len(t, grants, 2)
	assert.Equal(t, FolderGrant{ID: grants[0].ID, Path: "/srv/code"}, grants[0])
	assert.Equal(t, FolderGrant{ID: grants[1].ID, Path: "/srv/data", Write: true}, grants[1])

	require.NoError(t, s.GrantFolder(ctx, "/srv/code", true))
	again := folderGrants(t, s)
	assert.Equal(t, FolderGrant{ID: grants[0].ID, Path: "/srv/code", Write: true}, again[0], "the first id, read-write")

	require.NoError(t, s.RevokeFolder(ctx, grants[1].ID))
	assert.Len(t, folderGrants(t, s), 1)
	assert.ErrorIs(t, s.RevokeFolder(ctx, grants[1].ID), ErrNoGrant)

	exec(t, db, `DROP TABLE folder_grants`)
	_, err := s.FolderGrants(ctx)
	assert.Error(t, err, "a read that fails is an error, not an empty list")
}

// Each write of the modes, the always rules or the always folder grants
// pings once it commits; a refused one pings nothing.
func TestEachPermissionWritePublishes(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	ctx := t.Context()
	rx := s.ListenForPermissionChanges()
	t.Cleanup(rx.Close)
	published := func(what string, err error) {
		t.Helper()
		require.NoError(t, err, what)
		_, err = rx.TryRecv()
		require.NoError(t, err, what)
	}

	published("the default mode", s.SetMode(ctx, k8s, "*", permissions.ModeAuto))
	published("a cluster's mode", s.SetMode(ctx, k8s, "dev", permissions.ModeAuto))
	published("a cluster's mode cleared", s.ClearMode(ctx, k8s, "dev"))
	added, err := s.AddRule(ctx, rule("dev"))
	published("a rule", err)
	published("a rule removed", s.RemoveRule(ctx, added.ID))
	published("a folder grant", s.GrantFolder(ctx, "/srv/code", false))
	published("a folder grant revoked", s.RevokeFolder(ctx, folderGrants(t, s)[0].ID))

	assert.ErrorIs(t, s.SetMode(ctx, k8s, "gone", permissions.ModeAuto), ErrClusterGone)
	_, err = s.AddRule(ctx, badRule(func(r *permissions.Rule) { r.AccessLevel = permissions.Destructive }))
	assert.Error(t, err)
	_, err = rx.TryRecv()
	assert.ErrorIs(t, err, gobus.ErrEmpty, "a refused write pinged")
}

// looseTable replaces table with one of the same columns and no constraints,
// so a test can store a value no read of the real table meets.
func looseTable(t *testing.T, db *appdb.DB, table, columns string) {
	t.Helper()
	for _, q := range []string{`DROP TABLE ` + table, `CREATE TABLE ` + table + ` (` + columns + `)`} {
		exec(t, db, q)
	}
}

// A read that fails after some rows were read is a failed read: the rows
// already read are not used.
func TestAReadThatFailsPartwayAnswersNone(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	looseTable(t, db, "permission_rules", `id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at`)
	looseTable(t, db, "folder_grants", `id, chat_id, folder, access, created_at`)
	looseTable(t, db, "permission_modes", `target, account, mode, created_at, updated_at`)
	for _, q := range []string{
		`INSERT INTO permission_rules VALUES ('a', NULL, 'deny', 'write', '*', '*', '*', '*', '*', '*', 1), ('b', NULL, 'deny', 'write', '*', '*', NULL, '*', '*', '*', 9000000000002)`,
		`INSERT INTO folder_grants VALUES ('a', NULL, '/srv/code', 'read', 1), ('b', NULL, NULL, 'read', 2)`,
		`INSERT INTO permission_modes VALUES ('kubernetes', '*', 'ask', 1, 1), ('kubernetes', 'dev', NULL, 2, 2)`,
	} {
		exec(t, db, q)
	}
	_, err := s.ModeFor(t.Context(), k8s, "dev")
	assert.Error(t, err)
	modes, err := s.Modes(t.Context())
	assert.Error(t, err)
	assert.Nil(t, modes)

	read, err := s.Rules(t.Context())
	assert.Error(t, err)
	assert.Nil(t, read)
	grants, err := s.FolderGrants(t.Context())
	assert.Error(t, err)
	assert.Nil(t, grants)
}

// A write that fails reaches its caller, and publishes nothing.
func TestAWriteThatFailsReachesTheCaller(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	require.NoError(t, s.SetMode(ctx, k8s, "dev", permissions.ModeAuto))
	require.NoError(t, s.GrantFolder(ctx, "/srv/code", false))
	grant := folderGrants(t, s)[0]
	for _, q := range []string{
		`CREATE TRIGGER refuse_mode_write BEFORE INSERT ON permission_modes BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_mode_update BEFORE UPDATE ON permission_modes BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_mode_delete BEFORE DELETE ON permission_modes BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_grant_write BEFORE INSERT ON folder_grants BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_rule_write BEFORE INSERT ON permission_rules BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_grant_delete BEFORE DELETE ON folder_grants BEGIN SELECT RAISE(ABORT, 'refused'); END`,
	} {
		exec(t, db, q)
	}
	rx := s.ListenForPermissionChanges()
	t.Cleanup(rx.Close)

	for name, err := range map[string]error{
		"the default mode": s.SetMode(ctx, k8s, "*", permissions.ModeReadOnly),
		"a cluster's mode": s.SetMode(ctx, k8s, "prod", permissions.ModeReadOnly),
		"a mode cleared":   s.ClearMode(ctx, k8s, "dev"),
		"a folder grant":   s.GrantFolder(ctx, "/srv/data", true),
		"a grant revoked":  s.RevokeFolder(ctx, grant.ID),
		"a rule": func() error {
			_, err := s.AddRule(ctx, rule("dev"))
			return err
		}(),
	} {
		assert.ErrorContains(t, err, "refused", name)
	}
	_, err := rx.TryRecv()
	assert.ErrorIs(t, err, gobus.ErrEmpty, "a refused write pinged")
}

// A mode's write that cannot look its cluster up writes nothing.
func TestAModeWhoseClusterCannotBeReadIsAnError(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	exec(t, db, `ALTER TABLE clusters RENAME TO clusters_gone`)
	err := s.SetMode(t.Context(), k8s, "dev", permissions.ModeAuto)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrClusterGone)
}

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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// ModeSource is where an account's mode comes from.
type ModeSource string

const (
	// SourceEntry is the account's own row.
	SourceEntry ModeSource = "entry"
	// SourceDefault is its target's default.
	SourceDefault ModeSource = "default"
)

// ModeState is one account's mode and where it comes from.
type ModeState struct {
	Mode   permissions.Mode
	Source ModeSource
}

// ModeRow is one row of the modes: an account's, or with Account "*" its
// target's default.
type ModeRow struct {
	Target  permissions.Target
	Account string
	Mode    permissions.Mode
}

var (
	// ErrClusterGone is a mode or rule for a cluster that is gone or awaiting
	// deletion.
	ErrClusterGone = errors.New("settings: the cluster is gone")
	// ErrNoKubeContext is a mode or rule for a cluster with no kube-context,
	// which Settings does not list.
	ErrNoKubeContext = errors.New("settings: only a cluster with a kube-context has a mode or rule of its own")
	// ErrNoRule is a rule id the always rules do not hold.
	ErrNoRule = errors.New("settings: no rule has this id")
	// ErrNoGrant is an id no always folder grant has.
	ErrNoGrant = errors.New("settings: no folder grant has this id")
)

// RuleRefusal is why a rule cannot be written, in the user's words.
type RuleRefusal string

func (r RuleRefusal) Error() string { return string(r) }

// ModeRefusal is why a mode cannot be written, in the user's words.
type ModeRefusal string

func (r ModeRefusal) Error() string { return string(r) }

// FolderGrant is one folder grant as stored: its folder, absolute and
// resolved, and whether it grants writes.
type FolderGrant struct {
	ID    string
	Path  string
	Write bool
}

// defaultAccount is the account a target's default mode is stored under.
const defaultAccount = "*"

// readMode is a stored mode: read-only for one Kubetail cannot use.
func readMode(stored string) permissions.Mode {
	if mode := permissions.Mode(stored); slices.Contains(permissions.Modes, mode) {
		return mode
	}
	return permissions.ModeReadOnly
}

// ModeFor is the mode of target's account and where it comes from: its own
// row, else the target's default row, else ask. A mode Kubetail cannot use is
// read-only. A read that fails is an error, which a decision reads as
// read-only.
func (s *Store) ModeFor(ctx context.Context, target permissions.Target, account string) (ModeState, error) {
	account = canonicalScope(string(target), account)
	rows, err := s.stmts.Stmts().Query(ctx, stmtSelectAccountModes, string(target), account)
	if err != nil {
		return ModeState{}, fmt.Errorf("select modes: %w", err)
	}
	found, err := scanAll(rows, func() (ModeRow, error) {
		var r ModeRow
		var mode string
		if err := rows.Scan(&r.Account, &mode); err != nil {
			return ModeRow{}, fmt.Errorf("scan mode: %w", err)
		}
		r.Mode = readMode(mode)
		return r, nil
	})
	if err != nil {
		return ModeState{}, err
	}
	st := ModeState{Mode: permissions.ModeAsk, Source: SourceDefault}
	for _, r := range found {
		if r.Account == account {
			return ModeState{Mode: r.Mode, Source: SourceEntry}, nil
		}
		st.Mode = r.Mode
	}
	return st, nil
}

// Modes is every row of the modes, each mode Kubetail cannot use read-only.
func (s *Store) Modes(ctx context.Context) ([]ModeRow, error) {
	rows, err := s.stmts.Stmts().Query(ctx, stmtSelectModes)
	if err != nil {
		return nil, fmt.Errorf("select modes: %w", err)
	}
	return scanAll(rows, func() (ModeRow, error) {
		var r ModeRow
		var target, mode string
		if err := rows.Scan(&target, &r.Account, &mode); err != nil {
			return ModeRow{}, fmt.Errorf("scan mode: %w", err)
		}
		r.Target, r.Mode = permissions.Target(target), readMode(mode)
		return r, nil
	})
}

// SetMode sets the mode of target's account, "*" for the target's default.
// A Kubernetes account is a cluster Settings lists: ErrClusterGone for one
// that is gone or awaiting deletion, ErrNoKubeContext for one with no
// kube-context. A cloud account is any literal with no /, stored in lower
// case. Anything else is a ModeRefusal.
func (s *Store) SetMode(ctx context.Context, target permissions.Target, account string, mode permissions.Mode) error {
	account = canonicalScope(string(target), account)
	if !slices.Contains(permissions.Modes, mode) {
		return ModeRefusal(fmt.Sprintf("no mode is named %q", mode))
	}
	if err := checkAccount(target, account); err != nil {
		return err
	}
	return s.write(ctx, appdb.KeyPermissions, func(st stmts) error {
		if target == permissions.Kubernetes && account != defaultAccount {
			if err := requireKubeContext(ctx, st, apimeta.ClusterID(account)); err != nil {
				return err
			}
		}
		if _, err := st.Exec(ctx, stmtUpsertMode, string(target), account, string(mode), s.now().UnixMilli()); err != nil {
			return fmt.Errorf("write mode: %w", err)
		}
		return nil
	})
}

// ClearMode removes the mode of target's account, putting it back on the
// target's default, or the default back on ask: ErrClusterGone for a
// Kubernetes account that is gone or awaiting deletion.
func (s *Store) ClearMode(ctx context.Context, target permissions.Target, account string) error {
	account = canonicalScope(string(target), account)
	if err := checkAccount(target, account); err != nil {
		return err
	}
	return s.write(ctx, appdb.KeyPermissions, func(st stmts) error {
		if target == permissions.Kubernetes && account != defaultAccount {
			if _, err := clusterHasKubeContext(ctx, st, apimeta.ClusterID(account)); err != nil {
				return err
			}
		}
		if _, err := st.Exec(ctx, stmtDeleteMode, string(target), account); err != nil {
			return fmt.Errorf("delete mode: %w", err)
		}
		return nil
	})
}

// canonicalScope is scope, or an account, as the classifiers spell it for
// target: lower case, but for Kubernetes, whose account is a cluster's record
// id. A rule or a mode spelled otherwise would never match.
func canonicalScope(target, scope string) string {
	if target == string(permissions.Kubernetes) {
		return scope
	}
	return strings.ToLower(scope)
}

// checkAccount refuses a target Kubetail does not know, and an account that is
// neither "*" nor a non-empty literal with no /.
func checkAccount(target permissions.Target, account string) error {
	if _, ok := permissions.TargetNamed(string(target)); !ok {
		return ModeRefusal(fmt.Sprintf("no target is named %q", target))
	}
	if account == defaultAccount {
		return nil
	}
	if _, ok := permissions.LiteralValue(account); account == "" || strings.Contains(account, "/") || !ok {
		return ModeRefusal("an account is one name, with no / and no * or ?")
	}
	return nil
}

// Rules is the always rules in the order written, each row CheckRule refuses
// read as permissions.Unread in its place. A read that fails is an error,
// which a decision reads as permissions.Unread.
func (s *Store) Rules(ctx context.Context) ([]permissions.Rule, error) {
	return selectRules(ctx, s.stmts.Stmts())
}

// AddRule writes r as an always rule, under a fresh id when it has none and
// its scope as canonicalScope spells it, and answers it as written: a RuleRefusal for a rule Kubetail cannot apply,
// ErrClusterGone or ErrNoKubeContext for a Kubernetes rule naming a cluster
// as SetMode refuses it, and the rule held already, unwritten, when the rules
// hold the same one.
func (s *Store) AddRule(ctx context.Context, r permissions.Rule) (permissions.Rule, error) {
	r.Scope = canonicalScope(r.Target, r.Scope)
	if err := CheckRule(r); err != nil {
		return permissions.Rule{}, err
	}
	err := s.write(ctx, appdb.KeyPermissions, func(st stmts) error {
		if cluster, ok := ruleCluster(r); ok {
			if err := requireKubeContext(ctx, st, cluster); err != nil {
				return err
			}
		}
		held, err := selectRuleRows(ctx, st)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(held, r.Same); i >= 0 {
			r = held[i]
			return errUnchanged
		}
		if r.ID == "" {
			r.ID = appdb.NewID()
		}
		return insertRule(ctx, st, r, s.now().UnixMilli())
	})
	if err != nil {
		return permissions.Rule{}, err
	}
	return r, nil
}

// ruleCluster is the cluster a Kubernetes rule names, false for a rule naming
// none: another target's, or one whose first scope segment is a pattern.
func ruleCluster(r permissions.Rule) (apimeta.ClusterID, bool) {
	if r.Target != string(permissions.Kubernetes) {
		return "", false
	}
	cluster, ok := r.Account()
	return apimeta.ClusterID(cluster), ok
}

// UpdateRule replaces the always rule id with r, keeping its id and its place,
// and answers it as written. It refuses as AddRule does, and answers
// ErrNoRule when no always rule has id. When another always rule is the same
// as r, id is removed and that rule answered.
func (s *Store) UpdateRule(ctx context.Context, id string, r permissions.Rule) (permissions.Rule, error) {
	r.Scope = canonicalScope(r.Target, r.Scope)
	if err := CheckRule(r); err != nil {
		return permissions.Rule{}, err
	}
	r.ID = id
	err := s.write(ctx, appdb.KeyPermissions, func(st stmts) error {
		if cluster, ok := ruleCluster(r); ok {
			if err := requireKubeContext(ctx, st, cluster); err != nil {
				return err
			}
		}
		held, err := selectRuleRows(ctx, st)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(held, func(h permissions.Rule) bool { return h.ID == id })
		if i < 0 {
			return ErrNoRule
		}
		if held[i] == r {
			return errUnchanged
		}
		if j := slices.IndexFunc(held, func(h permissions.Rule) bool { return h.ID != id && h.Same(r) }); j >= 0 {
			r = held[j]
			return execOne(ctx, st, stmtDeleteRule, ErrNoRule, id)
		}
		return execOne(ctx, st, stmtUpdateRule, ErrNoRule, append(RuleValues(r)[1:], s.now().UnixMilli(), id)...)
	})
	if err != nil {
		return permissions.Rule{}, err
	}
	return r, nil
}

// RemoveRule removes the always rule id: ErrNoRule when none has it.
func (s *Store) RemoveRule(ctx context.Context, id string) error {
	return s.write(ctx, appdb.KeyPermissions, func(st stmts) error { return execOne(ctx, st, stmtDeleteRule, ErrNoRule, id) })
}

// FolderGrants is the always folder grants in the order written, unchecked: a
// grant is checked where it is read. A read that fails is an error.
func (s *Store) FolderGrants(ctx context.Context) ([]FolderGrant, error) {
	return selectFolderGrants(ctx, s.stmts.Stmts())
}

// GrantFolder writes an always grant of path, which its caller has checked. A
// folder granted already keeps its grant's id and changes its access.
func (s *Store) GrantFolder(ctx context.Context, path string, write bool) error {
	return s.write(ctx, appdb.KeyPermissions, func(st stmts) error {
		return upsertFolderGrant(ctx, st, appdb.NewID(), path, write, s.now().UnixMilli())
	})
}

// RevokeFolder removes the always folder grant id: ErrNoGrant when none has
// it.
func (s *Store) RevokeFolder(ctx context.Context, id string) error {
	return s.write(ctx, appdb.KeyPermissions, func(st stmts) error { return execOne(ctx, st, stmtDeleteFolderGrant, ErrNoGrant, id) })
}

// ListenForPermissionChanges notifies after each write of the modes, the always rules or
// the always folder grants commits. Listen before the first read.
func (s *Store) ListenForPermissionChanges() ChangeNotifications {
	return s.db.ListenForChanges(appdb.KeyPermissions)
}

var validEffects = []permissions.Effect{permissions.Allow, permissions.Deny, permissions.Ask}

// CheckRule is why r cannot be written, as a RuleRefusal, or nil. Every
// writer of permission_rules checks a rule here first, and every read of one
// runs it too.
func CheckRule(r permissions.Rule) error {
	if reason := ruleRefusal(r); reason != "" {
		return RuleRefusal(reason)
	}
	return nil
}

// ruleRefusal is why r cannot be written, or "".
func ruleRefusal(r permissions.Rule) string {
	_, levelKnown := permissions.LevelNamed(string(r.AccessLevel))
	_, targetKnown := permissions.TargetNamed(r.Target)
	_, accountNamed := r.Account()
	switch {
	case r.ID == permissions.Refused.ID || r.ID == permissions.RefusedData.ID:
		return "has the id Kubetail keeps for its own rule"
	case !slices.Contains(validEffects, r.Effect):
		return "has an effect other than allow, deny or ask"
	case !levelKnown:
		return "names an access level Kubetail does not know"
	case r.AccessLevel == permissions.Read || r.AccessLevel == permissions.Reach:
		return "names reads or reach, which no rule decides"
	case r.Principal != "*" && r.Principal != string(permissions.Chat) && r.Principal != string(permissions.Subagent):
		return "names a principal other than anyone, chat or subagent: the boundary alone decides the monitor and the console"
	case r.Target != "*" && !targetKnown:
		return "names a target Kubetail does not know"
	case !r.Storable():
		return "has an empty scope, operation or resource"
	case innerDoubleStar(r.Scope) || innerDoubleStar(r.Resource):
		return "has a ** that is not a path's last segment"
	case r.Effect != permissions.Allow:
		return ""
	case r.AccessLevel == permissions.Destructive:
		return "allows a destructive change, which always asks"
	case r.Principal == string(permissions.Subagent):
		return "allows for a subagent, which its parent's verdict would never let win"
	case r.Target == "*" || !accountNamed:
		return "allows on every target or account, which only a deny or ask rule may"
	}
	return ""
}

// innerDoubleStar is whether path has a ** segment before its last.
func innerDoubleStar(path string) bool {
	segs := strings.Split(path, "/")
	return slices.Contains(segs[:len(segs)-1], "**")
}

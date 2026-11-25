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

package chat

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// chatRules is the rules that last for chatID's life, read on every decision
// and never cached, so one written while a command runs applies to its next
// request. A read that fails may have hidden a deny, so it reads as
// permissions.Unread: the chat's writes and data reads are refused until a
// read succeeds.
func (s *service) chatRules(ctx context.Context, chatID ChatID) []permissions.Rule {
	rules, err := readRules(ctx, s.store.Stmts(), chatID)
	if err != nil {
		slog.Warn("chat rules not read", "chat", chatID, "err", err)
		return permissions.Unread()
	}
	return rules
}

// alwaysRules is the always rules for a decision: permissions.Unread when
// their read fails.
func (s *service) alwaysRules(ctx context.Context) []permissions.Rule {
	rules, err := s.settings.Rules(ctx)
	if err != nil {
		slog.Warn("always rules not read", "err", err)
		return permissions.Unread()
	}
	return rules
}

// modeFor is the mode of target's account for a decision: read-only when its
// read fails.
func (s *service) modeFor(ctx context.Context, target permissions.Target, account string) permissions.Mode {
	st, err := s.settings.ModeFor(ctx, target, account)
	if err != nil {
		slog.Warn("approval mode not read", "target", target, "err", err)
		return permissions.ModeReadOnly
	}
	return st.Mode
}

// readRules is chatID's rules in the order written.
func readRules(ctx context.Context, st stmts, chatID ChatID) ([]permissions.Rule, error) {
	rows, err := st.Query(ctx, stmtSelectChatRules, string(chatID))
	if err != nil {
		return nil, err
	}
	return settings.ScanRules(rows)
}

// readFolderGrants is chatID's folder grants in the order written, unchecked.
func readFolderGrants(ctx context.Context, st stmts, chatID ChatID) ([]settings.FolderGrant, error) {
	rows, err := st.Query(ctx, stmtSelectChatFolderGrants, string(chatID))
	if err != nil {
		return nil, err
	}
	return settings.FolderGrants(rows)
}

// ErrGrantGone is a rule or folder grant id the chat, or for an always
// grant the settings, do not hold.
var ErrGrantGone = errors.New("chat: no grant has this id")

// ChatRules is chatID's rules, each as Settings spells a rule; none for a chat
// that is gone.
func (s *service) ChatRules(ctx context.Context, chatID ChatID) ([]permissions.Rule, error) {
	return readRules(ctx, s.store.Stmts(), chatID)
}

// addRule writes rule as one of chatID's under a fresh id, and answers it as
// written, unless the chat holds the same rule already, which it answers
// instead, unwritten and with no ping: a settings.RuleRefusal for a rule
// Kubetail cannot apply. The lookup and the write are one transaction.
func (s *service) addRule(ctx context.Context, chatID ChatID, rule permissions.Rule) (permissions.Rule, error) {
	if err := settings.CheckRule(rule); err != nil {
		return permissions.Rule{}, err
	}
	written := false
	err := s.store.InTx(ctx, func(st stmts) error {
		if err := requireChat(ctx, st, chatID); err != nil {
			return err
		}
		held, err := readRules(ctx, st, chatID)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(held, rule.Same); i >= 0 {
			rule = held[i]
			return nil
		}
		rule.ID = appdb.NewID()
		now := millis(normalizeTime(s.now()))
		args := append(settings.RuleValues(rule), string(chatID), now, now)
		_, err = st.Exec(ctx, stmtInsertChatRule, args...)
		written = err == nil
		return err
	})
	if err != nil {
		return permissions.Rule{}, err
	}
	if written {
		s.notify(grantsKey(chatID))
	}
	return rule, nil
}

// RemoveChatRule removes chatID's rule id, and answers the rules left:
// ErrChatGone for a chat that is gone, ErrGrantGone for an id it does not
// hold.
func (s *service) RemoveChatRule(ctx context.Context, chatID ChatID, id string) ([]permissions.Rule, error) {
	var left []permissions.Rule
	err := s.store.InTx(ctx, func(st stmts) error {
		if err := deleteChatRow(ctx, st, stmtDeleteChatRule, chatID, id); err != nil {
			return err
		}
		var err error
		left, err = readRules(ctx, st, chatID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.notify(grantsKey(chatID))
	return left, nil
}

// requireChat is ErrChatGone for a chat that is gone.
func requireChat(ctx context.Context, st stmts, chatID ChatID) error {
	if _, ok, err := getChat(ctx, st, chatID); err != nil || !ok {
		return cmp.Or(err, ErrChatGone)
	}
	return nil
}

// deleteChatRow deletes chatID's row id with stmt: ErrChatGone for a chat that
// is gone, ErrGrantGone when it removed nothing.
func deleteChatRow(ctx context.Context, st stmts, stmt stmtID, chatID ChatID, id string) error {
	if err := requireChat(ctx, st, chatID); err != nil {
		return err
	}
	res, err := st.Exec(ctx, stmt, string(chatID), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil { // coverage:ignore modernc.org/sqlite's RowsAffected never fails
		return err
	}
	if n == 0 {
		return ErrGrantGone
	}
	return nil
}

// ListenForGrantChanges notifies of each change to chatID's rules and folder grants.
func (s *service) ListenForGrantChanges(chatID ChatID) GrantChangeNotifications {
	return s.db.ListenForChanges(grantsKey(chatID))
}

// FoldersFor is foldersFor, for a run with no turn of its own.
func (s *service) FoldersFor(ctx context.Context, chatID ChatID) ([]session.Folder, error) {
	return s.foldersFor(ctx, chatID)
}

// foldersFor is the one builder of a session's folders: chatID's folder
// grants, then the always ones, less every folder that fails its check now.
// Each is read and checked live, so a grant written or revoked meanwhile
// applies to the next read. A read that fails is an error, never an empty
// list. None where no sandbox confines a run: there a grant would open a
// folder to the file tools with nothing keeping the denied-always list out.
func (s *service) foldersFor(ctx context.Context, chatID ChatID) ([]session.Folder, error) {
	grants, err := s.folderGrantsIn(ctx, s.store.Stmts(), chatID)
	if err != nil {
		return nil, err
	}
	return s.checkedFolders(ctx, chatID, grants)
}

// checkedFolders is grants less every folder that fails its check now. A
// check whose read fails is an error.
func (s *service) checkedFolders(ctx context.Context, chatID ChatID, grants []settings.FolderGrant) ([]session.Folder, error) {
	checked, err := s.checkedFolderGrants(ctx, grants)
	if err != nil {
		return nil, err
	}
	var folders []session.Folder
	for _, g := range checked {
		if g.Refusal != "" {
			slog.Info("folder grant left out", "chat", chatID, "folder", g.Path, "reason", g.Refusal)
			continue
		}
		folders = append(folders, session.Folder{Path: g.Path, Write: g.Write})
	}
	return folders, nil
}

// folderGrantsIn is the folder grants a run of chatID reads, unchecked: the
// chat's, read through st, then the always ones. None without a sandbox.
func (s *service) folderGrantsIn(ctx context.Context, st stmts, chatID ChatID) ([]settings.FolderGrant, error) {
	if !s.sandboxState.Available {
		return nil, nil
	}
	var grants []settings.FolderGrant
	if chatID != "" {
		var err error
		if grants, err = readFolderGrants(ctx, st, chatID); err != nil {
			return nil, err
		}
	}
	always, err := s.settings.FolderGrants(ctx)
	if err != nil {
		return nil, err
	}
	return append(grants, always...), nil
}

// FolderGrant is one folder grant as Settings and the composer draw it:
// Refusal is why it fails its check now, so no run takes it; "" when it
// passes.
type FolderGrant struct {
	ID      string
	Path    string
	Write   bool
	Refusal string
}

// FolderGrants is the always grants and, with a chat, the chat's own, each
// checked. None on a machine with no sandbox. A read that fails is an error.
func (s *service) FolderGrants(ctx context.Context, chatID ChatID) (always, chat []FolderGrant, err error) {
	if !s.sandboxState.Available {
		return nil, nil, nil
	}
	stored, err := s.settings.FolderGrants(ctx)
	if err != nil {
		return nil, nil, err
	}
	if always, err = s.checkedFolderGrants(ctx, stored); err != nil {
		return nil, nil, err
	}
	if chatID != "" {
		if stored, err = readFolderGrants(ctx, s.store.Stmts(), chatID); err != nil {
			return nil, nil, err
		}
		if chat, err = s.checkedFolderGrants(ctx, stored); err != nil {
			return nil, nil, err
		}
	}
	return always, chat, nil
}

// checkedFolderGrants is grants, each checked: a refusal stays on its grant,
// and a check whose read fails is an error, so a failed read never looks like
// a list of refused grants.
func (s *service) checkedFolderGrants(ctx context.Context, grants []settings.FolderGrant) ([]FolderGrant, error) {
	out := make([]FolderGrant, 0, len(grants))
	for _, g := range grants {
		fg := FolderGrant{ID: g.ID, Path: g.Path, Write: g.Write}
		err := s.settings.CheckStoredFolder(ctx, g.Path, g.Write)
		if _, refused := errors.AsType[settings.FolderRefusal](err); refused {
			fg.Refusal = err.Error()
		} else if err != nil {
			return nil, err
		}
		out = append(out, fg)
	}
	return out, nil
}

// GrantFolder writes a folder grant, once it passes its check: in chatID's
// grants, or with no chat as an always grant. A grant of a folder already
// granted in the same place keeps that grant's id and changes its access, an
// upsert on the folder, so two grants of one folder at once leave one.
func (s *service) GrantFolder(ctx context.Context, chatID ChatID, path string, write bool) error {
	if err := s.settings.CheckFolder(ctx, path, write); err != nil {
		return err
	}
	if chatID == "" {
		return s.settings.GrantFolder(ctx, path, write)
	}
	err := s.store.InTx(ctx, func(st stmts) error {
		if err := requireChat(ctx, st, chatID); err != nil {
			return err
		}
		_, err := st.Exec(ctx, stmtUpsertChatFolderGrant, appdb.NewID(), string(chatID), path, settings.FolderAccess(write), millis(normalizeTime(s.now())))
		return err
	})
	if err != nil {
		return err
	}
	s.notify(grantsKey(chatID))
	return nil
}

// RevokeFolder removes folder grant id: chatID's, or with no chat an always
// one. ErrGrantGone for an id that is not one, ErrChatGone for a chat that is
// gone.
func (s *service) RevokeFolder(ctx context.Context, chatID ChatID, id string) error {
	if chatID == "" {
		err := s.settings.RevokeFolder(ctx, id)
		if errors.Is(err, settings.ErrNoGrant) {
			return ErrGrantGone
		}
		return err
	}
	if err := s.store.InTx(ctx, func(st stmts) error {
		return deleteChatRow(ctx, st, stmtDeleteChatFolderGrant, chatID, id)
	}); err != nil {
		return err
	}
	s.notify(grantsKey(chatID))
	return nil
}

// sessionFor is a chat's session: its policy read live on every decision, so
// a mode or rule changed in Settings applies to the next one, a running turn's
// and a running subagent's included. The mode is the action's account's; the
// chat's rules come before the always rules. Its network is the chat's switch,
// read at each command the same way, else the turn's toggle. Its folders are
// read live too, and none applies outside the sandbox.
func (s *service) sessionFor(chatID ChatID, outside, networkThisTurn bool) session.Session {
	folders := func(ctx context.Context) ([]session.Folder, error) { return s.foldersFor(ctx, chatID) }
	if outside {
		folders = func(context.Context) ([]session.Folder, error) { return nil, nil }
	}
	return session.Session{
		Kind:    session.Chat,
		Outside: outside,
		Folders: folders,
		Policy: func(ctx context.Context, target permissions.Target, account string) permissions.Policy {
			return permissions.Policy{
				Mode:  s.modeFor(ctx, target, account),
				Rules: append(s.chatRules(ctx, chatID), s.alwaysRules(ctx)...),
			}
		},
		Network: func(ctx context.Context) session.Network {
			switch {
			case s.networkEnabled(ctx, chatID):
				return session.NetworkChat
			case networkThisTurn:
				return session.NetworkTurn
			}
			return session.NoNetwork
		},
	}
}

// monitorSession is the monitor's session: it reads the cluster through the
// proxy and nothing else, which its kind's row of the boundary holds it to.
// Every field it leaves zero is read by something: no Policy is read-only at
// the proxy, no Network is none, no Folders is none.
func monitorSession() session.Session {
	return session.Session{Kind: session.Monitor}
}

// networkEnabled is the chat's network switch as stored now. A read that
// fails, or a chat that is gone, answers false, so a broken read gives no
// network.
func (s *service) networkEnabled(ctx context.Context, chatID ChatID) bool {
	c, ok, err := getChat(ctx, s.store.Stmts(), chatID)
	if err != nil {
		slog.Warn("chat network switch not read", "chat", chatID, "err", err)
		return false
	}
	return ok && c.NetworkEnabled
}

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
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/amorey/gobus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// exec runs q on db's writer.
func exec(t *testing.T, db *appdb.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Write.Exec(q, args...)
	require.NoError(t, err)
}

// kubeRule is a rule of effect over writes on cluster 1.
func kubeRule(id string, effect permissions.Effect) permissions.Rule {
	return permissions.Rule{ID: id, Effect: effect, AccessLevel: permissions.Write, Principal: "*", Target: "kubernetes",
		Scope: "1/**", Service: "*", Operation: "*", Resource: "*"}
}

// seedRule writes a permission_rules row of chatID's.
func seedRule(t *testing.T, db *appdb.DB, chatID ChatID, r permissions.Rule) {
	t.Helper()
	now := time.Now().UnixMilli()
	exec(t, db, `INSERT INTO permission_rules (`+settings.RuleColumns+`, chat_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(settings.RuleValues(r), string(chatID), now, now)...)
}

// seedFolderGrant writes a folder_grants row of chatID's, as given.
func seedFolderGrant(t *testing.T, db *appdb.DB, chatID ChatID, id, folder, access string) {
	t.Helper()
	exec(t, db, `INSERT INTO folder_grants (id, chat_id, folder, access, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, string(chatID), folder, access, time.Now().UnixMilli())
}

// rowsIn is how many rows table holds.
func rowsIn(t *testing.T, db *appdb.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Read.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

// foldersFor is s.FoldersFor, which must read.
func foldersFor(t *testing.T, s *service, chatID ChatID) []session.Folder {
	t.Helper()
	folders, err := s.FoldersFor(t.Context(), chatID)
	require.NoError(t, err)
	return folders
}

// grantedFolders is sess's folders, which must read.
func grantedFolders(t *testing.T, sess session.Session) []session.Folder {
	t.Helper()
	folders, err := sess.GrantedFolders(t.Context())
	require.NoError(t, err)
	return folders
}

// folderGrants is s.FolderGrants, which must read.
func folderGrants(t *testing.T, s *service, chatID ChatID) (always, chat []FolderGrant) {
	t.Helper()
	always, chat, err := s.FolderGrants(t.Context(), chatID)
	require.NoError(t, err)
	return always, chat
}

func TestAChatsGrantsGoWithTheChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	other := seedChat(t, s.db, aChat("1", time.Now()))
	assert.Empty(t, s.chatRules(t.Context(), c.ID))

	allow := kubeRule("a", permissions.Allow)
	seedRule(t, s.db, c.ID, allow)
	seedFolderGrant(t, s.db, c.ID, "f", "/srv/code", "read")
	assert.Equal(t, []permissions.Rule{allow}, s.chatRules(t.Context(), c.ID), "a row is read live, and a folder grant is no rule")
	assert.Empty(t, s.chatRules(t.Context(), other.ID), "another chat's rows are its own")

	exec(t, s.db, `DELETE FROM chats WHERE id = ?`, string(c.ID))
	assert.Zero(t, rowsIn(t, s.db, "permission_rules"), "the rows go with the chat")
	assert.Zero(t, rowsIn(t, s.db, "folder_grants"))
}

// Every field of a rule survives its row.
func TestAChatsRuleRoundTripsThroughItsRow(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	rule := permissions.Rule{Effect: permissions.Deny, AccessLevel: permissions.Destructive, Principal: "subagent", Target: "aws",
		Scope: "123456789012/*", Service: "ec2", Operation: "TerminateInstances", Resource: "instance"}
	added, err := s.addRule(t.Context(), c.ID, rule)
	require.NoError(t, err)
	rule.ID = added.ID
	assert.Equal(t, []permissions.Rule{rule}, s.chatRules(t.Context(), c.ID))
}

// withSettingsOnItsDB gives s settings over its own app.db, holding the
// clusters dev and prod.
func withSettingsOnItsDB(t *testing.T, s *service) {
	t.Helper()
	for id, context := range map[string]string{"dev": "dev-eks", "prod": "prod-eu"} {
		exec(t, s.db, `INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES (?, 'kubeconfig', ?, 0, 0)`, id, context)
	}
	s.settings = settings.NewService(settingsStore(t, s.db), nil, nil, nil, "")
}

// A session's policy is the mode of the action's account, read live, and the
// chat's rules then the always rules.
func TestASessionReadsItsAccountsModeAndRules(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	withSettingsOnItsDB(t, s)
	ctx := t.Context()
	c := seedChat(t, s.db, aChat("1", time.Now()))
	sess := s.sessionFor(c.ID, false, false)
	k8s := permissions.Kubernetes

	assert.Equal(t, permissions.ModeAsk, sess.Policy(ctx, k8s, "dev").Mode)
	require.NoError(t, s.settings.SetMode(ctx, k8s, "*", permissions.ModeAuto))
	assert.Equal(t, permissions.ModeAuto, sess.Policy(ctx, k8s, "dev").Mode, "a mode changed in Settings reaches the next write")
	require.NoError(t, s.settings.SetMode(ctx, k8s, "prod", permissions.ModeReadOnly))
	assert.Equal(t, permissions.ModeReadOnly, sess.Policy(ctx, k8s, "prod").Mode)
	assert.Equal(t, permissions.ModeAuto, sess.Policy(ctx, k8s, "dev").Mode)
	assert.Equal(t, permissions.ModeAsk, sess.Policy(ctx, permissions.AWS, "123456789012").Mode, "another target's account has its own")
	require.NoError(t, s.settings.SetMode(ctx, permissions.AWS, "123456789012", permissions.ModeAuto))
	assert.Equal(t, permissions.ModeAuto, sess.Policy(ctx, permissions.AWS, "123456789012").Mode)
	assert.Empty(t, sess.Policy(ctx, k8s, "dev").Rules)

	always, err := s.settings.AddRule(ctx, permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.Write, Principal: "*",
		Target: "kubernetes", Scope: "dev/**", Service: "*", Operation: "*", Resource: "*"})
	require.NoError(t, err)
	seedRule(t, s.db, c.ID, kubeRule("g", permissions.Deny))
	ids := []string{}
	for _, r := range sess.Policy(ctx, k8s, "dev").Rules {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []string{"g", always.ID}, ids, "the chat's rules, then the always rules")
}

// A policy whose mode or rules cannot be read is read-only and refuses every
// write and data read.
func TestAPolicyThatCannotBeReadRefuses(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	withSettingsOnItsDB(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	_, err := s.settings.AddRule(t.Context(), permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.DataRead, Principal: "*",
		Target: "kubernetes", Scope: "dev/**", Service: "*", Operation: "*", Resource: "*"})
	require.NoError(t, err)
	for _, table := range []string{"permission_modes", "permission_rules"} {
		exec(t, s.db, `DROP TABLE `+table)
	}

	p := s.sessionFor(c.ID, false, false).Policy(t.Context(), permissions.Kubernetes, "dev")
	assert.Equal(t, permissions.ModeReadOnly, p.Mode)
	for _, level := range []permissions.AccessLevel{permissions.Write, permissions.DataRead} {
		got, _ := p.Authorize(permissions.Action{Principal: permissions.Chat, Target: permissions.Kubernetes, AccessLevel: level,
			Scope: "dev/web", AccountName: "dev-eks", Operation: "get", Resource: "secrets"})
		assert.Equal(t, permissions.Deny, got.Effect, "%s", level)
	}
}

// A read of the chat's rules that fails may have hidden a deny, so it refuses
// the chat's writes and data reads, and a rule that cannot read them is not
// written.
func TestAGrantsReadThatFailsRefuses(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	seedRule(t, s.db, c.ID, kubeRule("a", permissions.Allow))
	exec(t, s.db, `DROP TABLE permission_rules`)

	assert.Equal(t, permissions.Unread(), s.chatRules(t.Context(), c.ID))
	_, err := s.addRule(t.Context(), c.ID, kubeRule("", permissions.Allow))
	assert.Error(t, err)
}

// A rule's write or removal that fails reaches the caller.
func TestAGrantsWriteThatFailsRefuses(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	rule := kubeRule("", permissions.Allow)
	added, err := s.addRule(t.Context(), c.ID, rule)
	require.NoError(t, err)
	for _, kind := range []string{"INSERT", "DELETE"} {
		exec(t, s.db, `CREATE TRIGGER refuse_`+kind+` BEFORE `+kind+` ON permission_rules
			BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	}

	rule.Scope = "2/**"
	_, err = s.addRule(t.Context(), c.ID, rule)
	assert.ErrorContains(t, err, "refused")
	_, err = s.RemoveChatRule(t.Context(), c.ID, added.ID)
	assert.ErrorContains(t, err, "refused")
	assert.Equal(t, []permissions.Rule{added}, s.chatRules(t.Context(), c.ID))
}

// A chat's rules are written, listed and removed by id, and the command's next
// decision reads each change. Its folder grants are no rules.
func TestAChatsRulesAreListedAndRemoved(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	ctx := t.Context()
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	rule := kubeRule("", permissions.Allow)
	rule.Scope = "1/web"
	require.NoError(t, s.GrantFolder(ctx, c.ID, filepath.Join(home, "code"), false))

	added, err := s.addRule(ctx, c.ID, rule)
	require.NoError(t, err)
	require.NotEmpty(t, added.ID)
	listed, err := s.ChatRules(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, []permissions.Rule{added}, listed)

	again, err := s.addRule(ctx, c.ID, rule)
	require.NoError(t, err)
	assert.Equal(t, added, again, "a rule the chat holds is not written twice")
	assert.Len(t, s.chatRules(ctx, c.ID), 1)

	_, chat := folderGrants(t, s, c.ID)
	require.Len(t, chat, 1)
	_, err = s.RemoveChatRule(ctx, c.ID, chat[0].ID)
	assert.ErrorIs(t, err, ErrGrantGone, "a folder grant is not a rule")

	left, err := s.RemoveChatRule(ctx, c.ID, added.ID)
	require.NoError(t, err)
	assert.Empty(t, left)
	assert.Empty(t, s.chatRules(ctx, c.ID), "the command's next decision no longer finds it")
	_, err = s.RemoveChatRule(ctx, c.ID, added.ID)
	assert.ErrorIs(t, err, ErrGrantGone)

	exec(t, s.db, `DELETE FROM chats WHERE id = ?`, string(c.ID))
	_, err = s.RemoveChatRule(ctx, c.ID, appdb.NewID())
	assert.ErrorIs(t, err, ErrChatGone)
	_, err = s.addRule(ctx, c.ID, rule)
	assert.ErrorIs(t, err, ErrChatGone)
}

// Each write or revoke of a chat's rules or folder grants pings its grants
// key once it commits, and a refused one pings nothing, so a watch of the
// chat's folders re-reads.
func TestAChatsGrantWritesPingItsKey(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	ctx := t.Context()
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	other := seedChat(t, s.db, aChat("1", time.Now()))
	rx := s.ListenForGrantChanges(c.ID)
	t.Cleanup(rx.Close)
	pinged := func(what string) {
		t.Helper()
		_, err := rx.TryRecv()
		require.NoError(t, err, what)
	}
	quiet := func(what string) {
		t.Helper()
		_, err := rx.TryRecv()
		require.ErrorIs(t, err, gobus.ErrEmpty, what)
	}

	require.NoError(t, s.GrantFolder(ctx, c.ID, filepath.Join(home, "code"), false))
	pinged("a folder grant")
	_, chat := folderGrants(t, s, c.ID)
	require.NoError(t, s.RevokeFolder(ctx, c.ID, chat[0].ID))
	pinged("a folder grant revoked")
	added, err := s.addRule(ctx, c.ID, kubeRule("", permissions.Allow))
	require.NoError(t, err)
	pinged("a rule an approval wrote")
	_, err = s.addRule(ctx, c.ID, kubeRule("", permissions.Allow))
	require.NoError(t, err)
	quiet("a rule the chat holds already")
	_, err = s.RemoveChatRule(ctx, c.ID, added.ID)
	require.NoError(t, err)
	pinged("a rule removed")

	_, err = s.RemoveChatRule(ctx, c.ID, added.ID)
	require.ErrorIs(t, err, ErrGrantGone)
	quiet("a refused removal")
	require.ErrorIs(t, s.RevokeFolder(ctx, c.ID, chat[0].ID), ErrGrantGone)
	quiet("a refused revoke")
	require.Error(t, s.GrantFolder(ctx, c.ID, filepath.Join(home, "missing"), false))
	quiet("a refused folder grant")
	require.ErrorIs(t, s.GrantFolder(ctx, ChatID(appdb.NewID()), filepath.Join(home, "code"), false), ErrChatGone)
	quiet("a grant for a chat that is gone")
	require.NoError(t, s.GrantFolder(ctx, other.ID, filepath.Join(home, "code"), false))
	quiet("another chat's grant")
}

// The chat's network switch is read at each command, so one turned on or off
// mid-turn reaches the next command; a read that fails, or a chat that is gone,
// gives no network.
func TestTheSwitchIsReadLive(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.sandboxState = sandbox.State{Available: true, NetworkAvailable: true}
	c := seedChat(t, s.db, aChat("1", time.Now()))
	sess := s.sessionFor(c.ID, false, false)
	assert.Empty(t, sess.Network(t.Context()))

	_, err := s.SetNetworkEnabled(t.Context(), c.ID, true)
	require.NoError(t, err)
	assert.Equal(t, session.NetworkChat, sess.Network(t.Context()))
	assert.Equal(t, session.NetworkChat, s.sessionFor(c.ID, false, true).Network(t.Context()), "the switch names itself over the toggle")

	failed, cancel := context.WithCancel(t.Context())
	cancel()
	assert.Empty(t, sess.Network(failed))
	assert.Empty(t, s.sessionFor(ChatID(appdb.NewID()), false, false).Network(t.Context()))

	_, err = s.SetNetworkEnabled(t.Context(), c.ID, false)
	require.NoError(t, err)
	assert.Empty(t, sess.Network(t.Context()))
	assert.Equal(t, session.NetworkTurn, s.sessionFor(c.ID, false, true).Network(t.Context()))
}

func TestFoldersForNoChatIsTheAlwaysOnes(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	code, svc := filepath.Join(home, "code"), filepath.Join(home, "code", "svc")
	require.NoError(t, s.GrantFolder(t.Context(), "", svc, true))
	require.NoError(t, s.GrantFolder(t.Context(), c.ID, code, false))

	assert.Equal(t, []session.Folder{{Path: svc, Write: true}}, foldersFor(t, s, ""), "the always folders alone")
	assert.Equal(t, []session.Folder{{Path: code}, {Path: svc, Write: true}}, foldersFor(t, s, c.ID),
		"the chat's, then the always ones")
	assert.Equal(t, foldersFor(t, s, c.ID), grantedFolders(t, s.sessionFor(c.ID, false, false)))
}

func TestNoFolderAppliesWithoutASandbox(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	require.NoError(t, s.GrantFolder(t.Context(), "", home, false))
	require.NotEmpty(t, grantedFolders(t, s.sessionFor(c.ID, false, false)))

	assert.Empty(t, grantedFolders(t, s.sessionFor(c.ID, true, false)), "a chat switched outside the sandbox gets none")
	s.sandboxState = sandbox.State{Reason: "no bwrap"}
	assert.Empty(t, foldersFor(t, s, c.ID))
	assert.Empty(t, grantedFolders(t, s.sessionFor(c.ID, false, false)))
	listed, chat := folderGrants(t, s, c.ID)
	assert.Empty(t, listed, "nor is one listed")
	assert.Empty(t, chat)
}

func TestFoldersForAnswersNoneWithNoSnapshot(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	s.settings = settings.NewService(settingsStore(t, s.db), nil, nil, nil, "")
	s.sandboxState = sandbox.State{Available: true}
	require.NoError(t, s.settings.GrantFolder(t.Context(), testutil.GrantableDir(t), false))
	assert.Empty(t, foldersFor(t, s, ""), "a service with no zones checks no folder")
}

// A read of the folder grants that fails is an error wherever they are read:
// a run, the listing, a send, never an empty list.
func TestAFoldersReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	require.NoError(t, s.GrantFolder(t.Context(), "", home, false))
	exec(t, s.db, `DROP TABLE folder_grants`)

	for _, chatID := range []ChatID{"", c.ID} {
		_, err := s.FoldersFor(t.Context(), chatID)
		assert.Error(t, err)
		_, _, err = s.FolderGrants(t.Context(), chatID)
		assert.Error(t, err)
	}
	_, err := s.sessionFor(c.ID, false, false).GrantedFolders(t.Context())
	assert.Error(t, err)
	_, err = s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.ErrorContains(t, err, "folder_grants", "a turn does not start")
}

// A read of the PATH that fails inside a grant's check is an error wherever
// the grants are read, never a refusal on the grant: a failed query must not
// look like a list of refused grants.
func TestAFailedFolderCheckReadEndsTheWatch(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	svc := filepath.Join(grantable(t, s), "code", "svc")
	c := seedChat(t, s.db, aChat("1", time.Now()))
	// Only a write grant is compared with the PATH.
	exec(t, s.db, `DROP TABLE shell_path`)
	seedFolderGrant(t, s.db, c.ID, "stored", svc, "write")
	_, _, err := s.FolderGrants(t.Context(), c.ID)
	assert.Error(t, err, "a chat's grant alone")

	exec(t, s.db, `CREATE TABLE shell_path (id, path)`)
	require.NoError(t, s.GrantFolder(t.Context(), "", svc, true))
	exec(t, s.db, `DROP TABLE shell_path`)

	for _, chatID := range []ChatID{"", c.ID} {
		_, err := s.FoldersFor(t.Context(), chatID)
		assert.Error(t, err)
		_, _, err = s.FolderGrants(t.Context(), chatID)
		assert.Error(t, err)
	}
	_, err = s.sessionFor(c.ID, false, false).GrantedFolders(t.Context())
	assert.Error(t, err)
}

// An always grant stored unchecked fails its check where it is listed, and a
// chat's: each is listed with its refusal.
func TestAGrantIsListedWithItsRefusal(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	seedFolderGrant(t, s.db, c.ID, "stored", home, "write")
	require.NoError(t, s.settings.GrantFolder(t.Context(), filepath.Join(home, ".ssh"), false))

	always, chat := folderGrants(t, s, c.ID)
	require.Len(t, always, 1)
	require.Len(t, chat, 1)
	assert.NotEmpty(t, always[0].Refusal)
	assert.NotEmpty(t, chat[0].Refusal)
	assert.Empty(t, foldersFor(t, s, c.ID), "no run takes either")
}

func TestAChatsFoldersJoinTheAlwaysOnes(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	ctx := t.Context()
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	code, svc := filepath.Join(home, "code"), filepath.Join(home, "code", "svc")
	sess := s.sessionFor(c.ID, false, false)

	require.NoError(t, s.GrantFolder(ctx, c.ID, code, false))
	assert.Equal(t, []session.Folder{{Path: code}}, grantedFolders(t, sess), "a chat's grant is read live")
	require.NoError(t, s.GrantFolder(ctx, "", svc, true))
	assert.Equal(t, []session.Folder{{Path: code}, {Path: svc, Write: true}}, grantedFolders(t, sess))

	always, chat := folderGrants(t, s, c.ID)
	require.Len(t, always, 1)
	require.Len(t, chat, 1)
	assert.Equal(t, FolderGrant{ID: always[0].ID, Path: svc, Write: true}, always[0])
	assert.Equal(t, FolderGrant{ID: chat[0].ID, Path: code}, chat[0])

	assert.ErrorIs(t, s.RevokeFolder(ctx, "", chat[0].ID), ErrGrantGone, "a chat's grant is revoked through the chat")
	assert.ErrorIs(t, s.RevokeFolder(ctx, c.ID, always[0].ID), ErrGrantGone, "an always grant is no chat's")
	require.NoError(t, s.RevokeFolder(ctx, "", always[0].ID))
	assert.Equal(t, []session.Folder{{Path: code}}, grantedFolders(t, sess))
	assert.ErrorIs(t, s.RevokeFolder(ctx, "", always[0].ID), ErrGrantGone)

	require.NoError(t, s.RevokeFolder(ctx, c.ID, chat[0].ID))
	assert.Empty(t, grantedFolders(t, sess))
	assert.ErrorIs(t, s.RevokeFolder(ctx, ChatID(appdb.NewID()), chat[0].ID), ErrChatGone)
}

func TestAGrantInTheSamePlaceChangesItsMode(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	svc := filepath.Join(home, "code", "svc")

	// The chat's grants first, then the always ones, which "" names.
	for _, grantee := range []ChatID{c.ID, ""} {
		require.NoError(t, s.GrantFolder(t.Context(), grantee, svc, false))
		beforeAlways, beforeChat := folderGrants(t, s, c.ID)
		require.NoError(t, s.GrantFolder(t.Context(), grantee, svc, true))
		afterAlways, afterChat := folderGrants(t, s, c.ID)
		before, after := beforeChat, afterChat
		if grantee == "" {
			before, after = beforeAlways, afterAlways
		}
		require.Len(t, after, 1, "%q: one grant", grantee)
		assert.Equal(t, before[0].ID, after[0].ID, "%q: the first id", grantee)
		assert.True(t, after[0].Write, "%q: read-write", grantee)
	}
	always, chat := folderGrants(t, s, c.ID)
	assert.Len(t, always, 1, "a chat grant beside an always one on the same folder")
	assert.Len(t, chat, 1)
}

func TestAGrantIsCheckedWhenWritten(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))

	var r settings.FolderRefusal
	require.ErrorAs(t, s.GrantFolder(t.Context(), c.ID, home, true), &r)
	assert.Equal(t, "home", r.Rule)
	require.ErrorAs(t, s.GrantFolder(t.Context(), "", filepath.Join(home, ".ssh"), false), &r)
	assert.Equal(t, "never", r.Rule)
	gone := ChatID(appdb.NewID())
	assert.ErrorIs(t, s.GrantFolder(t.Context(), gone, home, false), ErrChatGone)

	always, chat := folderGrants(t, s, c.ID)
	assert.Empty(t, always)
	assert.Empty(t, chat)
}

func TestAGrantGoesWithTheChat(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	svc := filepath.Join(home, "code", "svc")

	require.NoError(t, s.GrantFolder(t.Context(), c.ID, svc, true))
	assert.Equal(t, []session.Folder{{Path: svc, Write: true}}, grantedFolders(t, s.sessionFor(c.ID, false, false)),
		"the chat's next command reads it")
	assert.Equal(t, 1, rowsIn(t, s.db, "folder_grants"), "a chat's grant is a folder_grants row")

	exec(t, s.db, `DELETE FROM chats WHERE id = ?`, string(c.ID))
	assert.Zero(t, rowsIn(t, s.db, "folder_grants"))
}

// Two grants of one folder at once, in one place, leave one grant: each is an
// upsert on the folder.
func TestConcurrentGrantsOfOneFolderLeaveOne(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	home := grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	svc := filepath.Join(home, "code", "svc")
	for _, grantee := range []ChatID{c.ID, ""} {
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() { assert.NoError(t, s.GrantFolder(t.Context(), grantee, svc, i%2 == 1)) })
		}
		wg.Wait()
	}
	always, chat := folderGrants(t, s, c.ID)
	assert.Len(t, always, 1)
	assert.Len(t, chat, 1)
}

// A chat's session is the chat's, and a subagent's under it the subagent's:
// each a principal that asks and reads data under its rules.
func TestAChatsSessionAsksAndReadsSecretData(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	assert.Equal(t, session.Chat, s.sessionFor(c.ID, false, false).Kind)
	assert.Equal(t, session.Subagent, session.Narrow(s.sessionFor(c.ID, false, false)).Kind)
}

// The monitor's session holds less than any chat's: no switch, no policy (the
// proxy reads read-only), no network, no folders, and its kind alone, which
// the boundary holds to reads. Under any policy, every write, every data read
// and every action a rule would ask about is refused.
func TestTheMonitorSessionIsReadOnlyAndAsksNobody(t *testing.T) {
	t.Parallel()

	sess := monitorSession()

	assert.Equal(t, session.Monitor, sess.Kind)
	assert.False(t, sess.Outside)
	assert.Nil(t, sess.Policy)
	assert.Nil(t, sess.Network)
	assert.Nil(t, sess.Folders)
	assert.Empty(t, grantedFolders(t, sess))

	allow := func(level permissions.AccessLevel) permissions.Rule {
		return permissions.Rule{Effect: permissions.Allow, AccessLevel: level, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}
	}
	asks := allow(permissions.Read)
	asks.Effect = permissions.Ask
	for _, mode := range permissions.Modes {
		for _, level := range permissions.Levels {
			policy := permissions.Policy{Mode: mode, Rules: []permissions.Rule{asks, allow(level)}}
			got, why := policy.Authorize(permissions.Action{Principal: sess.Kind, Target: permissions.Kubernetes, Scope: "1/web",
				AccessLevel: level, Operation: "patch", Service: "apps", Resource: "deployments"})
			assert.Equal(t, permissions.Deny, got.Effect, "%s %s: %s", mode, level, why)
		}
	}
}

// A read of a chat's rules or folder grants that fails after some rows were
// read is a failed read: the rules refuse, and the folders are an error.
func TestAChatsReadThatFailsPartwayAnswersNone(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	for _, q := range []string{
		`DROP TABLE permission_rules`,
		`CREATE TABLE permission_rules (id, chat_id, effect, access_level, principal, target, scope, service, operation, resource, created_at)`,
		`DROP TABLE folder_grants`,
		`CREATE TABLE folder_grants (id, chat_id, folder, access, created_at)`,
	} {
		exec(t, s.db, q)
	}
	for _, q := range []string{
		`INSERT INTO permission_rules VALUES ('a', ?1, 'allow', 'write', '*', 'kubernetes', '1', '*', '*', '*', 1), ('b', ?1, 'deny', 'write', '*', '*', NULL, '*', '*', '*', 2)`,
		`INSERT INTO folder_grants VALUES ('a', ?1, '/srv/code', 'read', 1), ('b', ?1, NULL, 'read', 2)`,
	} {
		exec(t, s.db, q, string(c.ID))
	}

	assert.Equal(t, permissions.Unread(), s.chatRules(t.Context(), c.ID))
	_, err := s.FoldersFor(t.Context(), c.ID)
	assert.Error(t, err)
}

// A chat's grants that cannot be read fail the listing, though the always
// ones read.
func TestAChatsGrantsThatCannotBeReadFailTheListing(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	grantable(t, s)
	s.settings = testSettings(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	exec(t, s.db, `DROP TABLE folder_grants`)

	always, _, err := s.FolderGrants(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, always)
	_, _, err = s.FolderGrants(t.Context(), c.ID)
	assert.Error(t, err)
}

// A chat's rule is checked as an always rule is, so one Kubetail cannot apply
// is refused in the user's words and nothing is written.
func TestAChatsRuleKubetailCannotApplyIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	rule := kubeRule("", permissions.Allow)
	rule.AccessLevel = permissions.Destructive
	_, err := s.addRule(t.Context(), c.ID, rule)
	var refusal settings.RuleRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, "allows a destructive change, which always asks", string(refusal))
	assert.Zero(t, rowsIn(t, s.db, "permission_rules"))
}

// A send outside the sandbox takes no folders, so it starts though they cannot
// be read.
func TestASendOutsideTheSandboxReadsNoFolders(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	grantable(t, s)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	_, err := s.SetSandboxDisabled(t.Context(), c.ID, true)
	require.NoError(t, err)
	_, err = s.db.Write.Exec(`DROP TABLE folder_grants`)
	require.NoError(t, err)

	_, err = s.Send(t.Context(), &c.ID, ModeChat, "1", SendSwitches{SandboxDisabled: true}, "fake", "fake", "high", reqID("1"), "hi", nil)
	assert.NoError(t, err)
}

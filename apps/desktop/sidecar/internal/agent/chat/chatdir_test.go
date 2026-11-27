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
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A chat's directory is made only when a caller asks for it, and answers at the
// path its results name.
func TestAChatsDirectoryIsMadeOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := startService(t, dir)
	d := s.chatDir("c1")
	assert.Equal(t, filepath.Join(dir, "chats", "c1"), d.Path())

	_, err := d.Root(false)
	require.ErrorIs(t, err, fs.ErrNotExist)

	root, err := d.Root(true)
	require.NoError(t, err)
	require.NoError(t, root.WriteFile("x.txt", []byte("x"), 0o600))
	require.NoError(t, root.Close())

	root, err = d.Root(false)
	require.NoError(t, err, "an existing directory opens without create")
	got, err := root.ReadFile("x.txt")
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))
	require.NoError(t, root.Close())
}

// A chat's entry that is not a directory is refused, not replaced.
func TestOpenResultsRefusesAFile(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	d := s.chatDir("c1")
	require.NoError(t, os.MkdirAll(filepath.Dir(d.Path()), 0o700))
	require.NoError(t, os.WriteFile(d.Path(), nil, 0o600))

	for _, create := range []bool{false, true} {
		_, err := d.Root(create)
		assert.Error(t, err, "create %v", create)
	}
}

// whereTool answers with the path of the chat's directory its call was handed.
type whereTool struct{ testTool }

func (whereTool) Run(_ context.Context, rt tools.Runtime, _ json.RawMessage) (string, bool) {
	return rt.Dir.Path(), false
}

// A turn hands its tools the directory of the chat it runs in, under the
// chats' directory whatever the chat's cluster.
func TestAChatsDirectoryIsUnderTheChatsDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	box, lists := testBox(whereTool{testTool{name: "where"}})
	s := startServiceWith(t, dir, fakeLLM(), &stubClusterCards{}, box, lists)
	fakeOf(s).SetToolCalls(llm.StagedCall("where", `{}`))

	msg, err := s.Send(t.Context(), nil, ModeChat, "7", SendSwitches{}, "fake", "fake", "high", reqID("1"), "hi", nil)
	require.NoError(t, err)
	awaitSettled(t, s, msg.ChatID, msg.ID)

	rows := toolCallRows(t, s.db, msg.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, filepath.Join(dir, "chats", string(msg.ChatID)), rows[0].result)
}

// makeChatDir gives the chat a directory holding one file.
func makeChatDir(t *testing.T, s *service, id ChatID) string {
	t.Helper()
	root, err := s.chatDir(id).Root(true)
	require.NoError(t, err)
	require.NoError(t, root.WriteFile("out.txt", []byte("x"), 0o600))
	require.NoError(t, root.Close())
	return s.chatDir(id).Path()
}

// A chat's directory goes with it, and so do the directories of a cluster's chats.
func TestDeletingAChatRemovesItsResults(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	msg := send(t, s, nil, "1", "hi")
	awaitSettled(t, s, msg.ChatID, msg.ID)
	path := makeChatDir(t, s, msg.ChatID)

	require.NoError(t, s.Delete(t.Context(), msg.ChatID))
	assert.NoDirExists(t, path)

	a := seedChat(t, s.db, aChat("2", time.Now()))
	b := seedChat(t, s.db, aChat("2", time.Now()))
	other := seedChat(t, s.db, aChat("7", time.Now()))
	pathA, pathB, pathOther := makeChatDir(t, s, a.ID), makeChatDir(t, s, b.ID), makeChatDir(t, s, other.ID)
	_, err := s.deleteByCluster(t.Context(), "2")
	require.NoError(t, err)
	assert.NoDirExists(t, pathA)
	assert.NoDirExists(t, pathB)
	assert.DirExists(t, pathOther)
}

// A delete removes the chat's directory, and another chat's is left alone.
func TestDeleteRemovesTheChatsDirectoryAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := startService(t, dir)
	c := seedChat(t, s.db, aChat("7", time.Now()))
	other := seedChat(t, s.db, aChat("1", time.Now()))
	path, otherPath := makeChatDir(t, s, c.ID), makeChatDir(t, s, other.ID)
	require.Equal(t, filepath.Join(dir, "chats", string(c.ID)), path)

	require.NoError(t, s.Delete(t.Context(), c.ID))

	assert.NoDirExists(t, path)
	assert.DirExists(t, otherPath)
}

// A chat that never made its directory is deleted without a word: a directory
// already gone is a removal done, not a failure.
func TestDeletingAChatWithNoDirectoryLogsNothing(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("7", time.Now()))
	logs := testutil.CaptureLogs(t)

	require.NoError(t, s.Delete(t.Context(), c.ID))

	assert.Empty(t, logs.String())
}

// A delete whose write failed leaves the chat's directory with its rows.
func TestAFailedDeleteKeepsTheResults(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	path := makeChatDir(t, s, c.ID)
	s.deleteWrite = func(context.Context, ChatID) (bool, error) { return false, errors.New("disk full") }

	require.Error(t, s.Delete(t.Context(), c.ID))
	assert.DirExists(t, path)
}

// A delete takes a chat id, never a path: one reaching into a chat's directory
// is refused and removes nothing.
func TestADeleteOfAPathIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestService(t)
	c := seedChat(t, s.db, aChat("1", time.Now()))
	path := makeChatDir(t, s, c.ID)

	for _, id := range []ChatID{ChatID(string(c.ID) + "/out.txt"), ".", "..", ""} {
		assert.ErrorIs(t, s.Delete(t.Context(), id), ErrBadRequest, "%q", id)
	}
	assert.FileExists(t, filepath.Join(path, "out.txt"))
}

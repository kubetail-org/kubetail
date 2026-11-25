//go:build !windows

package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/graph"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// newFolderServer is a chat server on a machine with a sandbox, whose home is
// a fresh folder holding a never-readable .ssh and a code folder.
func newFolderServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	home := testutil.GrantableDir(t)
	for _, d := range []string{".ssh", "code", "Documents"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	security := settings.NewService(store, func() settings.Zones {
		return settings.Zones{
			Never: []string{filepath.Join(home, ".ssh")},
			Open:  sandbox.FilePolicy{Deny: []string{filepath.Join(home, "Documents")}},
			Home:  home,
		}
	}, nil, nil, "")
	srv, _, _ := newChatServerOn(t, sandbox.State{Available: true}, security)
	return srv, home
}

// gqlResult is one response: its data, and its errors' messages and extensions.
type gqlResult struct {
	Data   map[string]any
	Errors []struct {
		Message    string
		Extensions map[string]any
	}
}

func post(t *testing.T, srv *httptest.Server, query string) gqlResult {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	require.NoError(t, err)
	raw := postGQL(t, srv.URL, string(body))
	var res gqlResult
	require.NoError(t, json.Unmarshal(raw, &res), "%s", raw)
	return res
}

// newChatOn sends a first question, so the server holds a chat, and answers
// its id.
func newChatOn(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	sent := mutate(t, srv, `mutation { chatSend(mode: Chat, clusterID: "1", sandboxDisabled: false, networkEnabled: false, networkThisTurn: false, providerID: "fake", modelID: "fake", effort: "high",
		requestID: "`+appdb.NewID()+`", content: "hi") { chatID } }`)
	return sent["chatSend"].(map[string]any)["chatID"].(string)
}

const foldersFields = `{ always { id path write refusal } chat { id path write refusal } never closed wide }`

func TestFolderGrantAndRevoke(t *testing.T) {
	srv, home := newFolderServer(t)
	code := filepath.Join(home, "code")

	got := mutate(t, srv, `mutation { folderGrant(path: "`+code+`", write: false, duration: Always) `+foldersFields+` }`)
	folders := got["folderGrant"].(map[string]any)
	always := folders["always"].([]any)
	require.Len(t, always, 1)
	first := always[0].(map[string]any)
	assert.Equal(t, code, first["path"])
	assert.Equal(t, false, first["write"])
	assert.Nil(t, first["refusal"])
	assert.Equal(t, []any{filepath.Join(home, ".ssh")}, folders["never"])
	assert.Equal(t, []any{filepath.Join(home, "Documents")}, folders["closed"])
	assert.Contains(t, folders["wide"], home)

	got = mutate(t, srv, `mutation { folderGrant(path: "`+code+`", write: true, duration: Always) `+foldersFields+` }`)
	always = got["folderGrant"].(map[string]any)["always"].([]any)
	require.Len(t, always, 1, "a grant in the same place changes its mode")
	assert.Equal(t, first["id"], always[0].(map[string]any)["id"])
	assert.Equal(t, true, always[0].(map[string]any)["write"])

	got = mutate(t, srv, `{ permissionSettingsGet { rules { id } } }`)
	assert.Empty(t, got["permissionSettingsGet"].(map[string]any)["rules"], "a folder grant is no rule")

	got = mutate(t, srv, `mutation { folderRevoke(id: "`+first["id"].(string)+`", duration: Always) `+foldersFields+` }`)
	assert.Empty(t, got["folderRevoke"].(map[string]any)["always"])
	res := post(t, srv, `mutation { folderRevoke(id: "`+first["id"].(string)+`", duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", res.Errors[0].Extensions["code"])
}

func TestAChatsFolderGrantIsListedAndRemoved(t *testing.T) {
	srv, home := newFolderServer(t)
	code := filepath.Join(home, "code")
	chat := newChatOn(t, srv)

	got := mutate(t, srv, `mutation { folderGrant(chatID: "`+chat+`", path: "`+code+`", write: false, duration: Chat) `+foldersFields+` }`)
	assert.Empty(t, got["folderGrant"].(map[string]any)["always"])
	chatFolders := got["folderGrant"].(map[string]any)["chat"].([]any)
	require.Len(t, chatFolders, 1)
	id := chatFolders[0].(map[string]any)["id"].(string)

	got = mutate(t, srv, `{ chatPermissionRulesGet(chatID: "`+chat+`") { id } }`)
	assert.Equal(t, []any{}, got["chatPermissionRulesGet"], "a folder grant is no rule")
	folders := firstFrame(t, srv.URL, `subscription { sandboxFoldersStateWatch(chatID: "`+chat+`") `+foldersFields+` }`)["sandboxFoldersStateWatch"]
	assert.Len(t, folders.(map[string]any)["chat"], 1, "the chat's folders are listed for it")

	res := post(t, srv, `mutation { folderRevoke(id: "`+id+`", duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", res.Errors[0].Extensions["code"], "a chat's grant is no always one")
	got = mutate(t, srv, `mutation { folderRevoke(chatID: "`+chat+`", id: "`+id+`", duration: Chat) `+foldersFields+` }`)
	assert.Empty(t, got["folderRevoke"].(map[string]any)["chat"])
	for name, query := range map[string]string{
		"an unknown id":  `mutation { folderRevoke(chatID: "` + chat + `", id: "` + id + `", duration: Chat) { wide } }`,
		"a deleted chat": `mutation { folderRevoke(chatID: "` + appdb.NewID() + `", id: "` + id + `", duration: Chat) { wide } }`,
		"a chat's grant": `mutation { folderGrant(chatID: "` + appdb.NewID() + `", path: "` + code + `", write: false, duration: Chat) { wide } }`,
	} {
		res := post(t, srv, query)
		require.Len(t, res.Errors, 1, name)
		assert.Equal(t, "KUBETAIL_RECORD_NOT_FOUND", res.Errors[0].Extensions["code"], name)
	}
	for _, chatID := range []string{"", `chatID: "", `} {
		for _, query := range []string{
			`mutation { folderGrant(` + chatID + `path: "` + code + `", write: false, duration: Chat) { wide } }`,
			`mutation { folderRevoke(` + chatID + `id: "` + id + `", duration: Chat) { wide } }`,
		} {
			res := post(t, srv, query)
			require.Len(t, res.Errors, 1)
			assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", res.Errors[0].Extensions["code"], "a chat's grant names its chat")
		}
	}
	folders = firstFrame(t, srv.URL, `subscription { sandboxFoldersStateWatch `+foldersFields+` }`)["sandboxFoldersStateWatch"]
	assert.Empty(t, folders.(map[string]any)["always"], "and never widens to every chat")
}

// foldersFrame is the first sandboxFoldersStateWatch frame on events whose always
// and chat lists hold always and chat grants: a state watch over two sources can say
// one thing twice, so a frame is never counted.
func foldersFrame(t *testing.T, events <-chan sseEvent, always, chat int) map[string]any {
	t.Helper()
	for {
		ev := testutil.Recv(t, events, "a sandboxFoldersStateWatch frame")
		var payload struct {
			Data struct {
				Folders map[string]any `json:"sandboxFoldersStateWatch"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(ev.data), &payload), ev.data)
		folders := payload.Data.Folders
		if len(folders["always"].([]any)) == always && len(folders["chat"].([]any)) == chat {
			return folders
		}
	}
}

// The watch answers the folders on subscribe, and again after an always grant
// or revoke and after a write or revoke of the chat's own; one naming no chat
// follows the always grants alone.
func TestSandboxFoldersStateWatchFollowsEveryGrant(t *testing.T) {
	srv, home := newFolderServer(t)
	code := filepath.Join(home, "code")
	chatID := newChatOn(t, srv)

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxFoldersStateWatch(chatID: "`+chatID+`") `+foldersFields+` }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	first := foldersFrame(t, events, 0, 0)
	assert.Equal(t, []any{filepath.Join(home, ".ssh")}, first["never"])
	assert.Contains(t, first["wide"], home)

	unscoped := openSSESubscription(t, srv.URL, "", `subscription { sandboxFoldersStateWatch `+foldersFields+` }`)
	defer unscoped.Body.Close()
	alone := sseEvents(t, unscoped)
	foldersFrame(t, alone, 0, 0)

	got := mutate(t, srv, `mutation { folderGrant(path: "`+code+`", write: false, duration: Always) { always { id } } }`)
	alwaysID := got["folderGrant"].(map[string]any)["always"].([]any)[0].(map[string]any)["id"].(string)
	foldersFrame(t, events, 1, 0)
	foldersFrame(t, alone, 1, 0)

	got = mutate(t, srv, `mutation { folderGrant(chatID: "`+chatID+`", path: "`+code+`", write: true, duration: Chat) { chat { id } } }`)
	chatGrantID := got["folderGrant"].(map[string]any)["chat"].([]any)[0].(map[string]any)["id"].(string)
	granted := foldersFrame(t, events, 1, 1)
	assert.Equal(t, true, granted["chat"].([]any)[0].(map[string]any)["write"])

	mutate(t, srv, `mutation { folderRevoke(chatID: "`+chatID+`", id: "`+chatGrantID+`", duration: Chat) { wide } }`)
	foldersFrame(t, events, 1, 0)

	mutate(t, srv, `mutation { folderRevoke(id: "`+alwaysID+`", duration: Always) { wide } }`)
	foldersFrame(t, events, 0, 0)
	foldersFrame(t, alone, 0, 0)
}

func TestAFolderRefusalCarriesItsReason(t *testing.T) {
	srv, home := newFolderServer(t)
	link := filepath.Join(home, "link")
	require.NoError(t, os.Symlink(filepath.Join(home, "code"), link))

	res := post(t, srv, `mutation { folderGrant(path: "`+link+`", write: false, duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	e := res.Errors[0]
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", e.Extensions["code"])
	assert.Equal(t, "link", e.Extensions["rule"])
	assert.Equal(t, filepath.Join(home, "code"), e.Extensions["target"])
	assert.Equal(t, link+" is a link to "+filepath.Join(home, "code")+"; grant "+filepath.Join(home, "code")+" instead.", e.Message)

	res = post(t, srv, `mutation { folderGrant(path: "`+home+`", write: true, duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "Your home can be granted read-only.", res.Errors[0].Message)
	assert.Equal(t, "home", res.Errors[0].Extensions["rule"])
}

func TestARefusedFolderIsDrawnRefused(t *testing.T) {
	srv, home := newFolderServer(t)
	gone := filepath.Join(home, "gone")
	require.NoError(t, os.MkdirAll(gone, 0o755))
	mutate(t, srv, `mutation { folderGrant(path: "`+gone+`", write: false, duration: Always) { wide } }`)
	require.NoError(t, os.Remove(gone))

	folders := firstFrame(t, srv.URL, `subscription { sandboxFoldersStateWatch `+foldersFields+` }`)["sandboxFoldersStateWatch"]
	always := folders.(map[string]any)["always"].([]any)
	require.Len(t, always, 1)
	assert.Equal(t, "This folder does not exist.", always[0].(map[string]any)["refusal"])
}

func TestSandboxFoldersWithNoSandbox(t *testing.T) {
	srv := newChatServer(t)
	folders := firstFrame(t, srv.URL, `subscription { sandboxFoldersStateWatch `+foldersFields+` }`)["sandboxFoldersStateWatch"]
	assert.Equal(t, map[string]any{"always": []any{}, "chat": []any{}, "never": []any{}, "closed": []any{}, "wide": []any{}}, folders)

	res := post(t, srv, `mutation { folderGrant(path: "/home/me/code", write: false, duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", res.Errors[0].Extensions["code"])
}

// A read of the folders that fails ends the watch with its reason, never empty
// lists, so the client reconnects.
func TestSandboxFoldersFailWithTheirRead(t *testing.T) {
	home := testutil.GrantableDir(t)
	store, err := settings.Open(appdbtest.Open(t))
	require.NoError(t, err)
	security := settings.NewService(store, func() settings.Zones { return settings.Zones{Home: home} }, nil, nil, "")
	srv, db, _ := newChatServerOn(t, sandbox.State{Available: true}, security)
	chatID := newChatOn(t, srv)
	_, err = db.Write.Exec(`DROP TABLE folder_grants`)
	require.NoError(t, err)

	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxFoldersStateWatch(chatID: "`+chatID+`") { wide } }`)
	defer resp.Body.Close()
	ev := testutil.Recv(t, sseEvents(t, resp), "the failure frame")
	assert.Contains(t, ev.data, `"subscriptionFailed":true`)
	assert.Contains(t, ev.data, "read the sandbox folders")
}

// A grant or revoke whose write took, though the lists it answers could not be
// read after, says the write took, so no one reads it as a refusal.
func TestAFolderWriteThatTookSaysSoWhenTheListsFail(t *testing.T) {
	home := testutil.GrantableDir(t)
	code := filepath.Join(home, "code")
	require.NoError(t, os.MkdirAll(code, 0o755))
	settingsDB := appdbtest.Open(t)
	store, err := settings.Open(settingsDB)
	require.NoError(t, err)
	security := settings.NewService(store, func() settings.Zones { return settings.Zones{Home: home} }, nil, nil, "")
	srv, _, _ := newChatServerOn(t, sandbox.State{Available: true}, security)
	// A row whose folder no read can scan, beside the index the grant's upsert needs.
	for _, q := range []string{
		`DROP TABLE folder_grants`,
		`CREATE TABLE folder_grants (id, chat_id, folder, access, created_at)`,
		`CREATE UNIQUE INDEX folder_grants_folder ON folder_grants (ifnull(chat_id, ''), folder)`,
		`INSERT INTO folder_grants VALUES ('bad', NULL, NULL, 'read', 0)`,
	} {
		_, err := settingsDB.Write.Exec(q)
		require.NoError(t, err)
	}

	res := post(t, srv, `mutation { folderGrant(path: "`+code+`", write: false, duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_SAVED_UNREAD", res.Errors[0].Extensions["code"])
	assert.Contains(t, res.Errors[0].Message, "The folder was granted, but Kubetail could not read the settings back")
	var id string
	require.NoError(t, settingsDB.Read.QueryRow(`SELECT id FROM folder_grants WHERE folder = ?`, code).Scan(&id))

	res = post(t, srv, `mutation { folderRevoke(id: "`+id+`", duration: Always) { wide } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_SAVED_UNREAD", res.Errors[0].Extensions["code"])
	assert.Contains(t, res.Errors[0].Message, "The grant was removed, but Kubetail could not read the settings back")
	var n int
	require.NoError(t, settingsDB.Read.QueryRow(`SELECT count(*) FROM folder_grants WHERE folder = ?`, code).Scan(&n))
	assert.Zero(t, n)
}

// sandboxPathServer is a server over a Service whose zones read open as every
// run's and whose home is home, its shell's last PATH open then closed, and
// whose refresh answers resolve; with no sandbox the Service has neither.
func sandboxPathServer(t *testing.T, available bool, resolve func(context.Context) ([]string, error)) (srv *httptest.Server, svc *settings.Service, db *appdb.DB, base string) {
	t.Helper()
	base = testutil.GrantableDir(t)
	for _, d := range []string{"open", "home/closed", "home/bin"} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, d), 0o755))
	}
	db = appdbtest.Open(t)
	store, err := settings.Open(db)
	require.NoError(t, err)
	svc = settings.NewService(store, nil, nil, nil, "")
	if available {
		zones := func() settings.Zones {
			return settings.Zones{Open: sandbox.FilePolicy{Read: []string{filepath.Join(base, "open")}}, Home: filepath.Join(base, "home")}
		}
		svc = settings.NewService(store, zones, resolve, nil, "timeout")
		_, err := svc.SetShellPath(t.Context(), []string{filepath.Join(base, "open"), filepath.Join(base, "home/closed")})
		require.NoError(t, err)
	}
	srv = httptest.NewServer(graph.NewServer(context.Background(), &graph.Resolver{Settings: svc, SandboxState: sandbox.State{Available: available}}))
	t.Cleanup(srv.Close)
	return srv, svc, db, base
}

const sandboxPathFields = `{ value refusal folders { path origin target searched reason addable shared } }`

// The setting and each folder as a run judges it; a save answers the new
// setting, and a value that does not parse, or a refresh whose shell fails,
// is refused in the user's words.
func TestSandboxPathAnswersTheSettingAndItsFolders(t *testing.T) {
	srv, _, _, base := sandboxPathServer(t, true, func(context.Context) ([]string, error) {
		return nil, errors.New("no shell")
	})
	open, closed, bin := filepath.Join(base, "open"), filepath.Join(base, "home/closed"), filepath.Join(base, "home/bin")
	folder := func(path, origin string, searched, addable bool, reason any) map[string]any {
		return map[string]any{"path": path, "origin": origin, "target": path, "searched": searched, "reason": reason, "addable": addable, "shared": false}
	}

	res := post(t, srv, `{ sandboxPathGet `+sandboxPathFields+` sandboxPathFaultGet }`)
	require.Empty(t, res.Errors)
	path := res.Data["sandboxPathGet"].(map[string]any)
	assert.Equal(t, "$PATH", path["value"])
	assert.Nil(t, path["refusal"])
	assert.Equal(t, []any{
		folder(open, "Shell", true, false, nil),
		folder(closed, "Shell", false, true, "No sandboxed command reads this folder; type it into your PATH to include it."),
	}, path["folders"])
	assert.Equal(t, "timeout", res.Data["sandboxPathFaultGet"])

	got := mutate(t, srv, `mutation { sandboxPathSet(value: "~/bin:$PATH") `+sandboxPathFields+` }`)
	path = got["sandboxPathSet"].(map[string]any)
	assert.Equal(t, "~/bin:$PATH", path["value"])
	assert.Equal(t, folder(bin, "Typed", true, false, nil), path["folders"].([]any)[0])

	for query, message := range map[string]string{
		`mutation { sandboxPathSet(value: "bin") { value } }`: `"bin" is not a full path.`,
		`mutation { sandboxPathRefresh { value } }`:           "Your shell did not answer: no shell.",
	} {
		res := post(t, srv, query)
		require.Len(t, res.Errors, 1, query)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", res.Errors[0].Extensions["code"], query)
		assert.Equal(t, message, res.Errors[0].Message, query)
	}
	res = post(t, srv, `{ sandboxPathFaultGet }`)
	assert.Equal(t, "no shell", res.Data["sandboxPathFaultGet"], "the refresh's fault replaces the launch's")
}

// A machine with no sandbox answers the setting with no folders and no fault,
// and refuses every change.
func TestSandboxPathHasNoFoldersWithNoSandbox(t *testing.T) {
	srv, _, _, _ := sandboxPathServer(t, false, nil)

	res := post(t, srv, `{ sandboxPathGet `+sandboxPathFields+` sandboxPathFaultGet }`)
	require.Empty(t, res.Errors)
	assert.Equal(t, map[string]any{"value": "$PATH", "refusal": nil, "folders": []any{}}, res.Data["sandboxPathGet"])
	assert.Nil(t, res.Data["sandboxPathFaultGet"])
	for _, query := range []string{
		`mutation { sandboxPathSet(value: "$PATH") { value } }`,
		`mutation { sandboxPathRefresh { value } }`,
	} {
		res := post(t, srv, query)
		require.Len(t, res.Errors, 1, query)
		assert.Equal(t, "KUBETAIL_VALIDATION_ERROR", res.Errors[0].Extensions["code"], query)
	}
}

// A refused value is answered with its refusal and no folders.
func TestARefusedSandboxPathIsAnsweredWithItsRefusal(t *testing.T) {
	srv, _, db, _ := sandboxPathServer(t, true, nil)
	_, err := db.Write.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.path', 'bin', 0, 0)`)
	require.NoError(t, err)

	res := post(t, srv, `{ sandboxPathGet `+sandboxPathFields+` }`)
	require.Empty(t, res.Errors)
	path := res.Data["sandboxPathGet"].(map[string]any)
	assert.Equal(t, `"bin" is not a full path.`, path["refusal"])
	assert.Equal(t, []any{}, path["folders"])
}

// A read that fails is the query's error; a save whose write took answers
// KUBETAIL_SAVED_UNREAD.
func TestASandboxPathReadThatFailsIsAnError(t *testing.T) {
	srv, _, db, _ := sandboxPathServer(t, true, nil)
	_, err := db.Write.Exec(`DROP TABLE shell_path`)
	require.NoError(t, err)

	res := post(t, srv, `{ sandboxPathGet { value } }`)
	require.Len(t, res.Errors, 1)
	res = post(t, srv, `mutation { sandboxPathSet(value: "/opt/bin") { value } }`)
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "KUBETAIL_SAVED_UNREAD", res.Errors[0].Extensions["code"])
	assert.Contains(t, res.Errors[0].Message, "Your PATH was saved")
	var value string
	require.NoError(t, db.Read.QueryRow(`SELECT value FROM settings WHERE key = 'sandbox.path'`).Scan(&value))
	assert.Equal(t, "/opt/bin", value)
}

// The folders watch reads again on every read of the shell, unchanged PATH
// included, since new zones can change a grant's answer; a PATH read that
// fails inside a grant's check ends it, never a frame of refused grants.
func TestTheFoldersWatchFollowsThePath(t *testing.T) {
	srv, home := newFolderServer(t)
	code := filepath.Join(home, "code")
	mutate(t, srv, `mutation { folderGrant(path: "`+code+`", write: false, duration: Always) { wide } }`)
	resp := openSSESubscription(t, srv.URL, "", `subscription { sandboxFoldersStateWatch `+foldersFields+` }`)
	defer resp.Body.Close()
	events := sseEvents(t, resp)
	foldersFrame(t, events, 1, 0)

	res := post(t, srv, `mutation { sandboxPathSet(value: "$PATH:~/code") { value } }`)
	require.Empty(t, res.Errors)
	foldersFrame(t, events, 1, 0)
}

// A refresh whose shell answers stores its PATH and answers the setting over
// it.
func TestSandboxPathRefreshAnswersTheSetting(t *testing.T) {
	var answer []string
	srv, _, _, base := sandboxPathServer(t, true, func(context.Context) ([]string, error) { return answer, nil })
	bin := filepath.Join(base, "open/bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	answer = []string{bin}

	got := mutate(t, srv, `mutation { sandboxPathRefresh `+sandboxPathFields+` }`)
	folders := got["sandboxPathRefresh"].(map[string]any)["folders"].([]any)
	require.Len(t, folders, 1)
	assert.Equal(t, bin, folders[0].(map[string]any)["path"])
	res := post(t, srv, `{ sandboxPathFaultGet }`)
	assert.Nil(t, res.Data["sandboxPathFaultGet"])
}

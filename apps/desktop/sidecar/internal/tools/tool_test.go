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

package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
)

// An action's kind is the field it sets, and none for an action that sets none.
func TestAnActionsKindIsItsField(t *testing.T) {
	assert.Equal(t, ActionCommand, Action{Command: &CommandAction{}}.Kind())
	assert.Equal(t, ActionRead, Action{Read: &ReadAction{}}.Kind())
	assert.Equal(t, ActionWrite, Action{Write: &WriteAction{}}.Kind())
	assert.Equal(t, ActionEdit, Action{Edit: &EditAction{}}.Kind())
	assert.Equal(t, ActionSearch, Action{Search: &SearchAction{}}.Kind())
	assert.Equal(t, ActionFetch, Action{Fetch: &FetchAction{}}.Kind())
	assert.Equal(t, ActionMemory, Action{Memory: &MemoryAction{}}.Kind())
	assert.Equal(t, ActionDelegate, Action{Delegate: &DelegateAction{}}.Kind())
	assert.Equal(t, ActionKubeQuery, Action{KubeQuery: &KubeQueryAction{}}.Kind())
	assert.Equal(t, ActionLogsView, Action{LogsView: &LogsViewAction{}}.Kind())
	assert.Equal(t, ActionLogsRead, Action{LogsRead: &LogsReadAction{}}.Kind())
	assert.Equal(t, ActionLogsWatch, Action{LogsWatch: &LogsWatchAction{}}.Kind())
	assert.Equal(t, ActionKind(""), Action{Description: "no kind"}.Kind())
}

// ActionKinds is the set valid reads: each kind once, and nothing else.
func TestActionKindsIsTheSet(t *testing.T) {
	seen := map[ActionKind]bool{}
	for _, k := range ActionKinds {
		assert.False(t, seen[k], "%s twice", k)
		seen[k] = true
		assert.True(t, k.valid(), k)
	}
	assert.Len(t, ActionKinds, 13)
	assert.False(t, ActionKind("").valid())
	assert.False(t, ActionKind("unknown").valid())
}

// A refusal's error carries the result the model reads.
func TestARefusalsErrorIsItsResult(t *testing.T) {
	assert.EqualError(t, &Refusal{Result: `{"error":"bad-input"}`}, `refused: {"error":"bad-input"}`)
}

// A view's backend half is the log service's input, and the action's JSON keeps
// its flat shape: the row stores it as written.
func TestLogsViewActionEmbedsTheInput(t *testing.T) {
	at := time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC)
	action := LogsViewAction{
		LogsScopeInput: LogsScopeInput{
			Sources: []LogsViewSource{{Namespace: "prod", Kind: LogsSourceDeployment, Name: "web", Containers: []string{"app"}}},
			Filters: []LogsViewFilter{{Field: LogsFilterZone, Values: []string{"a"}}},
			Grep:    "err",
		},
		Anchor:     LogsViewAnchor{Kind: LogsAnchorAt, At: &at},
		PinToEnd:   true,
		ResolvedAt: at.Add(time.Minute),
	}

	raw, err := json.Marshal(action)

	require.NoError(t, err)
	assert.JSONEq(t, `{"sources":[{"namespace":"prod","kind":"deployment","name":"web","containers":["app"],"allContainers":false,"previous":false}],
"filters":[{"field":"zone","values":["a"]}],"grep":"err","anchor":{"kind":"at","at":"2026-10-08T14:00:00Z"},"pinToEnd":true,
"resolvedAt":"2026-10-08T14:01:00Z"}`, string(raw))
	var view logs.Scope = action.LogsScopeInput
	assert.Equal(t, "err", view.Grep)
}

// A read's action keeps the window as the service ran it: the paddings, which end
// the first page came from (null on a continuation), whether a page lies each way,
// and the notices.
func TestLogsReadActionKeepsTheWindow(t *testing.T) {
	since := time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC)
	first := logs.PageFromFirst
	action := LogsReadAction{
		LogsScopeInput: LogsScopeInput{Sources: []LogsViewSource{}, Filters: []LogsViewFilter{}},
		Since:          &since,
		Before:         LogsPadding{Lines: 10},
		After:          LogsPadding{Span: "PT5M"},
		PageFrom:       &first,
		ResolvedAt:     since.Add(time.Minute),
		Lines:          []LogsReadLine{{Timestamp: since, Message: "a…", TruncatedBytes: 7}},
		NewerMore:      true,
		Notices:        []LogsNotice{{Code: "padding-short", Message: "fewer lines"}},
	}

	raw, err := json.Marshal(action)

	require.NoError(t, err)
	assert.JSONEq(t, `{"sources":[],"filters":[],"grep":"","since":"2026-10-08T14:00:00Z","until":null,
"before":{"lines":10,"span":""},"after":{"lines":0,"span":"PT5M"},"pageFrom":"first","resolvedAt":"2026-10-08T14:01:00Z",
"lines":[{"timestamp":"2026-10-08T14:00:00Z","namespace":"","pod":"","container":"","previous":false,"message":"a…","gap":null,"truncatedBytes":7}],
"olderMore":false,"newerMore":true,"notices":[{"code":"padding-short","message":"fewer lines"}],"viewAround":null}`, string(raw))

	action.PageFrom = nil
	raw, err = json.Marshal(action)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"pageFrom":null`)
}

// A cloud request round-trips through the approval's request column, and a
// stored cluster request, in the keys migration 0006 writes, reads back.
func TestACloudRequestRoundTripsAsJSON(t *testing.T) {
	in := ActionRequest{
		Action: permissions.Action{
			Principal: permissions.Chat, Target: permissions.AWS, Scope: "123456789012/eu-west-1", AccountName: "123456789012",
			Service: "ec2", Operation: "TerminateInstances", AccessLevel: permissions.Write, Once: true,
			Summary: "AWS: call ec2:TerminateInstances in eu-west-1",
		},
		Grantable: false,
		Cloud: &CloudRequest{
			Host: "ec2.eu-west-1.amazonaws.com", Method: "POST", Path: "/?a=1", ContentType: "application/x-www-form-urlencoded",
			Body: "Action=TerminateInstances", Details: []Detail{{Label: "Region", Value: "eu-west-1"}},
		},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out ActionRequest
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
	assert.NotContains(t, string(b), `"write":`)

	var stored ActionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"action":{"principal":"","target":"kubernetes","accessLevel":"write","scope":"",
		"accountName":"dev","service":"","operation":"delete","resource":"pods","name":"b","summary":"Delete pods/b in a on dev",
		"dryRun":false,"confined":false,"once":false},"write":{"method":"DELETE","path":"/api/v1/namespaces/a/pods/b"}}`), &stored))
	assert.Nil(t, stored.Cloud)
	assert.Equal(t, permissions.Action{
		Target: permissions.Kubernetes, AccessLevel: permissions.Write, AccountName: "dev",
		Operation: "delete", Resource: "pods", Name: "b", Summary: "Delete pods/b in a on dev",
	}, stored.Action)
	assert.Equal(t, "DELETE", stored.Write.Method)
}

// A tool that finds another server than the run's tells the run through
// ServerMoved; a runtime with no OnServerMoved, a chat's, ignores it.
func TestServerMovedTellsTheRun(t *testing.T) {
	Runtime{}.ServerMoved()

	moved := 0
	Runtime{ServerUID: "uid-1", OnServerMoved: func() { moved++ }}.ServerMoved()
	assert.Equal(t, 1, moved)
}

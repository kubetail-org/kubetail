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

package kubequery

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

func TestTheDefinitionIsTheEmbeddedSchema(t *testing.T) {
	def := New(nil).Definition()

	assert.Equal(t, Name, def.Name)
	assert.Equal(t, description, def.Description)
	assert.JSONEq(t, string(inputSchema), string(def.InputSchema))
}

// inputErrorOf is the refusal Action answers raw with.
func inputErrorOf(t *testing.T, raw string) *inputError {
	t.Helper()
	_, err := New(nil).Action(json.RawMessage(raw), "", false)
	var ie *inputError
	require.ErrorAs(t, err, &ie, raw)
	return ie
}

// kubeQueryOf is the action Action reads off raw.
func kubeQueryOf(t *testing.T, raw string) tools.KubeQueryAction {
	t.Helper()
	a, err := New(nil).Action(json.RawMessage(raw), "", false)
	require.NoError(t, err, raw)
	require.NotNil(t, a.KubeQuery, raw)
	return *a.KubeQuery
}

// The loop bounds every call at 10 s, and a call asks no one.
func TestTheCallIsBounded(t *testing.T) {
	var tool tools.Tool = New(nil)
	bounded, ok := tool.(tools.Bounded)
	require.True(t, ok)
	assert.Equal(t, 10*time.Second, bounded.CallTimeout(json.RawMessage(`{"sql":"SELECT 1"}`)))
	_, gated := tool.(tools.Gated)
	assert.False(t, gated)
}

// SQLite separates statements with ; alone, and modernc runs every statement of a
// script, so a ; may only end the statement.
func TestASemicolonBeforeTheEndIsRefused(t *testing.T) {
	for _, raw := range []string{`{"sql":"SELECT 1; SELECT 2"}`, `{"sql":"SELECT ';'"}`, `{"sql":"SELECT 1;;"}`} {
		assert.Equal(t, &inputError{field: "sql", message: semicolonMessage}, inputErrorOf(t, raw), raw)
	}
}

func TestATrailingSemicolonIsDropped(t *testing.T) {
	assert.Equal(t, "SELECT 1", kubeQueryOf(t, `{"sql":"SELECT 1 ;  \n"}`).SQL)
	assert.Equal(t, "SELECT 1", kubeQueryOf(t, `{"sql":"  SELECT 1"}`).SQL)
}

func TestABlankStatementIsRefused(t *testing.T) {
	for _, raw := range []string{`{"sql":""}`, `{"sql":"  "}`, `{"sql":" ; "}`, `{"sql":3}`, `{"limit":5}`} {
		ie := inputErrorOf(t, raw)
		assert.Equal(t, "sql", ie.field, raw)
		assert.Equal(t, "kubequery input has a bad sql", ie.Error())
	}
}

func TestALimitPastTheCapReadsTheCap(t *testing.T) {
	assert.Equal(t, 2000, kubeQueryOf(t, `{"sql":"SELECT 1","limit":5000}`).Limit)
	assert.Equal(t, 200, kubeQueryOf(t, `{"sql":"SELECT 1"}`).Limit)
	assert.Equal(t, 1, kubeQueryOf(t, `{"sql":"SELECT 1","limit":1}`).Limit)
}

func TestABadLimitIsRefused(t *testing.T) {
	for _, limit := range []string{`0`, `-1`, `1.5`, `"10"`, `null`, `1e3`} {
		raw := `{"sql":"SELECT 1","limit":` + limit + `}`
		assert.Equal(t, "limit", inputErrorOf(t, raw).field, raw)
	}
}

// A key the tool does not know is refused without naming it, so nothing the model
// wrote comes back to it.
func TestAnUnknownKeyIsRefusedWithoutEcho(t *testing.T) {
	for _, raw := range []string{`{"sql":"SELECT 1","hunter2":1}`, `{"sql":"SELECT 1","sql":"SELECT 2"}`, `not json`, `[]`, `{"sql":"SELECT 1"} {}`,
		`{1:2}`, `{"sql":}`, `{"sql":"SELECT 1"`} {
		ie := inputErrorOf(t, raw)
		assert.Empty(t, ie.field, raw)
		assert.NotContains(t, ie.Error(), "hunter2")
	}
}

func TestTheActionIsTheQuery(t *testing.T) {
	a, err := New(nil).Action(json.RawMessage(`{"sql":"SELECT 1;","limit":5,"description":"Count the pods"}`), "", false)

	require.NoError(t, err)
	assert.Equal(t, tools.Action{Description: "Count the pods", KubeQuery: &tools.KubeQueryAction{SQL: "SELECT 1", Limit: 5}}, a)
	assert.Equal(t, tools.ActionKubeQuery, New(nil).ActionKind())
}

// watching is a cluster whose verdict lets rows through, answering every query with
// columns and rows.
func watching(columns []string, rows ...[]any) *fakeService {
	f := healthy()
	f.query = cluster.ClusterCachedDataQueryResult{Columns: columns, Rows: rows}
	return f
}

// run calls the tool over f with raw, for cluster 7.
func run(t *testing.T, f *fakeService, raw string) (string, bool) {
	t.Helper()
	return New(f).Run(t.Context(), tools.Runtime{ClusterID: "7"}, json.RawMessage(raw))
}

func TestARefusalNamesTheFieldNeverTheValue(t *testing.T) {
	for raw, want := range map[string]string{
		`{"sql":"SELECT 1; DROP"}`:       `{"error":"bad-input","field":"sql","message":"` + semicolonMessage + `"}`,
		`{"sql":"SELECT 1","limit":0}`:   `{"error":"bad-input","field":"limit"}`,
		`{"sql":"SELECT 1","hunter2":1}`: `{"error":"bad-input"}`,
	} {
		text, isError := run(t, watching(nil), raw)
		assert.True(t, isError, raw)
		assert.JSONEq(t, want, text, raw)
	}
}

func TestNoCacheIsARefusal(t *testing.T) {
	text, isError := run(t, &fakeService{readErr: cluster.ErrNotFound}, `{"sql":"SELECT 1"}`)
	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"no-cache"}`, text)

	gone := healthy()
	gone.queryGone = true
	text, isError = run(t, gone, `{"sql":"SELECT 1"}`)
	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"no-cache"}`, text)
}

// SQLite's complaint is the model's to act on, redacted as any text the model reads is.
func TestASQLErrorCarriesItsMessageAlone(t *testing.T) {
	msg := `near "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln": syntax error`
	f := healthy()
	f.queryErr = &cluster.QueryError{Message: msg}
	text, isError := run(t, f, `{"sql":"SELECT 1"}`)
	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"sql","message":"near \"[redacted]\": syntax error"}`, text)
}

func TestAReadFailureCarriesNoText(t *testing.T) {
	f := healthy()
	f.queryErr = errors.New("open cache 7: disk I/O error at /home/user")
	text, isError := run(t, f, `{"sql":"SELECT 1"}`)
	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"read-failed"}`, text)
}

func TestAWithheldResultCarriesNoRows(t *testing.T) {
	text, isError := run(t, connecting(), `{"sql":"SELECT 1"}`)
	assert.False(t, isError)
	assert.Equal(t, `{"cluster":"prod","freshness":{"status":"syncing"}}`, text)
}

// The rows come last and one to a line, so a preview shows the verdict and the columns.
func TestRowsAreOneToALine(t *testing.T) {
	m := watching([]string{"namespace", "name", "restarts"},
		[]any{"shop", "api-0", int64(3)}, []any{"shop", "api-1", nil})
	m.query.More = true

	text, isError := run(t, m, `{"sql":"SELECT namespace, name, restarts FROM objects;","limit":2}`)

	assert.False(t, isError)
	assert.Equal(t, `{"cluster":"prod","freshness":{"status":"watching"},"columns":["namespace","name","restarts"],"more":true,"rows":[`+"\n"+
		`["shop","api-0",3],`+"\n"+
		`["shop","api-1",null]`+"\n"+
		`]}`, text)
	assert.Equal(t, []queried{{11, "SELECT namespace, name, restarts FROM objects", 2, tools.FileLimit, nil}}, m.queried)
	assert.True(t, json.Valid([]byte(text)))
}

func TestNoRowsIsAnEmptyList(t *testing.T) {
	text, _ := run(t, watching([]string{"name"}), `{"sql":"SELECT name FROM objects"}`)
	assert.Equal(t, `{"cluster":"prod","freshness":{"status":"watching"},"columns":["name"],"more":false,"rows":[]}`, text)
}

// rowOf runs one row of cells through the tool and answers its line.
func rowOf(t *testing.T, cells ...any) string {
	t.Helper()
	columns := make([]string, len(cells))
	for i := range columns {
		columns[i] = "c"
	}
	text, isError := run(t, watching(columns, cells), `{"sql":"SELECT 1"}`)
	require.False(t, isError)
	require.True(t, json.Valid([]byte(text)), text)
	lines := strings.Split(text, "\n")
	require.Len(t, lines, 3, text)
	return lines[1]
}

// A cell holding a JSON object or array is nested, compact, so its row stays one line.
func TestAJSONCellIsNested(t *testing.T) {
	assert.Equal(t, `[{"a":1,"b":[true,null]},["x"]]`, rowOf(t, "{\n  \"a\": 1, \"b\": [true, null]\n}", ` ["x"]`))
}

func TestATextCellThatIsNotJSONStaysAString(t *testing.T) {
	assert.Equal(t, `["hello <world> & co","{ not json"]`, rowOf(t, "hello <world> & co", "{ not json"))
}

func TestABracedCellThatIsNotJSONIsRedactedAsText(t *testing.T) {
	assert.Equal(t, `["[warn]\ntoken: [redacted]"]`, rowOf(t, "[warn]\ntoken: abc"))
}

func TestABlobCellReadsAsItsSize(t *testing.T) {
	assert.Equal(t, `["<blob 3 bytes>"]`, rowOf(t, []byte{1, 2, 3}))
}

func TestAnUnrepresentableFloatIsNull(t *testing.T) {
	assert.Equal(t, `[null,null,null,1.5,-2]`, rowOf(t, math.NaN(), math.Inf(1), math.Inf(-1), 1.5, int64(-2)))
}

// Every text is redacted as a command's output is, whether the query read a column, a
// json_extract of a body, or a whole body.
func TestEveryTextCellIsRedacted(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"
	pod := `{"spec":{"containers":[{"args":["--token=abc"]}]},"status":{"phase":"Running"}}`
	configMap := `{"data":{"app.env":"USER=a\npassword=hunter2\nHOST=b"}}`

	assert.Equal(t, `["[redacted]",`+
		`{"spec":{"containers":[{"args":["--token=[redacted]"]}]},"status":{"phase":"Running"}},`+
		`{"data":{"app.env":"USER=a\npassword=[redacted]\nHOST=b"}}]`,
		rowOf(t, jwt, pod, configMap))
}

// chatDir is a chat's directory at a path of the test's.
type chatDir string

func (d chatDir) Path() string { return string(d) }

func (d chatDir) Root(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(string(d), 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(string(d))
}

// wide is n rows of one text cell, each size bytes.
func wide(n, size int) *fakeService {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{strings.Repeat("x", size)}
	}
	m := watching([]string{"body"})
	m.query.Rows = rows
	return m
}

// savedResult is the file a persisted answer names, and the result it holds.
func savedResult(t *testing.T, text string) (map[string]any, string) {
	t.Helper()
	require.True(t, strings.HasPrefix(text, "<persisted-output>\n"), text[:min(len(text), 200)])
	path := strings.TrimPrefix(strings.SplitN(strings.SplitN(text, "\n", 3)[1], "saved to: ", 2)[1], "")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var res map[string]any
	require.NoError(t, json.Unmarshal(raw, &res), "the saved result is whole JSON")
	return res, string(raw)
}

// Past the inline limit the result is saved to the chat's directory, with a preview that
// shows the verdict and the columns.
func TestALongResultIsSaved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c1")
	text, isError := New(wide(20, 2000)).Run(t.Context(), tools.Runtime{ClusterID: "7", Dir: chatDir(dir)},
		json.RawMessage(`{"sql":"SELECT body FROM objects"}`))

	assert.False(t, isError)
	assert.Contains(t, text, `{"cluster":"prod","freshness":{"status":"watching"},"columns":["body"],"more":false,"rows":[`)
	res, _ := savedResult(t, text)
	assert.Len(t, res["rows"], 20)
}

// The render stops before a row that would pass the file limit, so a saved result is
// whole rows, and says rows were left out.
func TestARenderPastTheFileLimitStopsAtARow(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c1")
	tool := New(wide(9, 8<<10))
	tool.fileLimit = 60 << 10
	text, _ := tool.Run(t.Context(), tools.Runtime{ClusterID: "7", Dir: chatDir(dir)},
		json.RawMessage(`{"sql":"SELECT body FROM objects"}`))

	res, raw := savedResult(t, text)
	assert.Len(t, res["rows"], 7)
	assert.Equal(t, true, res["more"])
	assert.LessOrEqual(t, len(raw), tool.fileLimit)
}

// A saved answer is bounded by the file limit, the largest file Read opens.
func TestASavedAnswerIsBoundedByTheFileLimit(t *testing.T) {
	assert.Equal(t, tools.FileLimit, New(nil).fileLimit)
}

// With nowhere to save, the answer is the render again within the inline limit: whole
// rows, never a cut mid-row.
func TestAFailedSaveAnswersWholeRowsInline(t *testing.T) {
	text, isError := run(t, wide(20, 2000), `{"sql":"SELECT body FROM objects"}`)

	assert.False(t, isError)
	assert.LessOrEqual(t, len(text), tools.InlineLimit)
	var res map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &res), text[:200])
	assert.Equal(t, true, res["more"])
	assert.Len(t, res["rows"], 14)
}

// The render checks its context between rows, so a slow redaction ends at the call's
// deadline; the loop reports the error as a timeout.
func TestARenderStopsAtTheDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	text, isError := New(wide(3, 10)).Run(ctx, tools.Runtime{ClusterID: "7"}, json.RawMessage(`{"sql":"SELECT 1"}`))

	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"read-failed"}`, text)
}

// The section lists the views that are built and nothing a later step adds: a view the
// model is told of and cannot read costs it a failed call.
func TestTheSectionListsTheBuiltViews(t *testing.T) {
	section := New(nil).Prompt()
	for _, view := range []string{"objects:", "labels, annotations:", "owners:", "ancestors:", "events:", "status_history:", "kinds:", "containers:", "selects:", "refs:", "events_fts", "changed_at", "quantity("} {
		assert.Contains(t, section, view)
	}
	assert.NotContains(t, section, "label_selector")
	// A null to_uid is a missing target only where the kind is listed, and refs is not every
	// reference: both are what keeps the model from calling a used Secret unused.
	for _, rule := range []string{"`to_listed` is 1", "It is not every reference", "`optional` 1"} {
		assert.Contains(t, section, rule)
	}
	assert.Contains(t, description, "events past their expiry")
}

// Dropping rows cannot bring an answer whose column names alone pass the bound within it,
// so that answer is a refusal of its own, inside the inline limit.
func TestColumnNamesPastTheBoundAreRefused(t *testing.T) {
	m := watching([]string{strings.Repeat("n", tools.InlineLimit+1)}, []any{int64(1)})

	text, isError := run(t, m, `{"sql":"SELECT 1"}`)

	assert.True(t, isError)
	assert.JSONEq(t, `{"error":"too-large","message":"`+columnsTooLong+`"}`, text)
	assert.LessOrEqual(t, len(text), tools.InlineLimit)
}

// Stored rows read the same forever: never edit this table.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	for raw, want := range map[string]tools.Action{
		`{"sql":"SELECT name FROM pods","description":"List pods"}`: {
			Description: "List pods", KubeQuery: &tools.KubeQueryAction{SQL: "SELECT name FROM pods", Limit: 200},
		},
		`{"sql":"SELECT count(*) FROM nodes","limit":5}`: {
			KubeQuery: &tools.KubeQueryAction{SQL: "SELECT count(*) FROM nodes", Limit: 5},
		},
	} {
		got, err := (&Tool{}).Action(json.RawMessage(raw), "", false)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

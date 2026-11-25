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

// Package kubequery is KubeQuery: read-only SQL over the chat's cluster's cache,
// run without asking.
package kubequery

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

//go:embed prompts/kubequery.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name KubeQuery is offered under.
const Name = "KubeQuery"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "kubequery/v1"

// callTimeout is the loop's bound on one call, whatever its input.
const callTimeout = 10 * time.Second

// The rows a call answers: defaultLimit unless it names a limit, and never past maxLimit.
const (
	defaultLimit = 200
	maxLimit     = 2000
)

// semicolonMessage is the refusal of a ; before the statement's end. The text is the
// tool's own, never the input's.
const semicolonMessage = "One statement: a ';' may only end it. Spell a literal semicolon char(59)."

var (
	_ tools.Custom  = (*Tool)(nil)
	_ tools.Bounded = (*Tool)(nil)
)

// Tool is KubeQuery, over the cluster service whose caches it reads.
type Tool struct {
	svc cluster.Service
	// fileLimit bounds a rendered answer in bytes: tools.FileLimit, or a test's.
	fileLimit int
}

// New is the tool over svc.
func New(svc cluster.Service) *Tool { return &Tool{svc: svc, fileLimit: tools.FileLimit} }

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionKubeQuery }

func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

// Action is the call's description and the statement as it runs, its limit as read.
func (t *Tool) Action(raw json.RawMessage, _ string, _ bool) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{
		Description: in.description,
		KubeQuery:   &tools.KubeQueryAction{SQL: in.sql, Limit: in.limit},
	}, nil
}

// The answers that are not rows.
const (
	noCache    = `{"error":"no-cache"}`
	readFailed = `{"error":"read-failed"}`
)

// columnsTooLong is the refusal of a result whose column names alone pass what an answer
// may hold: dropping rows cannot bring it within the bound.
const columnsTooLong = "The column names alone pass the size limit. Name the columns shorter."

// errColumnsTooLong is a render whose head passes its bound before any row.
var errColumnsTooLong = errors.New("kubequery: the column names pass the answer's bound")

// Run runs the call's statement over the active cache of the chat's cluster. A failure
// the model can act on names itself; any other carries none of its text.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return refusal(err), true
	}
	res, err := t.query(ctx, rt.ClusterID, in.sql, in.limit)
	var qe *cluster.QueryError
	switch {
	case errors.Is(err, errNoCache):
		return noCache, true
	case errors.As(err, &qe):
		return result(map[string]string{"error": "sql", "message": safe.String(qe.Message)}), true
	case err != nil:
		return readFailed, true
	}

	text, err := render(ctx, res, t.fileLimit)
	if err != nil {
		return renderFailed(err), true
	}
	if len(text) <= tools.InlineLimit {
		return text, false
	}
	if path, ok := tools.SaveTo(rt.Dir)(text); ok {
		return tools.Persisted(path, text, len(text), "result"), false
	}
	// Not Fit's fallback, which cuts at a byte: the render again, whole rows within
	// what an answer holds inline.
	text, err = render(ctx, res, tools.InlineLimit)
	if err != nil {
		return renderFailed(err), true
	}
	return text, false
}

// renderFailed is a render's error as the model reads it: column names it can shorten,
// or a context that ended, which the loop reports as a timeout.
func renderFailed(err error) string {
	if errors.Is(err, errColumnsTooLong) {
		return result(map[string]string{"error": "too-large", "message": columnsTooLong})
	}
	return readFailed
}

// refusal is a bad input as the model reads it: the field, never its value.
func refusal(err error) string {
	out := map[string]string{"error": "bad-input"}
	if ie, ok := errors.AsType[*inputError](err); ok {
		if ie.field != "" {
			out["field"] = ie.field
		}
		if ie.message != "" {
			out["message"] = ie.message
		}
	}
	return result(out)
}

// result is v as one line of JSON, leaving <, > and & as they are.
func result(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // strings, string slices and maps of them always encode
	return strings.TrimSuffix(b.String(), "\n")
}

// render is the answer within bound bytes: the cluster and its verdict, then the
// columns, then the rows last and one to a line, so a preview shows the verdict and the
// columns and Read can page a saved answer by line. It stops before a row that would
// take the answer past bound, and says rows were left out. Redacting a row can be slow,
// so it checks ctx between rows.
func render(ctx context.Context, res answer, bound int) (string, error) {
	head := `{"cluster":` + result(res.cluster) + `,"freshness":` + string(res.freshness)
	if res.withheld {
		return head + `}`, nil
	}
	head += `,"columns":` + result(res.rows.Columns)
	// The longer of the two spellings of more, so the rows' room holds either.
	room := bound - len(head+`,"more":false,"rows":[`+"\n"+`]}`)
	if room < 0 {
		return "", errColumnsTooLong
	}
	more := res.rows.More
	var rows []string
	for _, row := range res.rows.Rows {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := renderRow(row)
		if len(line)+len(",\n") > room {
			more = true
			break
		}
		room -= len(line) + len(",\n")
		rows = append(rows, line)
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(`,"more":`)
	b.WriteString(strconv.FormatBool(more))
	b.WriteString(`,"rows":[`)
	if len(rows) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(rows, ",\n"))
		b.WriteString("\n")
	}
	b.WriteString(`]}`)
	return b.String(), nil
}

// input is one call's arguments: the statement without a trailing ;, the limit
// defaulted and capped.
type input struct {
	sql, description string
	limit            int
}

// inputError is arguments the tool refuses. field names the argument when it is one
// the tool knows, "" otherwise; message, the tool's own words, says what to change.
type inputError struct{ field, message string }

func (e *inputError) Error() string {
	if e.field == "" {
		return "kubequery input is not one flat object of the tool's own fields"
	}
	return "kubequery input has a bad " + e.field
}

// parse reads an object whose keys are the tool's fields, each spelled exactly and at
// most once, with nothing after it. Each value's type is checked off its token, since
// a typed decode takes null as the zero value.
func parse(raw json.RawMessage) (input, error) {
	in := input{limit: defaultLimit}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return input{}, &inputError{}
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return input{}, &inputError{}
		}
		name, _ := key.(string)
		if seen[name] {
			return input{}, &inputError{}
		}
		seen[name] = true
		val, err := dec.Token()
		if err != nil {
			return input{}, &inputError{}
		}
		ok := false
		switch name {
		case "sql":
			in.sql, ok = val.(string)
		case "description":
			in.description, ok = val.(string)
		case "limit":
			in.limit, ok = limitOf(val)
		default:
			return input{}, &inputError{}
		}
		if !ok {
			return input{}, &inputError{field: name}
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return input{}, &inputError{}
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input{}, &inputError{}
	}

	// SQLite separates statements with ; alone, so text without one is one statement;
	// modernc would run every statement of a script and answer the last.
	in.sql = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in.sql), ";"))
	if strings.Contains(in.sql, ";") {
		return input{}, &inputError{field: "sql", message: semicolonMessage}
	}
	if in.sql == "" {
		return input{}, &inputError{field: "sql"}
	}
	return in, nil
}

// limitOf is a limit token as the rows to answer: a whole number of at least 1, read
// as maxLimit past it.
func limitOf(val json.Token) (int, bool) {
	n, ok := val.(json.Number)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(string(n))
	if err != nil || v < 1 {
		return 0, false
	}
	return min(v, maxLimit), true
}

// renderRow is one row as a JSON array on one line, each cell by its type.
func renderRow(row []any) string {
	cells := make([]string, len(row))
	for i, v := range row {
		cells[i] = renderCell(v)
	}
	return "[" + strings.Join(cells, ",") + "]"
}

// renderCell is one cell. Every text is redacted as a command's output is: a cell
// holding a JSON object or array by its structure, nested so its row stays one line,
// and any other as text.
func renderCell(v any) string {
	switch v := v.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "null"
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		if t := strings.TrimLeft(v, " \t\r\n"); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			if out, ok := safe.RedactJSON(v); ok {
				return out
			}
		}
		return result(safe.Redact(v))
	case []byte:
		// Only a query of the store's own tables reads a blob.
		return result("<blob " + strconv.Itoa(len(v)) + " bytes>")
	default:
		return "null"
	}
}

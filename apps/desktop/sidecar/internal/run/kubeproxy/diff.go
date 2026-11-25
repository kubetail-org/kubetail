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

package kubeproxy

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"sigs.k8s.io/yaml"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

const (
	// diffTimeout bounds a preview's two requests together.
	diffTimeout = 10 * time.Second
	// maxDiffLines is the most of a diff the request draws before the raw
	// request takes over.
	maxDiffLines = 2000
	// maxDiffObject is the largest answer a preview reads: the API server's own
	// request limit, which bounds an object it stores.
	maxDiffObject = 3 << 20
	// changedMark is a redacted value the write changes.
	changedMark = "[redacted: changed]"
	// noChange is the diff of a write that changes nothing.
	noChange = "No change."
)

// Why a write that asks has no preview, in the user's words.
const (
	previewUnhonored = "This API may not honor a dry run, so Kubetail does not preview it."
	previewBusy      = "Too many requests are open at once to preview this change."
	previewTooLarge  = "The object is too large to preview."
	previewHidden    = "Kubetail could not hide this object's secret values, so it does not preview it."
	previewNoRead    = "Kubetail could not read the object: "
	previewNoDryRun  = "The dry run failed: "
)

// serverAssigned are the metadata fields the API server sets on every write,
// which would make every diff noise.
var serverAssigned = []string{"managedFields", "resourceVersion", "generation"}

// errTooLarge is an answer past maxDiffObject.
var errTooLarge = errors.New("kubeproxy: an answer too large to preview")

// preview is a write's change as the user reads it: a unified diff of YAML,
// whether it stops short of the whole change, or why there is none.
type preview struct {
	diff string
	cut  bool
	err  string
}

// previewChange is the change a PUT or PATCH of a named object makes: the
// object now against what a dry run of the same request says it will be. It
// runs for a write that asks alone, on a group version that honors a dry run,
// under the write lock, in a slot and through the limiter, and both requests
// end with the endpoint, the grant, or diffTimeout. An object not there is no
// preview and no error.
func (g *Grant) previewChange(r *http.Request, p apiPath, body []byte) preview {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch || p.name == "" {
		return preview{}
	}
	if !honorsDryRun[cmp.Or(p.group, "core")+"/"+p.version] {
		return preview{err: previewUnhonored}
	}
	if !g.openRequests.TryAcquire(1) {
		return preview{err: previewBusy}
	}
	defer g.openRequests.Release(1)
	ctx, cancel := g.previewBound(r.Context())
	defer cancel()
	conn, err := g.up.Endpoint(ctx)
	if err != nil {
		return preview{err: previewNoRead + strings.TrimPrefix(unavailable(err), "kubetail: ")}
	}
	go func() {
		select {
		case <-conn.Done:
			cancel()
		case <-ctx.Done():
		}
	}()

	u := *conn.Base
	u.Path = strings.TrimSuffix(u.Path, "/") + r.URL.Path
	code, now, err := g.previewRequest(ctx, conn.Client, http.MethodGet, u.String(), "", nil)
	switch {
	case errors.Is(err, errTooLarge):
		return preview{err: previewTooLarge}
	case err != nil:
		return preview{err: previewNoRead + "the cluster did not answer"}
	case code == http.StatusNotFound:
		return preview{}
	case code/100 != 2:
		return preview{err: previewNoRead + statusMessage(code, now)}
	}
	u.RawQuery = r.URL.RawQuery
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += "dryRun=All"
	code, then, err := g.previewRequest(ctx, conn.Client, r.Method, u.String(), r.Header.Get("Content-Type"), body)
	switch {
	case errors.Is(err, errTooLarge):
		return preview{err: previewTooLarge}
	case err != nil:
		return preview{err: previewNoDryRun + "the cluster did not answer"}
	case code/100 != 2:
		return preview{err: previewNoDryRun + statusMessage(code, then)}
	}
	return diffObjects(now, then, p.onSecrets())
}

// previewRequest sends one request of a preview and answers its status and
// body, read to maxDiffObject.
func (g *Grant) previewRequest(ctx context.Context, client *http.Client, method, url, contentType string, body []byte) (int, []byte, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxDiffObject+1))
	if err != nil {
		return 0, nil, err
	}
	if len(answer) > maxDiffObject {
		return 0, nil, errTooLarge
	}
	return resp.StatusCode, answer, nil
}

// statusMessage is the message of the Status body answers with, rendered
// safe, else the code's own text.
func statusMessage(code int, body []byte) string {
	var st struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &st) == nil && st.Message != "" {
		return safe.String(st.Message)
	}
	return http.StatusText(code)
}

// diffObjects is the diff of two JSON objects as YAML, what the server
// assigns dropped and every value that must not be shown redacted.
func diffObjects(nowJSON, thenJSON []byte, secret bool) preview {
	now, err := decodeObject(nowJSON)
	if err != nil {
		return preview{err: previewNoRead + "it is not a JSON object"}
	}
	then, err := decodeObject(thenJSON)
	if err != nil {
		return preview{err: previewNoDryRun + "its answer is not a JSON object"}
	}
	for _, obj := range []map[string]any{now, then} {
		if metadata, ok := obj["metadata"].(map[string]any); ok {
			for _, field := range serverAssigned {
				delete(metadata, field)
			}
		}
	}
	if err := hideValues(now, then, secret); err != nil {
		return preview{err: previewHidden}
	}
	a, errA := yaml.Marshal(now)
	b, errB := yaml.Marshal(then)
	if errA != nil || errB != nil { // coverage:ignore both sides were decoded from JSON, which always marshals back
		return preview{err: previewNoRead + "it cannot be drawn as YAML"}
	}
	return unifiedDiff(string(a), string(b))
}

// decodeObject is a JSON object decoded with UseNumber.
func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("kubeproxy: not an object")
	}
	return obj, nil
}

// hideValues redacts both sides, comparing each hidden value first so a
// change to it reads as one: on a Secret each data and stringData value, and
// on every kind each last-applied annotation, wherever the redaction reaches
// one. What the dry run's side changes, or holds alone, reads changedMark
// there.
func hideValues(now, then map[string]any, secret bool) error {
	appliedBefore, appliedAfter := lastAppliedByPath(now), lastAppliedByPath(then)
	if secret {
		changed := map[string]map[string]bool{}
		for _, field := range []string{"data", "stringData"} {
			changed[field] = changedKeys(now, then, field)
		}
		if err := redact(now); err != nil {
			return err
		}
		if err := redact(then); err != nil {
			return err
		}
		for field, keys := range changed {
			markValues(now, field, nil)
			markValues(then, field, keys)
		}
	} else {
		redactLastApplied(now)
		redactLastApplied(then)
	}
	eachMap(then, "", func(path string, m map[string]any) {
		if _, ok := m[lastApplied]; !ok {
			return
		}
		if old, held := appliedBefore[path]; !held || !reflect.DeepEqual(old, appliedAfter[path]) {
			m[lastApplied] = changedMark
		}
	})
	return nil
}

// lastAppliedByPath is every last-applied annotation in obj, by the path of
// the map holding it, so the two sides compare location by location: a
// workload's pod template carries one beside the object's own.
func lastAppliedByPath(obj map[string]any) map[string]any {
	values := map[string]any{}
	eachMap(obj, "", func(path string, m map[string]any) {
		if v, ok := m[lastApplied]; ok {
			values[path] = v
		}
	})
	return values
}

// eachMap calls fn on every map in v, at any depth, with the path of keys and
// indices that reaches it, NUL-separated since a key can hold anything else.
func eachMap(v any, path string, fn func(path string, m map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		fn(path, v)
		for k, child := range v {
			eachMap(child, path+"\x00"+k, fn)
		}
	case []any:
		for i, child := range v {
			eachMap(child, path+"\x00"+strconv.Itoa(i), fn)
		}
	}
}

// changedKeys are the keys of then's field whose value now's differs from or
// lacks.
func changedKeys(now, then map[string]any, field string) map[string]bool {
	before, _ := now[field].(map[string]any)
	after, _ := then[field].(map[string]any)
	keys := map[string]bool{}
	for k, v := range after {
		if old, ok := before[k]; !ok || !reflect.DeepEqual(old, v) {
			keys[k] = true
		}
	}
	return keys
}

// markValues sets each value of obj's field to the mark, or to changedMark for
// a key in changed.
func markValues(obj map[string]any, field string, changed map[string]bool) {
	values, _ := obj[field].(map[string]any)
	for k := range values {
		values[k] = safe.Redacted
		if changed[k] {
			values[k] = changedMark
		}
	}
}

// unifiedDiff is a's lines against b's with three lines of context and no
// header, cut past maxDiffLines, or noChange for none.
func unifiedDiff(a, b string) preview {
	text, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(strings.TrimSuffix(a, "\n")), B: difflib.SplitLines(strings.TrimSuffix(b, "\n")), Context: 3,
	})
	if text == "" {
		return preview{diff: noChange}
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxDiffLines {
		return preview{diff: text}
	}
	more := len(lines) - maxDiffLines
	return preview{diff: strings.Join(lines[:maxDiffLines], "") + fmt.Sprintf("… %d more lines not shown\n", more), cut: true}
}

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

// Package logsinput is what the log tools share: the strict parse of a view's
// arguments, its resolve against the mirror, the words for each source, and the
// refusals the model reads.
package logsinput

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Refusal is an error as the model reads it: bad arguments by field and source, a
// source the mirror refuses by code, a refusal of the log service by its code with
// its reason through logline.ForModel, a cluster with no cache or one still
// syncing as the cluster package says it, and anything else with none of its text.
func Refusal(err error) string {
	if r, ok := refusal.As(err); ok {
		return Result(map[string]any{"error": r.Code, "message": logline.ForModel(r.Reason)})
	}
	var ie *InputError
	var rf *Refused
	switch {
	case errors.As(err, &ie):
		out := map[string]any{"error": "bad-input"}
		if ie.Field != "" {
			out["field"] = ie.Field
		}
		if ie.Source >= 0 {
			out["source"] = ie.Source
		}
		if ie.Message != "" {
			out["message"] = ie.Message
		}
		return Result(out)
	case errors.As(err, &rf):
		out := map[string]any{"error": rf.Code}
		if rf.Source >= 0 {
			out["source"] = rf.Source
		}
		if rf.Containers != nil {
			out["containers"] = rf.Containers
		}
		return Result(out)
	case errors.Is(err, cluster.ErrIdentityMismatch):
		return `{"error":"server-changed"}`
	case errors.Is(err, cluster.ErrNoCache):
		return `{"error":"no-cache"}`
	case errors.Is(err, cluster.ErrSyncing):
		return `{"error":"syncing"}`
	}
	return `{"error":"read-failed"}`
}

// Result is v as one line of JSON, leaving <, > and & as they are.
func Result(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // strings, string slices, ints and maps of them always encode
	return strings.TrimSuffix(b.String(), "\n")
}

// RefusalIn is Refusal for a call of rt's run. A cache or a connection of
// another server than the run reads ends the run, as well as refusing the call.
func RefusalIn(rt tools.Runtime, err error) string {
	if errors.Is(err, cluster.ErrIdentityMismatch) {
		rt.ServerMoved()
	}
	return Refusal(err)
}

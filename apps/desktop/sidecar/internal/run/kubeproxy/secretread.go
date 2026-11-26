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
	"log/slog"
	"mime"
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// metadataKinds are the forms of an answer that carry no Secret's data.
var metadataKinds = map[string]bool{"Table": true, "PartialObjectMetadata": true, "PartialObjectMetadataList": true}

// metadataOnly is whether r prefers an answer of metadata alone: the first
// JSON type it accepts, the one askForJSON leaves first, is a Table or
// PartialObjectMetadata, and it does not ask for the whole object. kubectl's
// get with no -o sends two Table types, then plain JSON, and --sort-by adds
// includeObject=Object. Either way the answer is redacted, so a wrong call
// shows nothing more than [redacted].
func metadataOnly(r *http.Request) bool {
	if r.URL.Query().Get("includeObject") == "Object" {
		return false
	}
	for _, t := range acceptedTypes(r.Header) {
		mediaType, params, err := mime.ParseMediaType(t)
		if err != nil || mediaType != "application/json" {
			continue
		}
		return metadataKinds[params["as"]] && params["g"] == "meta.k8s.io"
	}
	return false
}

// serveSecretRead answers a read of Secret data: unredacted once the policy
// allows it or the user approves it, redacted otherwise, with a 200 either
// way. A grant with no asker reads redacted, since no record of the read could
// be written. The decision is made under the write lock, as a write's is, so
// the user is asked one question at a time; the lock is released before the
// forward, since a watch streams for as long as it is open.
func (g *Grant) serveSecretRead(w http.ResponseWriter, r *http.Request, p apiPath, body []byte) {
	if g.asker == nil {
		g.forward(w, r, p, body, true)
		return
	}
	if !g.takeWriteLock(r.Context(), w) {
		return
	}
	allowed := g.decideSecretRead(r, p)
	if !allowed {
		g.redactedReads[p.namespace] = true
	}
	g.writeLock.Release(1)
	g.forward(w, r, p, body, !allowed)
}

// decideSecretRead is whether a Secret read may show its data: a record that
// landed, or an approval; anything else keeps it redacted. Called under the
// write lock.
func (g *Grant) decideSecretRead(r *http.Request, p apiPath) bool {
	ctx := r.Context()
	act := g.classify(r, p, nil)
	v, reason := g.policy(ctx).Authorize(act)
	req := newRequest(act, v, &Write{Method: r.Method, Path: r.URL.RequestURI(), Subresource: p.subresource})
	switch d := v.Outcome(); d {
	case permissions.Allowed:
		return g.asker.Record(ctx, req, d, reason) == nil
	case permissions.Denied:
		if err := g.asker.Record(ctx, req, d, reason); err != nil {
			slog.Warn("a refused Secret read was not recorded", "err", err)
		}
		return false
	}
	answer, err := g.ask(ctx, req)
	return err == nil && answer.Approved
}

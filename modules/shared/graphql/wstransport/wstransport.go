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

// Package wstransport provides gqlgen's WebSocket transport behind an origin
// check of the caller's choosing.
package wstransport

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/coder/websocket"
)

// Websocket is gqlgen's WebSocket transport, refusing with 403 any upgrade
// that CheckOrigin rejects. CheckOrigin is required.
type Websocket struct {
	transport.Websocket
	CheckOrigin func(r *http.Request) bool
}

var _ graphql.Transport = Websocket{}

// Do refuses the upgrade unless CheckOrigin allows it, then accepts it.
func (t Websocket) Do(w http.ResponseWriter, r *http.Request, exec graphql.GraphExecutor) {
	if !t.CheckOrigin(r) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	// CheckOrigin is the whole origin policy, so coder/websocket's own
	// same-host check is turned off.
	ws := t.Websocket
	ws.Implementation = transport.CoderWebsocketImplementation{
		AcceptOptions: websocket.AcceptOptions{InsecureSkipVerify: true},
	}
	ws.Do(w, r, exec)
}

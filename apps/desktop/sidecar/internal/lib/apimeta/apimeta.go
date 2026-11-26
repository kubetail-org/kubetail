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

// Package apimeta holds wire vocabulary no single service owns. A leaf: it imports
// nothing of ours, so any service can use it without an import cycle.
package apimeta

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// ClusterID identifies a cluster: its clusters row id in app.db, opaque text on the
// wire (the GraphQL ClusterID scalar binds to it as a string). Distinct from
// ObjectID, which names a beehive object. It lives here so chat can file a chat
// under one without importing cluster.
type ClusterID string

// ChatID identifies a chat: its chats row id in app.db, a UUIDv7 minted by
// appdb and identity alone (the GraphQL ChatID scalar binds to it as a string). It
// lives here so a tool's runtime and memory can name a chat without importing
// chat.
type ChatID string

// ObjectID is the identity of a persisted object — the beehive ObjectID of any
// kind, opaque on the wire (a decimal string) and bound to the one GraphQL
// ObjectID scalar. It lives here so a service can name another's record without
// importing it.
type ObjectID int64

// parseObjectID parses an ObjectID from its decimal-string wire form; a
// malformed value is a client error surfaced through UnmarshalGQL.
func parseObjectID(s string) (ObjectID, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid object id %q: %w", s, err)
	}
	return ObjectID(n), nil
}

// MarshalGQL writes the ObjectID to the GraphQL ObjectID scalar as a quoted
// decimal string (its wire form).
func (id ObjectID) MarshalGQL(w io.Writer) {
	io.WriteString(w, strconv.Quote(strconv.FormatInt(int64(id), 10)))
}

// UnmarshalGQL parses the GraphQL ObjectID scalar into a typed ObjectID. Accepts
// string, json.Number (JSON-variable number), and int64/int (inline literal).
func (id *ObjectID) UnmarshalGQL(v any) error {
	switch t := v.(type) {
	case string:
		n, err := parseObjectID(t)
		if err != nil {
			return err
		}
		*id = n
	case json.Number:
		n, err := parseObjectID(t.String())
		if err != nil {
			return err
		}
		*id = n
	case int64:
		*id = ObjectID(t)
	case int:
		*id = ObjectID(t)
	default:
		return fmt.Errorf("ObjectID must be a string or integer, got %T", v)
	}
	return nil
}

// CollectionWatchFrameType classifies one frame on a collection watch, mirroring a Kubernetes watch
// event. Named for the frame rather than the change because Bookmark is not a change:
// the values are what a frame can BE, and only three of the four carry an entity.
// Added/Modified/Deleted match beehive's strings, so a watch pump converts plainly.
//
// It lives here because more than one service streams collection watches and gqlgen binds
// the GraphQL enum to one Go type — a second definition would make every frame
// wrapper's `type` field a resolver.
type CollectionWatchFrameType string

const (
	CollectionWatchFrameAdded    CollectionWatchFrameType = "Added"
	CollectionWatchFrameModified CollectionWatchFrameType = "Modified"
	CollectionWatchFrameDeleted  CollectionWatchFrameType = "Deleted"
	// CollectionWatchFrameBookmark closes the on-subscribe snapshot: exactly one per stream, after
	// the last snapshot object and before the first live change. It carries no object —
	// the one case for which every frame's entity is a pointer — so a consumer must skip
	// it rather than key on it.
	// See docs/read-contracts/README.md.
	CollectionWatchFrameBookmark CollectionWatchFrameType = "Bookmark"
)

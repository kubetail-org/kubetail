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

// Package rawjson holds the type the GraphQL `JSON` scalar binds to. Its own package
// because gqlgen binds one scalar to one Go type, and more than one service serves a
// JSON field — a cached object's body, a chat message's content blocks.
package rawjson

import (
	"encoding/json"
	"fmt"
	"io"
)

// RawJSON is already-serialized JSON in a string, written verbatim by MarshalGQL. A
// string (not a map) so the enclosing record stays comparable — the collection watches diff
// frames with ==, which is what makes an in-place edit surface as Modified. Empty value
// = absent body → null.
type RawJSON string

// MarshalGQL writes the stored JSON bytes verbatim, or `null` for the absent body.
func (r RawJSON) MarshalGQL(w io.Writer) {
	if r == "" {
		io.WriteString(w, "null")
		return
	}
	io.WriteString(w, string(r))
}

// UnmarshalGQL re-serializes a decoded value back to JSON bytes (nil → absent body).
// The schema uses RawJSON as output only; this exists to satisfy gqlgen's scalar
// contract (Marshaler and Unmarshaler both required).
func (r *RawJSON) UnmarshalGQL(v any) error {
	if v == nil {
		*r = ""
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("invalid JSON scalar: %w", err)
	}
	*r = RawJSON(b)
	return nil
}

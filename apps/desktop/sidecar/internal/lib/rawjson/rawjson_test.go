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

package rawjson

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RawJSON marshals its stored bytes verbatim — the sidecar forwards a cached
// object's raw_json as-is, without re-serializing a parsed map — so field order,
// spacing, and nesting are whatever the store holds.
func TestRawJSONMarshalGQLVerbatim(t *testing.T) {
	// A non-canonical body (unsorted keys, extra spacing) must survive byte-for-byte.
	body := `{"kind":"Pod","metadata":{"name":"p","namespace":"ns"},"spec":{"a":1}}`

	var buf bytes.Buffer
	RawJSON(body).MarshalGQL(&buf)

	assert.Equal(t, body, buf.String())
}

// An empty RawJSON is the absent body — it serializes as JSON null, not "" or an
// empty object, so consumers that omitted the resolver-gated field read null.
func TestRawJSONMarshalGQLEmptyIsNull(t *testing.T) {
	var buf bytes.Buffer
	RawJSON("").MarshalGQL(&buf)

	assert.Equal(t, "null", buf.String())
}

// RawJSON is a full round-trip scalar: UnmarshalGQL re-serializes an arbitrary
// decoded value back to JSON so the type also works as an input, and the result
// re-marshals to an equivalent value.
func TestRawJSONUnmarshalGQLRoundTrip(t *testing.T) {
	var r RawJSON
	require.NoError(t, r.UnmarshalGQL(map[string]any{"name": "p", "n": float64(1)}))

	var buf bytes.Buffer
	r.MarshalGQL(&buf)
	assert.JSONEq(t, `{"name":"p","n":1}`, buf.String())
}

// A nil input is the absent value — it unmarshals to the empty RawJSON, which in
// turn marshals back to null.
func TestRawJSONUnmarshalGQLNil(t *testing.T) {
	var r RawJSON
	require.NoError(t, r.UnmarshalGQL(nil))
	assert.Equal(t, RawJSON(""), r)
}

// A value JSON cannot represent is an error rather than a silently empty body,
// which would read downstream as the absent-body case.
func TestRawJSONUnmarshalGQLRejectsUnserializableInput(t *testing.T) {
	var r RawJSON
	assert.Error(t, r.UnmarshalGQL(make(chan int)))
	assert.Equal(t, RawJSON(""), r, "a rejected value must not be stored")
}

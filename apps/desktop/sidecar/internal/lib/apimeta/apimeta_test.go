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

package apimeta

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ObjectID rides the wire as a quoted decimal string, so a client never sees an
// int64 it cannot represent (JSON numbers are float64 in every JS runtime).
func TestObjectIDMarshalsAsAQuotedDecimalString(t *testing.T) {
	var buf bytes.Buffer
	ObjectID(9007199254740993).MarshalGQL(&buf) // 2^53+1: unrepresentable as a float64
	assert.Equal(t, `"9007199254740993"`, buf.String())
}

// UnmarshalGQL accepts every form a client id arrives in: the string it was served
// as, a json.Number from a JSON variable, and the int Go decodes an inline literal
// to. All four must land on the same id.
func TestObjectIDUnmarshalsEveryWireForm(t *testing.T) {
	for _, v := range []any{"42", json.Number("42"), int64(42), 42} {
		var id ObjectID
		require.NoError(t, id.UnmarshalGQL(v), "%T", v)
		assert.Equal(t, ObjectID(42), id, "%T", v)
	}
}

// A malformed id is a client error, not a zero id: silently reading garbage as 0
// would resolve to "not found" instead of saying what was wrong.
func TestObjectIDUnmarshalRejectsMalformedInput(t *testing.T) {
	for _, v := range []any{"not-a-number", "", json.Number("nope"), true, 1.5} {
		var id ObjectID
		assert.Error(t, id.UnmarshalGQL(v), "%#v should not parse", v)
	}
}

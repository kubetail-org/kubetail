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

package memory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

func TestCheckNotePassesAWellFormedNote(t *testing.T) {
	require.NoError(t, checkNote("payments-pages", "Treat its pods as prod."))
	require.NoError(t, checkNote(strings.Repeat("a", NameMax), strings.Repeat("b", BodyMax)))
}

// Each refusal names the field it is about, so the tool can tell the model which
// one to fix.
func TestCheckNoteNamesTheBadField(t *testing.T) {
	cases := map[string]struct {
		name, body, field string
	}{
		"an empty name":       {"", "b", "name"},
		"an upper-case name":  {"Payments", "b", "name"},
		"a name with a space": {"pay ments", "b", "name"},
		"a trailing hyphen":   {"payments-", "b", "name"},
		"a double hyphen":     {"pay--ments", "b", "name"},
		"a long name":         {strings.Repeat("a", NameMax+1), "b", "name"},
		"an empty body":       {"pages", "", "body"},
		"a long body":         {"pages", strings.Repeat("b", BodyMax+1), "body"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkNote(tc.name, tc.body)
			require.ErrorIs(t, err, ErrBadInput)
			var fe *FieldError
			require.ErrorAs(t, err, &fe)
			assert.Equal(t, tc.field, fe.Field)
		})
	}
}

// A forget names a note and carries no body, so its name is checked alone.
func TestCheckNoteNameChecksTheNameAlone(t *testing.T) {
	require.NoError(t, checkNoteName("payments-pages"))
	var fe *FieldError
	require.ErrorAs(t, checkNoteName("Pay ments"), &fe)
	assert.Equal(t, "name", fe.Field)
}

// A registered key can fit the name's pattern, and the name reaches every later
// chat as the body does, so it is checked the same way.
func TestCheckNoteRefusesACredentialInTheName(t *testing.T) {
	t.Cleanup(safe.ResetSecrets)
	key := "0123456789abcdef0123456789abcdef"
	safe.AddSecret(key)

	assert.ErrorIs(t, checkNote(key, "an innocuous body"), ErrSecret)
	assert.ErrorIs(t, checkNoteName(key), ErrSecret)
	assert.ErrorIs(t, checkNote("the-key-"+key, "b"), ErrSecret)
}

func TestCheckNoteRefusesACredential(t *testing.T) {
	for _, body := range []string{"log in with:\ntoken: 0123456789abcdef", "key sk-ant-0123456789abcdefghij"} {
		assert.ErrorIs(t, checkNote("pages", body), ErrSecret)
	}
}

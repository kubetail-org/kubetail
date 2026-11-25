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

package safe_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// redactJSON is RedactJSON for input that must be JSON.
func redactJSON(t *testing.T, s string) string {
	t.Helper()
	out, ok := safe.RedactJSON(s)
	require.True(t, ok, s)
	require.True(t, json.Valid([]byte(out)), out)
	return out
}

// Over the compact text a match would take the rest of the line, which is the rest of
// the body; by structure it takes its own string alone.
func TestRedactJSONKeepsWhatFollowsAMatch(t *testing.T) {
	got := redactJSON(t, `{"spec":{"containers":[{"args":["--token=abc"]}]},"status":{"phase":"Running"}}`)
	assert.Equal(t, `{"spec":{"containers":[{"args":["--token=[redacted]"]}]},"status":{"phase":"Running"}}`, got)
}

// An escaped newline is a line again, so a rule anchored at a line's start reads it.
func TestRedactJSONReadsEscapedLines(t *testing.T) {
	got := redactJSON(t, `{"data":{"env":"USER=a\npassword=hunter2\nHOST=b"}}`)
	assert.Equal(t, `{"data":{"env":"USER=a\npassword=[redacted]\nHOST=b"}}`, got)
}

func TestRedactJSONKeepsOrderAndNumbers(t *testing.T) {
	in := `{"b":1.50,"a":[3,2e5,true,false,null,-0.0],"c":"<&>"}`
	assert.Equal(t, in, redactJSON(t, in))
}

func TestRedactJSONRedactsKeys(t *testing.T) {
	got := redactJSON(t, `{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln":"x"}`)
	assert.Equal(t, `{"[redacted]":"x"}`, got)
}

func TestRedactJSONRefusesWhatIsNotJSON(t *testing.T) {
	for _, s := range []string{`text`, `"a string"`, `12`, `{}{}`, `{} x`, `{"a":`, `[1,]`, `[1`, `{1:2}`, ``} {
		_, ok := safe.RedactJSON(s)
		assert.False(t, ok, s)
	}
}

// A member whose key names a credential loses its value, whatever the value's type.
func TestRedactJSONRedactsACredentialMember(t *testing.T) {
	got := redactJSON(t, `{"password":"x","token":3,"name":"api"}`)
	assert.Equal(t, `{"password":"[redacted]","token":"[redacted]","name":"api"}`, got)
}

// A key matches lower-cased, with - and _ removed, when it equals or ends with a name.
func TestRedactJSONMatchesAKeyBySuffixAndCase(t *testing.T) {
	got := redactJSON(t, `{"DB_PASSWORD":"a","clientSecret":"b","awsSecretAccessKey":"c","secretName":"d","tokenTTL":"e"}`)
	assert.Equal(t, `{"DB_PASSWORD":"[redacted]","clientSecret":"[redacted]","awsSecretAccessKey":"[redacted]","secretName":"d","tokenTTL":"e"}`, got)
}

// Everything under a credential key goes with it: its strings and numbers, not its keys.
func TestRedactJSONRedactsUnderACredentialKey(t *testing.T) {
	got := redactJSON(t, `{"token":{"value":"x","ttl":60},"apiKey":["a",{"k":"b"}]}`)
	assert.Equal(t, `{"token":{"value":"[redacted]","ttl":"[redacted]"},"apiKey":["[redacted]",{"k":"[redacted]"}]}`, got)
}

// A boolean or a null carries no credential.
func TestRedactJSONKeepsBooleansAndNull(t *testing.T) {
	got := redactJSON(t, `{"automountServiceAccountToken":true,"password":null}`)
	assert.Equal(t, `{"automountServiceAccountToken":true,"password":null}`, got)
}

// A Pod's env var names its credential in a member beside the value, not in the value's
// key; Helm values and Argo parameters take the same shape.
func TestRedactJSONRedactsANamedValue(t *testing.T) {
	got := redactJSON(t, `{"env":[`+
		`{"value":"hunter2","name":"DB_PASSWORD"},`+
		`{"name":"DB_HOST","value":"db.prod"},`+
		`{"name":"API_TOKEN","valueFrom":{"secretKeyRef":{"name":"api","key":"token"}}},`+
		`{"name":"GITHUB_TOKEN","value":{"nested":["x",7]}}]}`)
	assert.Equal(t, `{"env":[`+
		`{"value":"[redacted]","name":"DB_PASSWORD"},`+
		`{"name":"DB_HOST","value":"db.prod"},`+
		`{"name":"API_TOKEN","valueFrom":{"secretKeyRef":{"name":"api","key":"token"}}},`+
		`{"name":"GITHUB_TOKEN","value":{"nested":["[redacted]","[redacted]"]}}]}`, got)
}

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

package settings

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// The monitoring triage model round-trips through its row; a nil model writes
// NULL, which reads as unset, as no row does.
func TestTheMonitoringTriageModelRoundTrips(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	read := func() *MonitoringTriageModel {
		t.Helper()
		var raw any
		err := db.Read.QueryRow(SelectMonitoringTriageModel).Scan(&raw)
		if err != nil {
			require.ErrorIs(t, err, sql.ErrNoRows)
			return MonitoringTriageModelOf(nil)
		}
		return MonitoringTriageModelOf(raw)
	}
	assert.Nil(t, read(), "no row is unset")

	set := &MonitoringTriageModel{ProviderID: "anthropic", ModelID: "claude", Effort: "low"}
	args, err := MonitoringTriageModelValues(set, 1)
	require.NoError(t, err)
	exec(t, db, UpsertMonitoringTriageModel, args...)
	assert.Equal(t, set, read())

	args, err = MonitoringTriageModelValues(nil, 2)
	require.NoError(t, err)
	exec(t, db, UpsertMonitoringTriageModel, args...)
	assert.Nil(t, read(), "a NULL is unset")
}

// A model with no provider or no model id is refused; a stored value Kubetail
// cannot use reads as unset, the strictest.
func TestAMonitoringTriageModelKubetailCannotUseIsUnset(t *testing.T) {
	t.Parallel()
	for _, m := range []MonitoringTriageModel{{ModelID: "claude"}, {ProviderID: "anthropic"}} {
		_, err := MonitoringTriageModelValues(&m, 1)
		assert.ErrorAs(t, err, new(SettingRefusal), "%+v", m)
	}
	for _, raw := range []any{int64(1), `{"providerID":"anthropic"}`, `{"providerID":"a","modelID":"b","extra":1}`, `not json`} {
		assert.Nil(t, MonitoringTriageModelOf(raw), "%v", raw)
	}
}

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// OffTools reads every tool turned off for every cluster: a 0, or a value
// Kubetail cannot use. A NULL is the default, on; a cluster's row and a key
// outside the tools are not a tool's setting.
func TestOffToolsReadsTheToolsTurnedOff(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	seedCluster(t, db, "dev", "dev-eks")
	for _, args := range [][]any{ToolSettingValues("Bash", false, 1), ToolSettingValues("WebFetch", true, 2)} {
		exec(t, db, UpsertToolSetting, args...)
	}
	exec(t, db, `INSERT INTO settings (key, cluster_id, value, created_at, updated_at) VALUES
		('tools.Read.enabled', NULL, 'yes', 0, 0),
		('tools.Grep.enabled', NULL, NULL, 0, 0),
		('tools.Edit.enabled', 'dev', 0, 0, 0),
		('tools.Bash.extra', NULL, 0, 0, 0),
		('sandbox.aws.enabled', NULL, 0, 0, 0)`)

	rows, err := db.Read.Query(SelectToolSettings)
	require.NoError(t, err)
	off, err := OffTools(rows)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bash", "Read"}, off)
}

// The tools' read takes the index, inside a send's write transaction.
func TestTheToolSettingsReadUsesTheIndex(t *testing.T) {
	t.Parallel()
	assert.Contains(t, queryPlan(t, appdbtest.Open(t), SelectToolSettings), "USING INDEX settings_scope_key")
}

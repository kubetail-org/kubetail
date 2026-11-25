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

package toolsettings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

var (
	bash  = Listing{Name: "Bash", Label: "Bash", Group: GroupCommands, Description: "Run commands."}
	fetch = Listing{Name: "WebFetch", Label: "WebFetch", Group: GroupWeb, Description: "Fetch a page."}
	at    = time.UnixMilli(1000)
)

func newService(t *testing.T, db *appdb.DB) *Service {
	t.Helper()
	s, err := New(db, []Listing{bash, fetch}, func() time.Time { return at })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func list(t *testing.T, s *Service) []Setting {
	t.Helper()
	l, err := s.List(t.Context())
	require.NoError(t, err)
	return l
}

func TestEveryToolIsOnUntilTurnedOff(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []Setting{{bash, true}, {fetch, true}}, list(t, newService(t, appdbtest.Open(t))))
}

// Turning a tool back on keeps its row, stamped, so there is no delete to sync.
func TestSetWritesOneRowPerToolAndKeepsIt(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := newService(t, db)
	ctx := t.Context()
	require.NoError(t, s.Set(ctx, "Bash", false))
	require.NoError(t, s.Set(ctx, "Bash", false))
	assert.Equal(t, []Setting{{bash, false}, {fetch, true}}, list(t, s))

	require.NoError(t, s.Set(ctx, "Bash", true))
	assert.Equal(t, []Setting{{bash, true}, {fetch, true}}, list(t, s))
	var rows, enabled, updated int64
	require.NoError(t, db.Read.QueryRowContext(ctx, `SELECT count(*), max(value), max(updated_at) FROM settings`).Scan(&rows, &enabled, &updated))
	assert.Equal(t, []int64{1, 1, 1000}, []int64{rows, enabled, updated})
}

// A row naming a tool this build or machine lacks is kept and changes nothing,
// and a cluster's row is not the setting for every cluster.
func TestRowsOutsideTheListAreIgnored(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	for _, q := range []string{
		`INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES ('c1', 'kubeconfig', 'ctx', 0, 0)`,
		`INSERT INTO settings (key, cluster_id, value, created_at, updated_at) VALUES ('tools.Gone.enabled', NULL, 0, 0, 0), ('tools.WebFetch.enabled', 'c1', 0, 0, 0)`,
	} {
		_, err := db.Write.Exec(q)
		require.NoError(t, err)
	}
	assert.Equal(t, []Setting{{bash, true}, {fetch, true}}, list(t, newService(t, db)))
}

// A row whose value Kubetail cannot use turns its tool off.
func TestARowKubetailCannotUseTurnsItsToolOff(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES ('tools.Bash.enabled', 'yes', 0, 0)`)
	require.NoError(t, err)
	assert.Equal(t, []Setting{{bash, false}, {fetch, true}}, list(t, newService(t, db)))
}

func TestSetRefusesAToolOffTheList(t *testing.T) {
	t.Parallel()
	assert.ErrorIs(t, newService(t, appdbtest.Open(t)).Set(t.Context(), "Gone", false), ErrUnknownTool)
}

// An always-on tool cannot be turned off, and a stored row turning it off is
// ignored.
func TestAnAlwaysOnToolStaysOn(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`INSERT INTO settings (key, value, created_at, updated_at) VALUES ('tools.TaskStop.enabled', 0, 0, 0)`)
	require.NoError(t, err)
	stop := Listing{Name: "TaskStop", Label: "TaskStop", Group: GroupCommands, Description: "Stop a task.", AlwaysOn: true}
	s, err := New(db, []Listing{bash, stop}, time.Now)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	assert.Equal(t, []Setting{{bash, true}, {stop, true}}, list(t, s))
	assert.ErrorIs(t, s.Set(t.Context(), "TaskStop", false), ErrAlwaysOn)
	require.NoError(t, s.Set(t.Context(), "TaskStop", true))
	assert.Equal(t, []Setting{{bash, true}, {stop, true}}, list(t, s))
}

func TestWatchSettingsStateSendsTheWholeValue(t *testing.T) {
	t.Parallel()
	s := newService(t, appdbtest.Open(t))
	frames, cancel := s.WatchSettingsState()
	defer cancel()
	assert.Equal(t, []Setting{{bash, true}, {fetch, true}}, testutil.Recv(t, frames, "the current list"))
	require.NoError(t, s.Set(t.Context(), "WebFetch", false))
	assert.Equal(t, []Setting{{bash, true}, {fetch, false}}, testutil.Recv(t, frames, "the change"))
}

// A write publishes the list it made, apart from the caller's context, and a
// list sent earlier is left as it was.
func TestSetPublishesWithoutRereading(t *testing.T) {
	t.Parallel()
	s := newService(t, appdbtest.Open(t))
	frames, cancel := s.WatchSettingsState()
	defer cancel()
	before := testutil.Recv(t, frames, "the current list")

	ctx, stop := context.WithCancel(t.Context())
	require.NoError(t, s.Set(ctx, "Bash", false))
	stop()
	assert.Equal(t, []Setting{{bash, false}, {fetch, true}}, testutil.Recv(t, frames, "the change"))
	assert.Equal(t, []Setting{{bash, true}, {fetch, true}}, before)
	assert.Equal(t, []Setting{{bash, false}, {fetch, true}}, list(t, s), "the table agrees")
}

func TestAClosedServiceReportsItsErrors(t *testing.T) {
	t.Parallel()
	s, err := New(appdbtest.Open(t), []Listing{bash}, time.Now)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	_, err = s.List(t.Context())
	assert.ErrorContains(t, err, "select disabled tools")
	assert.ErrorContains(t, s.Set(t.Context(), "Bash", false), "write setting of Bash")
}

func TestNewFailsWithoutTheTable(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	_, err := db.Write.Exec(`DROP TABLE settings`)
	require.NoError(t, err)
	_, err = New(db, nil, time.Now)
	assert.ErrorContains(t, err, "prepare toolsettings statements")
}

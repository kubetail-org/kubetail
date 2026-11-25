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
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

var cloudPlatforms = []permissions.Target{permissions.AWS, permissions.GitHub, permissions.GCP, permissions.Azure}

func TestEachCloudPlatformStartsOn(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	sw, err := s.CloudPlatformSwitches(t.Context())
	require.NoError(t, err)
	for _, target := range cloudPlatforms {
		assert.True(t, sw.On(target), target)
	}
	assert.False(t, sw.On(permissions.Kubernetes), "the cluster has no switch")
}

// A write turns one switch, and leaves the others.
func TestSetCloudPlatformSwitchWritesOneSwitch(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := t.Context()
	for i, target := range cloudPlatforms {
		require.NoError(t, s.SetCloudPlatformSwitch(ctx, target, false))
		sw, err := s.CloudPlatformSwitches(ctx)
		require.NoError(t, err)
		for j, other := range cloudPlatforms {
			assert.Equal(t, j > i, sw.On(other), "%s after %s is off", other, target)
		}
	}
	require.NoError(t, s.SetCloudPlatformSwitch(ctx, permissions.GitHub, true))
	require.NoError(t, s.SetCloudPlatformSwitch(ctx, permissions.GitHub, true), "a write of the value it holds")
	sw, err := s.CloudPlatformSwitches(ctx)
	require.NoError(t, err)
	assert.Equal(t, CloudPlatformSwitches{GitHub: true}, sw)
}

func TestTheClusterHasNoCloudPlatformSwitch(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	assert.ErrorIs(t, s.SetCloudPlatformSwitch(t.Context(), permissions.Kubernetes, false), ErrNoCloudPlatformSwitch)
	sw, err := s.CloudPlatformSwitches(t.Context())
	require.NoError(t, err)
	assert.Equal(t, CloudPlatformSwitches{AWS: true, GitHub: true, GCP: true, Azure: true}, sw, "nothing was written")
}

// A read that fails is an error, never a default: each caller decides what it
// reads as.
func TestACloudPlatformSwitchReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `DROP TABLE settings`)

	_, err := s.CloudPlatformSwitches(t.Context())
	assert.Error(t, err)
	assert.Error(t, s.SetCloudPlatformSwitch(t.Context(), permissions.AWS, false))
}

// The switches' read takes their four rows through the index, so a run's
// start reads no other setting.
func TestTheSwitchesReadTheirRowsAlone(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.path', '~/bin:$PATH', 0, 0)`)
	plan := queryPlan(t, db, statements[stmtSelectCloudPlatformSwitches].Text, cloudPlatformSwitchKeys()...)
	assert.Contains(t, plan, "USING INDEX settings_scope_key")

	rows, err := db.Read.Query(statements[stmtSelectCloudPlatformSwitches].Text, cloudPlatformSwitchKeys()...)
	require.NoError(t, err)
	stored, err := scanSettings[string](rows)
	require.NoError(t, err)
	assert.Empty(t, stored, "the PATH is not a switch")
}

// A switch whose stored value Kubetail cannot use is off; the others keep
// theirs, and a NULL is the default, on.
func TestASwitchValueKubetailCannotUseIsOff(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `INSERT INTO settings (key, value, created_at, updated_at) VALUES ('sandbox.aws.enabled', 'yes', 0, 0), ('sandbox.gcp.enabled', NULL, 0, 0)`)

	sw, err := s.CloudPlatformSwitches(t.Context())
	require.NoError(t, err)
	assert.Equal(t, CloudPlatformSwitches{GitHub: true, GCP: true, Azure: true}, sw)
}

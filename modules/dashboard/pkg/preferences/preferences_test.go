// Copyright 2024-2025 Andres Morey
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

package preferences

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPreferences(t *testing.T) {
	p := DefaultPreferences()
	assert.Equal(t, CurrentVersion, p.Version)
	require.NotNil(t, p.Theme)
	assert.Equal(t, "system", *p.Theme)
	require.NotNil(t, p.Timezone)
	assert.Equal(t, "UTC", *p.Timezone)
}

func TestMerge_OverridesNonNilFields(t *testing.T) {
	base := DefaultPreferences()
	dark := "dark"
	patch := &Preferences{Theme: &dark}

	result := Merge(base, patch)
	require.NotNil(t, result.Theme)
	assert.Equal(t, "dark", *result.Theme)
}

func TestMerge_PreservesBaseWhenPatchNil(t *testing.T) {
	base := DefaultPreferences()
	patch := &Preferences{}

	result := Merge(base, patch)
	require.NotNil(t, result.Theme)
	assert.Equal(t, "system", *result.Theme)
}

func TestMerge_OverridesTimezone(t *testing.T) {
	base := DefaultPreferences()
	tz := "America/New_York"
	patch := &Preferences{Timezone: &tz}

	result := Merge(base, patch)
	require.NotNil(t, result.Timezone)
	assert.Equal(t, "America/New_York", *result.Timezone)
}

func TestMerge_PreservesTimezoneWhenPatchNil(t *testing.T) {
	base := DefaultPreferences()
	patch := &Preferences{}

	result := Merge(base, patch)
	require.NotNil(t, result.Timezone)
	assert.Equal(t, "UTC", *result.Timezone)
}

func TestMerge_SetsVersionToCurrentVersion(t *testing.T) {
	base := &Preferences{Version: 0}
	patch := &Preferences{}

	result := Merge(base, patch)
	assert.Equal(t, CurrentVersion, result.Version)
}

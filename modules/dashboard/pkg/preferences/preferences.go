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

// CurrentVersion is the schema version written to preferences files.
const CurrentVersion = 1

// Preferences represents user preferences.
type Preferences struct {
	Version  int     `json:"version"`
	Theme    *string `json:"theme,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
}

// DefaultPreferences returns preferences with default values.
func DefaultPreferences() *Preferences {
	theme := "system"
	timezone := "UTC"
	return &Preferences{
		Version:  CurrentVersion,
		Theme:    &theme,
		Timezone: &timezone,
	}
}

// Merge returns a new Preferences with non-nil fields from patch
// overwriting corresponding fields in base. The result always has
// Version set to CurrentVersion.
func Merge(base, patch *Preferences) *Preferences {
	result := &Preferences{
		Version:  CurrentVersion,
		Theme:    base.Theme,
		Timezone: base.Timezone,
	}
	if patch.Theme != nil {
		result.Theme = patch.Theme
	}
	if patch.Timezone != nil {
		result.Timezone = patch.Timezone
	}
	return result
}

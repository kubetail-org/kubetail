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
	"slices"
	"strings"
)

// A tool's setting is the key tools.<name>.enabled, on until the user turns it
// off. Off is the strictest: a value Kubetail cannot use takes the tool out of
// a turn's offer. services/toolsettings writes it and agent/chat reads it,
// both through what this file exports, so neither spells a row apart.

const (
	toolKeyPrefix = "tools."
	toolKeySuffix = ".enabled"
)

func toolEntry(name string) Entry[bool] {
	return Entry[bool]{Key: toolKeyPrefix + name + toolKeySuffix, Default: true, Strictest: new(false)}
}

// SelectToolSettings reads every tool's setting for every cluster, through
// the index; OffTools reads its rows.
const SelectToolSettings = `SELECT key, value FROM settings
	WHERE ifnull(cluster_id, '') = '' AND key >= 'tools.' AND key < 'tools/'`

// UpsertToolSetting writes one tool's setting for every cluster; its
// arguments are ToolSettingValues.
const UpsertToolSetting = upsertSettingQuery

// ToolSettingValues is UpsertToolSetting's arguments for the tool named name.
func ToolSettingValues(name string, enabled bool, at int64) []any {
	e := toolEntry(name)
	raw, _ := e.encode(enabled) // a tool's entry has no check
	return []any{e.Key, "", raw, at}
}

// OffTools is the names of the tools SelectToolSettings' rows turn off,
// sorted. It closes rows.
func OffTools(rows *sql.Rows) ([]string, error) {
	stored, err := scanSettings[string](rows)
	if err != nil { // coverage:ignore scanSettings fails only past a row that read
		return nil, err
	}
	var off []string
	for key := range stored {
		name, ok := strings.CutSuffix(strings.TrimPrefix(key, toolKeyPrefix), toolKeySuffix)
		if ok && !toolEntry(name).valueIn(stored) {
			off = append(off, name)
		}
	}
	slices.Sort(off)
	return off, nil
}

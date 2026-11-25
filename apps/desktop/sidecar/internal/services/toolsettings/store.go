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
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// disabled is the names of the tools turned off for every cluster, sorted: a
// row whose value Kubetail cannot use turns its tool off.
func disabled(ctx context.Context, st stmts) ([]string, error) {
	rows, err := st.Query(ctx, stmtSelectToolSettings)
	if err != nil {
		return nil, fmt.Errorf("select disabled tools: %w", err)
	}
	return settings.OffTools(rows)
}

// upsert writes the setting of the tool named name for every cluster.
func upsert(ctx context.Context, st stmts, name string, enabled bool, at int64) error {
	if _, err := st.Exec(ctx, stmtUpsert, settings.ToolSettingValues(name, enabled, at)...); err != nil {
		return fmt.Errorf("write setting of %s: %w", name, err)
	}
	return nil
}

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

// Every statement toolsettings issues, named once and prepared once on the
// app's DB (sqlstmt.Set).
package toolsettings

import (
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// stmtID indexes statements.
type stmtID int

const (
	stmtSelectToolSettings stmtID = iota
	stmtUpsert
)

var statements = []sqlstmt.Statement{
	stmtSelectToolSettings: sqlstmt.OnReader(settings.SelectToolSettings),
	stmtUpsert:             sqlstmt.OnWriter(settings.UpsertToolSetting),
}

type stmts = sqlstmt.Stmts[stmtID]

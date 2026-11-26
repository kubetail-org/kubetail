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

package logstore

import "embed"

// The log file's schema. A change to a table's form is the next numbered migration; a
// shipped one is never edited or removed
// (docs/adr/2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md).
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

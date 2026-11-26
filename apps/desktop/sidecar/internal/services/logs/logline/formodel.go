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

package logline

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"

// MaxLine is the longest message the sidecar hands out, in bytes: maxMessage and
// the ellipsis that ends a cut one.
const MaxLine = MaxMessage + len("…")

// ForModel is a log line as the model may read it: every control stripped, its
// colours among them, then redacted, as a Bash result is. The file and the viewer
// keep the line unredacted; this runs at read time, on every line a model reads.
func ForModel(s string) string {
	return safe.Redact(StripControl(s))
}

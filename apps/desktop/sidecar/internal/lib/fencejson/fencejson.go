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

// Package fencejson marshals a value for a markdown fence: json.Marshal plus
// the one escape it lacks, a backtick as \u0060, so a value cannot spell the
// fence that closes it. Marshal already escapes angle brackets and newlines,
// and the value reads back unchanged.
package fencejson

import (
	"bytes"
	"encoding/json"
)

// Marshal is json.Marshal with every backtick escaped.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(raw, []byte("`"), []byte("\\u0060")), nil
}

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

package fencejson

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalEscapesEveryBacktick(t *testing.T) {
	raw, err := Marshal(map[string]string{"k": "a`b`"})
	require.NoError(t, err)
	assert.Equal(t, `{"k":"a\u0060b\u0060"}`, string(raw))
	var back map[string]string
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, "a`b`", back["k"])
}

func TestMarshalReportsWhatJSONRefuses(t *testing.T) {
	_, err := Marshal(make(chan int))
	assert.Error(t, err)
}

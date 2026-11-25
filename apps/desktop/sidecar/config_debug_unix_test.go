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

//go:build debug && !windows

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A debug build's overrides move the data and cache directories, so a `..`
// in one is refused as the runtime directory's is.
func TestConfigFromArgsRefusesAnOverrideHoldingDotDot(t *testing.T) {
	for variable, name := range map[string]string{
		"KUBETAIL_DATA_DIR":  "the data directory",
		"KUBETAIL_CACHE_DIR": "the cache directory",
	} {
		_, err := configFromArgs(nil, env{variable: "/home/x/link/../kubetail"}.get)
		require.ErrorContains(t, err, name, variable)
	}
}

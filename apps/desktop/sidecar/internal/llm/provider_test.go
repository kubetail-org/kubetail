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

package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthHeaderFollowsTheRowThenTheDialect(t *testing.T) {
	for _, tc := range []struct {
		name         string
		p            Provider
		header, pref string
	}{
		{"the provider's header", Provider{KeyHeader: "Api-Key", Dialect: DialectMessages}, "Api-Key", ""},
		{"the Messages API's", Provider{Dialect: DialectMessages}, "X-Api-Key", ""},
		{"a bearer", Provider{Dialect: DialectChatCompletions}, "Authorization", "Bearer "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header, prefix := tc.p.AuthHeader()
			assert.Equal(t, tc.header, header)
			assert.Equal(t, tc.pref, prefix)
		})
	}
}

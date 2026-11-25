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

package anthropicwebsearch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A search shows its query. A key the provider adds beside it is the
// provider's and changes nothing, and a call the cap cut off shows an empty one.
func TestActionOfReadsTheQuery(t *testing.T) {
	for input, want := range map[string]string{
		`{"query":"kubernetes 1.36"}`:                  "kubernetes 1.36",
		`{"query":"kubernetes 1.36","locale":"en-US"}`: "kubernetes 1.36",
		`{}`: "",
	} {
		got, err := ActionOf(json.RawMessage(input), "")

		require.NoError(t, err, input)
		assert.Equal(t, tools.Action{Search: &tools.SearchAction{Query: want}}, got, input)
	}
}

// Arguments that are not an object, or a query that is not a string, show
// nothing.
func TestActionOfRefusesWhatIsNotASearch(t *testing.T) {
	for _, input := range []string{`"kubernetes"`, `[]`, `{"query":7}`, `{"query":null}`, `{`} {
		_, err := ActionOf(json.RawMessage(input), "")

		assert.Error(t, err, input)
	}
}

// Stored search rows read as they always have.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	got, err := ActionOf(json.RawMessage(`{"query":"kubernetes 1.36 release notes"}`), "")

	require.NoError(t, err)
	assert.Equal(t, tools.Action{Search: &tools.SearchAction{Query: "kubernetes 1.36 release notes"}}, got)
}

// The search is named for its vendor and contract, records the Messages API's
// own type as its contract, is capped at five per turn, and is a search
// whatever its arguments hold.
func TestTheSearchIsATool(t *testing.T) {
	search := New(time.Now)

	assert.Equal(t, tools.ContractName("web_search_20260318"), search.ContractName())
	assert.Equal(t, ContractName, search.ContractName())
	assert.Equal(t, "anthropic_web_search_20260318", search.Name())
	assert.Equal(t, Name, search.Name())
	assert.Equal(t, tools.ActionSearch, search.ActionKind())
	assert.Equal(t, 5, search.MaxUses())
	assert.Equal(t, 10_000, search.Allowance(), "five searches at 2,000 tokens each")
	got, err := search.Action(json.RawMessage(`{"query":"q"}`), "", false)
	require.NoError(t, err)
	assert.Equal(t, tools.ActionSearch, got.Kind())
}

// The prompt sits under "What you can do" and names the month the clock reads
// at each call, not the month the tool was built in.
func TestThePromptNamesTheMonthOfEachCall(t *testing.T) {
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	search := New(func() time.Time { return now })

	first := search.Prompt()
	assert.True(t, strings.HasPrefix(first, "## Searching the web\n"), first)
	assert.Contains(t, first, "The current month is September 2026;")

	now = now.AddDate(0, 1, 0)
	assert.Contains(t, search.Prompt(), "The current month is October 2026;")
}

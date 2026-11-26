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

//go:build livecache

package catalog

import (
	"crypto/rand"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// unchecked is the models the check skips, and why.
var unchecked = map[string]string{
	"openrouter/openrouter/auto": "OpenRouter picks the model per request, so two requests can land on two caches",
}

// secondTries is how many times the second request is sent before the check
// gives up: DeepSeek writes its cache to disk behind the reply, so a request
// right after the first can miss.
const secondTries = 3

// cachedPrefix is the system prompt every request opens with: a line minted per
// run, so the first request writes whatever an earlier run left cached, then
// about 6,000 tokens of fixed text, over every documented minimum.
func cachedPrefix() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s. You answer in one word.\n\n", rand.Text())
	for i := range 500 {
		fmt.Fprintf(&b, "Note %d: a pod is one or more containers scheduled together on a node.\n", i)
	}
	return b.String()
}

// Every keyed model that caches reads its second turn from the cache. The keys
// come from the environment; a provider with none set is not checked.
func TestEveryModelReadsItsSecondTurnFromTheCache(t *testing.T) {
	svc := llm.New(New(Config{Credentials: newKeys(t, liveKeys(t), nil)}).Providers()...)
	system := cachedPrefix()
	msgs := []llm.Message{{Role: "user", Blocks: []llm.Block{llm.TextBlock("Are you ready?")}}}

	for _, p := range testutil.Current(t, svc.WatchProvidersState, "the providers") {
		for _, m := range p.Catalog {
			name := p.ID + "/" + m.ID
			reason, skip := unchecked[name]
			if m.Cache == llm.CacheNone {
				reason, skip = "its provider documents no cache", true
			}
			if skip {
				t.Logf("%s: skipped: %s", name, reason)
				continue
			}
			t.Run(name, func(t *testing.T) {
				target, err := svc.Resolve(p.ID, m.ID, m.DefaultEffort)
				require.NoError(t, err)
				key := rand.Text()
				ask := func() llm.Usage {
					resp, err := target.Stream(t.Context(), system, msgs, key, nil, nil, func(llm.Chunk) {})
					require.NoError(t, err)
					require.True(t, resp.Usage.Reported, "no usage reported, so no check can see the cache")
					return resp.Usage
				}

				first := ask()
				var last llm.Usage
				for range secondTries {
					if last = ask(); last.CacheReadTokens > 0 {
						break
					}
				}
				fmt.Printf("%s\t%s\t%s\tfirst read %d\tlast read %d\n",
					p.ID, m.ID, m.Cache, first.CacheReadTokens, last.CacheReadTokens)
				require.Positive(t, last.CacheReadTokens, "the second turn read nothing from the cache")
			})
		}
	}
}

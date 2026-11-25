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

package logsread

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput/logsinputtest"
)

// The widest page prints both its tokens whole, next reading on, and its header
// leaves the lines their whole budget under tools.InlineLimit. The page is a
// realistic worst case: 100 pods named as a Deployment names them, a 30-character
// namespace, 64-hex runtime ids, a page in the middle of the window, so prev and
// next both carry a line per stream, its messages filling max_page_bytes. Each
// line's stamp and stream take it past the limit, and the result is saved to a
// file, as every tool's is.
func TestTheWidestPagePrintsItsTokensWhole(t *testing.T) {
	t.Parallel()
	ns, deployment := strings.Repeat("n", 30), strings.Repeat("d", 30)
	var objects []map[string]any
	var sources, pods []string
	for i := range 100 {
		sum := sha256.Sum256(fmt.Append(nil, i))
		id := hex.EncodeToString(sum[:])
		pod := deployment + "-" + id[:10] + "-" + id[10:15]
		o := logsinputtest.PodOf(pod, "", nil, logsinputtest.Container{Name: "app"})
		o["metadata"].(map[string]any)["namespace"] = ns
		o["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)["containerID"] = "containerd://" + id
		objects, pods = append(objects, o), append(pods, pod)
		sources = append(sources, fmt.Sprintf(`{"namespace":%q,"resource":"pods/%s"}`, ns, pod))
	}
	l := logsinputtest.LogsOf(t, objects...)
	msg := strings.Repeat("x", logsinput.SampleBudget/100-1)
	for i, pod := range pods {
		for s := range 3 {
			l.Server.Add(ns+"/"+pod+"/app", logstest.Stamped(time.Duration(s)*time.Second+time.Duration(i)*time.Millisecond, msg))
		}
	}
	text, _ := readOf(t, l, `{"sources":[`+strings.Join(sources, ",")+`],"page_size":100,"page_from":"first"}`)
	_, next, _ := pageOf(t, text)

	text, read := readOf(t, l, `{"cursor":"`+next+`","page_size":100}`)

	require.Len(t, read.Lines, 100)
	prev, next, _ := pageOf(t, text)
	require.NotEmpty(t, prev)
	require.NotEmpty(t, next)
	header, _, _ := strings.Cut(text, "\n2026-")
	assert.LessOrEqual(t, len(header)+logsinput.SampleBudget, tools.InlineLimit, "header %d bytes", len(header))
	after, _ := readOf(t, l, `{"cursor":"`+next+`","page_size":100}`)
	assert.Len(t, messagesOf(after), 100)
}

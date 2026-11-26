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

package githubproxy

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// pkt is s as one pkt-line.
func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

const (
	zero = "0000000000000000000000000000000000000000"
	idA  = "1111111111111111111111111111111111111111"
	idB  = "2222222222222222222222222222222222222222"
	pack = "PACK\x00\x00\x00\x02 the commits"
)

// pushBody is a push's body: shallow lines, then each command, the first
// carrying caps, a flush, the options and a flush when caps names
// push-options, then the pack.
func pushBody(shallow []string, caps string, commands []string, options []string) string {
	var b strings.Builder
	for _, s := range shallow {
		b.WriteString(pkt("shallow " + s + "\n"))
	}
	for i, c := range commands {
		if i == 0 {
			c += "\x00" + caps
		}
		b.WriteString(pkt(c + "\n"))
	}
	b.WriteString("0000")
	if strings.Contains(caps, "push-options") {
		for _, o := range options {
			b.WriteString(pkt(o + "\n"))
		}
		b.WriteString("0000")
	}
	b.WriteString(pack)
	return b.String()
}

// The opening names each ref and option, and with the bytes left unread it
// is the body whole, so the pack reaches GitHub as it was sent.
func TestAPushNamesItsRefs(t *testing.T) {
	t.Parallel()
	body := pushBody([]string{idA}, "report-status side-band-64k push-options agent=git/2.47", []string{
		zero + " " + idA + " refs/heads/new",
		idA + " " + idB + " refs/heads/main",
		idB + " " + zero + " refs/tags/old",
	}, []string{"ci.skip", "merge_request.create"})

	r := strings.NewReader(body)
	opening, p, err := readPush(r)
	require.NoError(t, err)
	assert.False(t, p.probe)
	assert.True(t, p.deletes())
	assert.Equal(t, []runproxy.Detail{
		{Label: "Repository", Value: "o/r"},
		{Label: "Creates", Value: "refs/heads/new " + zero + " → " + idA},
		{Label: "Updates", Value: "refs/heads/main " + idA + " → " + idB},
		{Label: "Deletes", Value: "refs/tags/old " + idB + " → " + zero},
		{Label: "Push option", Value: "ci.skip"},
		{Label: "Push option", Value: "merge_request.create"},
	}, p.details("o/r"))
	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, body, string(opening)+string(rest))
}

func TestAPushProbeIsOneFlush(t *testing.T) {
	t.Parallel()
	_, p, err := readPush(strings.NewReader("0000"))
	require.NoError(t, err)
	assert.True(t, p.probe)
	_, _, err = readPush(strings.NewReader("0000" + pack))
	assert.ErrorIs(t, err, errPushUnread, "a flush and more is no probe")
}

func TestAPushThatDoesNotReadIsRefused(t *testing.T) {
	t.Parallel()
	update := idA + " " + idB + " refs/heads/main"
	for name, body := range map[string]string{
		"empty":                ``,
		"a short length":       `00`,
		"not hex":              `zzzz`,
		"a length under four":  `0003`,
		"a delimiter":          `0001`,
		"cut short":            pkt(update + "\x00report-status\n")[:20],
		"no flush":             pkt(update + "\x00report-status\n"),
		"a push certificate":   pkt("push-cert\x00report-status\n") + pkt("certificate version 0.1\n"),
		"a short id":           pushBody(nil, "report-status", []string{"1111 " + idB + " refs/heads/main"}, nil),
		"an id not hex":        pushBody(nil, "report-status", []string{strings.Repeat("g", 40) + " " + idB + " refs/heads/main"}, nil),
		"ids of two lengths":   pushBody(nil, "report-status", []string{idA + " " + idB + "22 refs/heads/main"}, nil),
		"no ref":               pushBody(nil, "report-status", []string{idA + " " + idB}, nil),
		"a control character":  pushBody(nil, "report-status", []string{idA + " " + idB + " refs/heads/\x1bmain"}, nil),
		"invalid UTF-8":        pushBody(nil, "report-status", []string{idA + " " + idB + " refs/heads/\xffmain"}, nil),
		"an option not text":   pushBody(nil, "report-status push-options", []string{update}, []string{"a\x07b"}),
		"options never closed": pushBody(nil, "report-status push-options", []string{update}, nil)[:len(pkt(update+"\x00report-status push-options\n"))+4],
		"only a shallow line":  pkt("shallow " + idA + "\n"),
		"a bad shallow line":   pkt("shallow nope\n") + pkt(update+"\x00report-status\n") + "0000",
	} {
		_, _, err := readPush(strings.NewReader(body))
		assert.ErrorIs(t, err, errPushUnread, name)
	}
}

// An opening past 64 KiB is refused with its own error, which tells the
// model to push fewer refs at a time.
func TestAPushPastItsOpeningIsTooLarge(t *testing.T) {
	t.Parallel()
	var commands []string
	for i := range 700 {
		commands = append(commands, fmt.Sprintf("%s %s refs/tags/release-%04d-with-a-long-name", zero, idA, i))
	}
	_, _, err := readPush(strings.NewReader(pushBody(nil, "report-status", commands, nil)))
	assert.ErrorIs(t, err, errPushTooLarge)
}

// SHA-256 repositories name commits in 64 hex digits.
func TestAPushReadsLongIDs(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 64)
	_, p, err := readPush(strings.NewReader(pushBody(nil, "report-status object-format=sha256",
		[]string{strings.Repeat("0", 64) + " " + long + " refs/heads/main"}, nil)))
	require.NoError(t, err)
	assert.Equal(t, "Creates", p.details("o/r")[1].Label)
}

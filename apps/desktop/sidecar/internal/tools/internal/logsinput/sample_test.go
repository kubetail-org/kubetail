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

package logsinput

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A line is its timestamp, its stream and its message; the instance before a
// restart says so.
func TestALineIsItsStampStreamAndMessage(t *testing.T) {
	t.Parallel()
	rec := record(20*time.Second+4*time.Millisecond, 0, "panic: nil map")
	assert.Equal(t, "2026-10-17T09:14:20.004000000Z prod/webapp-a/app panic: nil map", LineText(rec, ""))

	rec.Stream.Previous = true
	assert.Equal(t, "2026-10-17T09:14:20.004000000Z prod/webapp-a/app (previous) panic: nil map", LineText(rec, ""))
}

// A stamp is UTC with nine fraction digits always, so a timestamp copied into
// since and until names one nanosecond.
func TestAStampHasNineDigits(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 17, 11, 14, 20, 0, time.FixedZone("", 2*60*60))
	assert.Equal(t, "2026-10-17T09:14:20.000000000Z", Stamp(at))
	assert.Equal(t, "2026-10-17T09:14:20.000000007Z", Stamp(at.Add(7)))
}

// A hole before a line is a line of its own ahead of it, starting with [.
func TestAHoleIsALineBeforeIt(t *testing.T) {
	t.Parallel()
	rec := record(time.Minute, 0, "back")
	rec.Gap = &logline.Gap{After: rec.Timestamp.Add(-time.Minute), Before: rec.Timestamp}
	assert.Equal(t, "[lines may be missing from prod/webapp-a/app between 2026-10-17T09:14:00.000000000Z and 2026-10-17T09:15:00.000000000Z]\n"+
		"2026-10-17T09:15:00.000000000Z prod/webapp-a/app back", LineText(rec, ""))
}

// A line outside the range carries its place before its message, and a line the
// service cut says how many bytes it lost.
func TestALineSaysItsPlaceAndItsCut(t *testing.T) {
	t.Parallel()
	rec := record(time.Second, 0, "panic: nil")
	assert.Equal(t, "2026-10-17T09:14:01.000000000Z prod/webapp-a/app [before] panic: nil", LineText(rec, PlaceBefore))
	assert.Equal(t, "2026-10-17T09:14:01.000000000Z prod/webapp-a/app [after] panic: nil", LineText(rec, PlaceAfter))

	rec.TruncatedBytes = 1234
	assert.Equal(t, "2026-10-17T09:14:01.000000000Z prod/webapp-a/app panic: nil … [1234 bytes cut]", LineText(rec, ""))
}

// A record's place is its timestamp against the echoed bounds, which include their
// moment. With until open and after set, the service ends the range the nanosecond
// before ResolvedAt, so a line at or past it is padding whatever until says.
func TestARecordsPlaceIsItsStampAgainstTheBounds(t *testing.T) {
	t.Parallel()
	at := func(d time.Duration) time.Time { return time.Date(2026, 10, 17, 9, 14, 0, 0, time.UTC).Add(d) }
	since, until := at(time.Second), at(3*time.Second)
	bounded := logs.FetchResponse{Request: logs.FetchRequest{Window: logs.Window{Since: &since, Until: &until}}}
	for d, want := range map[time.Duration]string{0: PlaceBefore, time.Second: "", 3 * time.Second: "", 3*time.Second + 1: PlaceAfter} {
		assert.Equal(t, want, PlaceOf(record(d, 0, "m"), bounded), d)
	}

	open := logs.FetchResponse{ResolvedAt: at(5 * time.Second), Request: logs.FetchRequest{Window: logs.Window{Since: &since, After: logs.Padding{Lines: 3}}}}
	for d, want := range map[time.Duration]string{5*time.Second - 1: "", 5 * time.Second: PlaceAfter, 6 * time.Second: PlaceAfter} {
		assert.Equal(t, want, PlaceOf(record(d, 0, "m"), open), d)
	}
	open.Request.After = logs.Padding{}
	assert.Equal(t, "", PlaceOf(record(6*time.Second, 0, "m"), open), "no after, no padding")
}

// A record is the action's line field for field, and the view around a line is
// the view at its moment, unpinned.
func TestARecordIsTheActionsLine(t *testing.T) {
	t.Parallel()
	rec := record(time.Second, 3, "a")
	rec.Stream.Previous = true
	rec.Gap = &logline.Gap{After: rec.Timestamp.Add(-time.Second), Before: rec.Timestamp}
	rec.TruncatedBytes = 5
	assert.Equal(t, tools.LogsReadLine{
		Timestamp: rec.Timestamp, Namespace: "prod", Pod: "webapp-a", Container: "app", Previous: true, Message: "a", Gap: rec.Gap,
		TruncatedBytes: 5,
	}, ReadLine(rec))

	view := tools.LogsScopeInput{Grep: "a"}
	resolved := rec.Timestamp.Add(time.Hour)
	around := ViewAround(view, rec.Timestamp, resolved)
	assert.Equal(t, tools.LogsViewAction{
		LogsScopeInput: view, Anchor: tools.LogsViewAnchor{Kind: tools.LogsAnchorAt, At: &rec.Timestamp}, ResolvedAt: resolved,
	}, *around)
}

// record is a line of prod/webapp-a/app at offset past 09:14, the ordinal-th of its
// moment, saying msg.
func record(offset time.Duration, ordinal int, msg string) logline.Record {
	return logline.Record{
		Timestamp: time.Date(2026, 10, 17, 9, 14, 0, 0, time.UTC).Add(offset), Message: msg,
		Stream:      logline.StreamKey{Namespace: "prod", Pod: "webapp-a", Container: "app"},
		ContainerID: "containerd://3f9a", Ordinal: ordinal,
	}
}

// A notice is a line of its own, its message stripped and redacted, while the
// notices fit NoticesBudget; the rest are counted.
func TestNoticesStopAtTheirBudget(t *testing.T) {
	t.Parallel()
	var ns Notices
	ns.Add(logs.Notice{Code: "stream-skipped", Message: "prod/a/app: \x1b[31mrefused\x1b[0m"})
	assert.Equal(t, []string{"notice: stream-skipped: prod/a/app: refused"}, ns.Lines())

	ns = Notices{}
	for range 100 {
		ns.Add(logs.Notice{Code: "stream-skipped", Message: strings.Repeat("n", 100)})
	}
	lines := ns.Lines()
	kept := len(lines) - 1
	assert.LessOrEqual(t, len(strings.Join(lines[:kept], "\n")), NoticesBudget)
	assert.Equal(t, fmt.Sprintf("notice: %d more notices left out", 100-kept), lines[kept])
}

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

import (
	"encoding/base64"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// A line cursor names its line back, and a moment's names the moment, with or
// without its container.
func TestALineCursorRoundTrips(t *testing.T) {
	t.Parallel()
	line := lineAt(StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, 1500*time.Millisecond, 3)
	moment := MomentAt(at(1500 * time.Millisecond))
	own := Cursor{ContainerName: line.ContainerName, RunPlace: RunPlace{Timestamp: line.Timestamp}}
	for _, c := range []Cursor{line, moment, own} {
		back, err := Decode(Encode(c))
		require.NoError(t, err)
		assert.Equal(t, c, back)
	}
	assert.True(t, moment.IsMoment())
	assert.True(t, own.IsMoment())
	assert.False(t, line.IsMoment())
}

// A line cursor is versioned JSON, base64url with no padding, its keys in one order.
func TestALineCursorIsVersionedJSON(t *testing.T) {
	t.Parallel()
	c := lineAt(StreamKey{Namespace: "prod", Pod: "web-1", Container: "app"}, time.Second, 2)

	raw, err := base64.RawURLEncoding.DecodeString(Encode(c))

	require.NoError(t, err)
	assert.JSONEq(t, `{"v":1,"ns":"prod","pod":"web-1","c":"app","id":"containerd://uid-web-1-app-0","t":"2026-10-08T10:00:01Z","n":2}`, string(raw))
	assert.True(t, strings.HasPrefix(string(raw), `{"v":1,"ns":"prod","pod":"web-1","c":"app","id":`), string(raw))
	moment, err := base64.RawURLEncoding.DecodeString(Encode(MomentAt(at(0))))
	require.NoError(t, err)
	assert.Equal(t, `{"v":1,"t":"2026-10-08T10:00:00Z"}`, string(moment))
}

// One line has one cursor string.
func TestALineHasOneCursor(t *testing.T) {
	t.Parallel()
	rec := nth("web-1", "app", time.Second, 2, "hello")

	assert.Equal(t, rec.Cursor(), rec.Cursor())
	back, err := Decode(rec.Cursor())
	require.NoError(t, err)
	assert.Equal(t, rec.Cursor(), Encode(back))
}

// A record's cursor names its line.
func TestARecordsCursorNamesItsLine(t *testing.T) {
	t.Parallel()
	rec := nth("web-1", "app", time.Second, 2, "hello")

	p, err := Decode(rec.Cursor())

	require.NoError(t, err)
	assert.Equal(t, CursorOf(rec), p)
}

// A line keeps its cursor once its run is the previous one: the id names the
// run, not whether it is the last.
func TestALineKeepsItsCursorAcrossARestart(t *testing.T) {
	t.Parallel()
	rec := line("web-1", "app", time.Second, "")
	prev := rec
	prev.Stream.Previous = true

	assert.Equal(t, rec.Cursor(), prev.Cursor())
}

// A line cursor that is not one the service wrote is refused as cursor-invalid.
func TestABadLineCursorIsRefused(t *testing.T) {
	t.Parallel()
	enc := func(j string) string { return base64.RawURLEncoding.EncodeToString([]byte(j)) }
	line := func(fields string) string {
		return enc(`{"v":1,"ns":"prod","pod":"web","c":"app",` + fields + `}`)
	}
	const ok = `"t":"2026-10-08T10:00:01Z"`
	for name, c := range map[string]string{
		"not base64url":     "a+b/",
		"not JSON":          enc("yesterday"),
		"another version":   enc(`{"v":2,"t":"2026-10-08T10:00:01Z"}`),
		"no version":        enc(`{"t":"2026-10-08T10:00:01Z"}`),
		"an upper-case pod": enc(`{"v":1,"ns":"prod","pod":"Web","c":"app","id":"c://a",` + ok + `,"n":0}`),
		"a long name":       enc(`{"v":1,"ns":"` + strings.Repeat("a", 254) + `","pod":"web","c":"app","id":"c://a",` + ok + `,"n":0}`),
		"an empty id":       line(`"id":"",` + ok + `,"n":0`),
		"a control in id":   line(`"id":"c://a\nb",` + ok + `,"n":0`),
		"a long id":         line(`"id":"c://` + strings.Repeat("a", 254) + `",` + ok + `,"n":0`),
		"no time":           line(`"id":"c://a","n":0`),
		"a zero time":       line(`"id":"c://a","t":"0001-01-01T00:00:00Z","n":0`),
		"a negative":        line(`"id":"c://a",` + ok + `,"n":-1`),
		"the last ordinal":  line(`"id":"c://a",` + ok + `,"n":` + strconv.Itoa(math.MaxInt)),
		"names in part":     enc(`{"v":1,"ns":"prod","pod":"web",` + ok + `}`),
		"id and not n":      line(`"id":"c://a",` + ok),
		"n and not id":      line(ok + `,"n":0`),
		"id with no names":  enc(`{"v":1,"id":"c://a",` + ok + `,"n":0}`),
	} {
		_, err := Decode(c)
		var r refusal.Refusal
		if assert.ErrorAs(t, err, &r, name) {
			assert.Equal(t, refusal.CodeCursorInvalid, r.Code, name)
		}
	}
}

// A cursor of another version says to read again.
func TestALineCursorOfAnotherVersionSaysToReadAgain(t *testing.T) {
	t.Parallel()
	_, err := Decode(base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"t":"2026-10-08T10:00:01Z"}`)))

	assert.ErrorContains(t, err, "read again")
}

// A cursor takes whatever id a runtime writes, up to 256 bytes: the server handed
// it out.
func TestACursorTakesARuntimesID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"containerd://uid-web-1-app-0", "docker://3f4e", "containerd://" + strings.Repeat("a", 243)} {
		c := lineAt(StreamKey{Namespace: "prod", Pod: "web", Container: "app"}, time.Second, 0)
		c.ContainerID = id
		p, err := Decode(Encode(c))
		require.NoError(t, err, id)
		assert.Equal(t, id, p.ContainerID)
	}
}

// Lines order by time, then namespace, pod and container, then ordinal, and the
// instance id decides only an equal ordinal; against a moment, by time alone.
func TestPositionsOrder(t *testing.T) {
	t.Parallel()
	a := StreamKey{Namespace: "prod", Pod: "a", Container: "app"}
	sidecar := StreamKey{Namespace: "prod", Pod: "a", Container: "sidecar"}
	older, newer := lineAt(a, 0, 0), lineAt(a, 0, 0)
	older.ContainerID, newer.ContainerID = "containerd://a-0", "containerd://a-1"
	assert.Less(t, CompareCursors(lineAt(a, 0, 5), lineAt(a, time.Second, 0)), 0)
	assert.Less(t, CompareCursors(lineAt(a, 0, 0), lineAt(a, 0, 1)), 0)
	assert.Less(t, CompareCursors(older, newer), 0)
	assert.Greater(t, CompareCursors(newer, older), 0)
	assert.Less(t, CompareCursors(lineAt(a, 0, 9), lineAt(sidecar, 0, 0)), 0)
	assert.Equal(t, 0, CompareCursors(lineAt(a, 0, 7), MomentAt(at(0))))
	assert.Equal(t, 0, CompareCursors(MomentAt(at(0)), lineAt(a, 0, 7)))
}

// A line past a cursor's ordinal at the cursor's moment sorts after it whichever
// instance id it carries: the resolve lags a restart, so a page can end on a
// line keyed with the old id that the next page keys with the new one, and the
// lines past it must not fall before the cursor.
func TestALaterOrdinalFollowsTheCursorWhateverItsID(t *testing.T) {
	t.Parallel()
	a := StreamKey{Namespace: "prod", Pod: "a", Container: "app"}
	for _, ids := range [][2]string{{"containerd://a-0", "containerd://a-1"}, {"containerd://a-1", "containerd://a-0"}} {
		end, next := lineAt(a, 0, 3), lineAt(a, 0, 4)
		end.ContainerID, next.ContainerID = ids[0], ids[1]
		assert.Greater(t, CompareCursors(next, end), 0, ids)
	}
}

// A bound keeps or drops the line it sits on as its inclusivity says, and a
// moment bound every line of the moment.
func TestBoundsKeepOrDropTheirLine(t *testing.T) {
	t.Parallel()
	a := StreamKey{Namespace: "prod", Pod: "a", Container: "app"}
	rec := nth("a", "app", 0, 1, "")
	assert.False(t, Bound{lineAt(a, 0, 1), true}.Below(rec))
	assert.True(t, Bound{lineAt(a, 0, 1), false}.Below(rec))
	assert.True(t, Bound{lineAt(a, 0, 2), true}.Below(rec))
	assert.False(t, Bound{MomentAt(at(0)), true}.Below(rec))
	assert.True(t, Bound{MomentAt(at(0)), false}.Below(rec))
	assert.False(t, Bound{MomentAt(at(0)), true}.Above(rec))
	assert.True(t, Bound{MomentAt(at(0)), false}.Above(rec))
	assert.False(t, Bound{}.Below(rec))
	assert.False(t, Bound{}.Above(rec))
}

// Each stream's bound is worked out from the one cursor as compareLineCursors orders
// lines: at the cursor's moment a stream whose names sort before the cursor's
// starts after the moment, one whose names sort after starts at it, the cursor's own
// stream starts past its ordinal, and another instance of its container at its
// ordinal, kept when its id sorts after the cursor's and dropped when before. Older
// is the mirror image, and a moment bounds every stream at the moment.
func TestEachStreamsBoundOffOneCursor(t *testing.T) {
	t.Parallel()
	moment := at(0).UnixNano()
	ats := func(ord int) logstore.Pos { return logstore.Pos{TS: moment, Ord: ord} }
	stream := func(pod, id string) Cursor {
		return Cursor{ContainerName{"prod", pod, "app"}, RunPlace{ContainerID: id}}
	}
	cursor := Bound{Cursor{ContainerName{"prod", "web-1", "app"}, RunPlace{"b", at(0), 2}}, false}
	for name, tc := range map[string]struct {
		in           Cursor
		newer, older logstore.Pos
	}{
		"names before":  {stream("web-0", "b"), ats(math.MaxInt), ats(math.MaxInt)},
		"names after":   {stream("web-2", "b"), ats(-1), ats(-1)},
		"its own":       {stream("web-1", "b"), ats(2), ats(2)},
		"an earlier id": {stream("web-1", "a"), ats(2), ats(3)},
		"a later id":    {stream("web-1", "c"), ats(1), ats(2)},
	} {
		assert.Equal(t, tc.newer, StreamPos(cursor, tc.in, Newer), name+", newer")
		assert.Equal(t, tc.older, StreamPos(cursor, tc.in, Older), name+", older")
	}

	own := stream("web-1", "b")
	kept := Bound{cursor.At, true}
	assert.Equal(t, ats(1), StreamPos(kept, own, Newer), "a kept line, newer")
	assert.Equal(t, ats(3), StreamPos(kept, own, Older), "a kept line, older")
	m := MomentAt(at(0))
	assert.Equal(t, ats(math.MaxInt), StreamPos(Bound{m, false}, own, Newer), "after a moment")
	assert.Equal(t, ats(-1), StreamPos(Bound{m, true}, own, Newer), "from a moment")
	assert.Equal(t, ats(-1), StreamPos(Bound{m, false}, own, Older), "before a moment")
	assert.Equal(t, ats(math.MaxInt), StreamPos(Bound{m, true}, own, Older), "up to a moment")
	assert.Equal(t, logstore.Oldest, StreamPos(Bound{}, own, Newer), "the start")
	assert.Equal(t, logstore.Newest, StreamPos(Bound{}, own, Older), "the end")
}

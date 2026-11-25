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
	"cmp"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// containerName is a container by name, which a restart does not change.
type ContainerName struct {
	Namespace, Pod, Container string
}

// runPlace is where a line sits within its container's lines: the runtime id
// the kubelet gave the run, so a restarted container's lines are told from the
// run before, and the line's moment and ordinal among the run's lines at that
// moment. A page cursor's seen holds one per container.
type RunPlace struct {
	ContainerID string
	Timestamp   time.Time
	Ordinal     int
}

// line cursor is a line's place in the scope's order: its container, and its
// place within it. Every line has one and no two share one, so a page can end
// at any line and the next can start just past it. One with no ContainerID is
// a moment, which stands before, or after, every line of that moment at once,
// a form only a page cursor holds; one with a zero Timestamp names nothing.
type Cursor struct {
	ContainerName
	RunPlace
}

// MomentAt is the line cursor of the moment t.
func MomentAt(t time.Time) Cursor { return Cursor{RunPlace: RunPlace{Timestamp: t}} }

// isZero is whether c names nothing.
func (c Cursor) IsZero() bool { return c.Timestamp.IsZero() }

// moment is whether c names a moment rather than a line.
func (c Cursor) IsMoment() bool { return c.ContainerID == "" }

// at is the line cursor of the line at p of c's run.
func (c Cursor) At(p logstore.Pos) Cursor {
	return Cursor{c.ContainerName, RunPlace{c.ContainerID, time.Unix(0, p.TS).UTC(), p.Ord}}
}

// CursorOf is rec's line cursor.
func CursorOf(rec Record) Cursor {
	return Cursor{
		ContainerName{rec.Stream.Namespace, rec.Stream.Pod, rec.Stream.Container},
		RunPlace{rec.ContainerID, rec.Timestamp, rec.Ordinal},
	}
}

// compareNames orders two containers by namespace, pod and container.
func CompareNames(a, b ContainerName) int {
	return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Pod, b.Pod), cmp.Compare(a.Container, b.Container))
}

// compareLineCursors orders a against b: by time, then, unless either is a moment,
// by namespace, pod, container, ordinal and instance id. The ordinal goes before
// the id because the id is the resolve's, which lags a restart: a page can end on
// a line keyed with the old id that the next page keys with the new one, and
// with the id deciding the tie, the new instance's later lines at that moment
// would all sort before the cursor and be lost.
func CompareCursors(a, b Cursor) int {
	if c := a.Timestamp.Compare(b.Timestamp); c != 0 || a.IsMoment() || b.IsMoment() {
		return c
	}
	return cmp.Or(
		CompareNames(a.ContainerName, b.ContainerName),
		cmp.Compare(a.Ordinal, b.Ordinal),
		cmp.Compare(a.ContainerID, b.ContainerID),
	)
}

// compare orders rec against p.
func compare(rec Record, p Cursor) int { return CompareCursors(CursorOf(rec), p) }

// CompareLines orders two lines as their cursors do: by moment, then, at one
// moment, by namespace, pod, container, ordinal and instance id.
func CompareLines(a, b Record) int { return CompareCursors(CursorOf(a), CursorOf(b)) }

// lineCursorVersion is the version a line cursor's JSON carries first.
const lineCursorVersion = 1

// lineCursorJSON is a line cursor as JSON, its keys in the order written: a
// line's every field, a container's moment its names and time, a moment its
// time alone.
type CursorJSON struct {
	V         int       `json:"v"`
	Namespace string    `json:"ns,omitempty"`
	Pod       string    `json:"pod,omitempty"`
	Container string    `json:"c,omitempty"`
	ID        string    `json:"id,omitempty"`
	Time      time.Time `json:"t"`
	Ordinal   *int      `json:"n,omitempty"`
}

// toJSON is c as its JSON object.
func (c Cursor) ToJSON() CursorJSON {
	j := CursorJSON{V: lineCursorVersion, Namespace: c.Namespace, Pod: c.Pod, Container: c.Container, Time: c.Timestamp.UTC()}
	if !c.IsMoment() {
		j.ID, j.Ordinal = c.ContainerID, &c.Ordinal
	}
	return j
}

// line cursor is j checked: a moment, a container's moment, or a line.
func (j CursorJSON) Cursor() (Cursor, error) {
	if j.V != lineCursorVersion {
		return Cursor{}, ErrCursorVersion
	}
	names := j.Namespace != "" || j.Pod != "" || j.Container != ""
	switch {
	case j.Time.IsZero(),
		names && !(validName(j.Namespace) && validName(j.Pod) && validName(j.Container)),
		(j.ID == "") != (j.Ordinal == nil),
		j.Ordinal != nil && (!names || !ValidContainerID(j.ID) || *j.Ordinal < 0 || *j.Ordinal == math.MaxInt):
		// StreamPos steps one past the ordinal, so MaxInt has no cursor past it.
		return Cursor{}, errBadCursor
	}
	c := Cursor{ContainerName{j.Namespace, j.Pod, j.Container}, RunPlace{ContainerID: j.ID, Timestamp: j.Time.UTC()}}
	if j.Ordinal != nil {
		c.Ordinal = *j.Ordinal
	}
	return c, nil
}

// Encode is c as a line cursor: its JSON, base64url with no padding.
func Encode(c Cursor) string {
	b, err := json.Marshal(c.ToJSON())
	if err != nil { // coverage:ignore a cursor's time is an RFC 3339 stamp, whose four-digit year always marshals
		panic(err) // a struct of strings, a time and an int always marshals
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

var (
	// errBadCursor refuses a line cursor this service did not write.
	errBadCursor = refusal.New(refusal.CodeCursorInvalid, "this is not a line cursor a page handed back")
	// errCursorVersion refuses a cursor another version of the service wrote.
	ErrCursorVersion = refusal.New(refusal.CodeCursorInvalid, "this cursor is from another version of Kubetail; read again without it")
)

// Decode is the line cursor s spells.
func Decode(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, errBadCursor
	}
	var j CursorJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return Cursor{}, errBadCursor
	}
	return j.Cursor()
}

// maxName is the longest namespace, pod or container name.
const maxName = 253

// validName is whether name is one Kubernetes takes for a namespace, a pod or a
// container: lower-case letters, digits, '-' and '.'.
func validName(name string) bool {
	if name == "" || len(name) > maxName {
		return false
	}
	for _, r := range name {
		if !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// maxContainerID is the longest runtime id a line cursor takes.
const maxContainerID = 256

// validContainerID is whether id is one a line cursor takes: what the resolve
// guarantees of every id it hands out, and nothing more, since a runtime spells
// its ids as it likes.
func ValidContainerID(id string) bool {
	return id != "" && len(id) <= maxContainerID && !strings.ContainsFunc(id, unicode.IsControl)
}

// Bound is one end of a read's range. At is a moment, standing at every line
// stamped then, or one line; Inclusive is whether a line at At is in range. A zero
// Bound is no bound.
type Bound struct {
	At        Cursor
	Inclusive bool
}

// below is whether rec is under the lower bound b.
func (b Bound) Below(rec Record) bool {
	if b.At.IsZero() {
		return false
	}
	c := compare(rec, b.At)
	return c < 0 || c == 0 && !b.Inclusive
}

// above is whether rec is over the upper bound b.
func (b Bound) Above(rec Record) bool {
	if b.At.IsZero() {
		return false
	}
	c := compare(rec, b.At)
	return c > 0 || c == 0 && !b.Inclusive
}

// StreamPos is b as a position in run's stream alone, where a page reading mode
// from b starts there: a newer page reads the stream's lines after it, an older
// page those before it, and with a zero b the page reads from the start or the
// end. b is one place across the scope, read as CompareCursors orders lines, so
// the pages of one scope join in every stream.
func StreamPos(b Bound, run Cursor, mode Direction) logstore.Pos {
	newer := mode == Newer
	if b.At.IsZero() {
		if newer {
			return logstore.Oldest
		}
		return logstore.Newest
	}
	moment := b.At.Timestamp.UnixNano()
	at := func(ord int) logstore.Pos { return logstore.Pos{TS: moment, Ord: ord} }
	// every and none read every line of the moment or none of it.
	every, none := logstore.Pos{TS: moment, Ord: -1}, logstore.Pos{TS: moment, Ord: math.MaxInt}
	if !newer {
		every, none = none, every
	}
	if b.At.IsMoment() {
		if b.Inclusive {
			return every
		}
		return none
	}
	names := CompareNames(run.ContainerName, b.At.ContainerName)
	switch {
	case names < 0 && newer, names > 0 && !newer:
		return none
	case names != 0:
		return every
	}
	// The ordinal decides before the id, so the stream's line at the cursor's ordinal
	// is the page's when the id puts it on the page's side, or when it is the
	// cursor's own line and the bound keeps it.
	ord := b.At.Ordinal
	id := cmp.Compare(run.ContainerID, b.At.ContainerID)
	switch {
	case id == 0 && !b.Inclusive, newer && id < 0, !newer && id > 0:
		return at(ord)
	case newer:
		return at(ord - 1)
	}
	return at(ord + 1)
}

// direction is which way a read goes: newer reads oldest first past its key, older
// newest first before it.
type Direction string

const (
	Newer Direction = "newer"
	Older Direction = "older"
)

// back is the other way.
func (d Direction) Back() Direction {
	if d == Newer {
		return Older
	}
	return Newer
}

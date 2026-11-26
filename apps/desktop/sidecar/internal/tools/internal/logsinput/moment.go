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
	"errors"
	"math"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/iso8601"
)

// Direction is the way a duration runs from now, which is the field's to say: a
// duration has no sign of its own.
type Direction int

const (
	Behind Direction = iota
	Ahead
)

// End is which end of a range a value is read for.
type End int

const (
	Since End = iota // the earliest nanosecond of the value
	Until            // the latest
)

// The moments a value can name lie between the epoch and the end of 9999; a log
// holds none outside them.
var (
	epoch = time.Unix(0, 0).UTC()
	last  = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
)

// spanYears is more years than lie between epoch and last.
const spanYears = 10000

var (
	errDatedOffset = errors.New("a date is UTC and takes no offset")
	errNotAMoment  = errors.New("neither an RFC 3339 time nor an ISO 8601 duration")
)

// MomentOf reads s as one end of a range on the clock now: an RFC 3339 time at any
// precision, of which Since takes the first nanosecond and Until the last; or an
// ISO 8601 duration, exact, measured from now in dir. A duration is applied in
// UTC, years and months first on the calendar, then weeks and days, then the time
// part.
func MomentOf(s string, now time.Time, end End, dir Direction) (time.Time, error) {
	at, unit, err := timeOf(s)
	switch {
	case err == nil && end == Until:
		at = at.Add(unit - 1)
	case errors.Is(err, errNotAMoment):
		at, err = durationOf(s, now, dir)
	}
	if err != nil {
		return time.Time{}, err
	}
	if at.Before(epoch) || at.After(last) {
		return time.Time{}, errNotAMoment
	}
	return at, nil
}

// MomentMessage is the refusal of a value MomentOf answered err for: the forms the
// field takes and the clock now, which the model has no other way to know; a date
// with an offset says first that a date is UTC.
func MomentMessage(err error, forms string, now time.Time) string {
	msg := forms + "; now is " + now.UTC().Format(time.RFC3339)
	if errors.Is(err, errDatedOffset) {
		msg = errDatedOffset.Error() + "; " + msg
	}
	return msg
}

// timeOf is the interval the time s spells, in UTC: its first nanosecond and its
// length, a day, a minute, a second, or a fraction of 1 to 9 digits.
func timeOf(s string) (time.Time, time.Duration, error) {
	if at, err := time.Parse(time.DateOnly, s); err == nil {
		return at, 24 * time.Hour, nil
	}
	if _, err := time.Parse(time.DateOnly+"Z07:00", s); err == nil {
		return time.Time{}, 0, errDatedOffset
	}
	if at, err := time.Parse("2006-01-02T15:04Z07:00", s); err == nil {
		return at.UTC(), time.Minute, nil
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, 0, errNotAMoment
	}
	_, frac, ok := strings.Cut(s, ".")
	if !ok {
		return at.UTC(), time.Second, nil
	}
	// time.Parse reads a tenth digit and drops it; a fraction is at most nine.
	digits := len(frac) - len(strings.TrimLeft(frac, "0123456789"))
	if digits > 9 {
		return time.Time{}, 0, errNotAMoment
	}
	return at.UTC(), time.Duration(math.Pow10(9 - digits)), nil
}

// durationOf is the moment the duration s names from now in dir.
func durationOf(s string, now time.Time, dir Direction) (time.Time, error) {
	p, err := iso8601.Parse(s)
	if err != nil {
		return time.Time{}, errNotAMoment
	}
	// The time part alone is a fixed length. Uncapped here: one a Duration cannot
	// hold lands outside the range MomentOf checks anyway.
	clock, err := iso8601.Span(iso8601.Parts{Hours: p.Hours, Minutes: p.Minutes, Seconds: p.Seconds, Nanos: p.Nanos}, math.MaxInt64)
	if err != nil {
		return time.Time{}, errNotAMoment
	}
	// No calendar count past the years between the epoch and 9999 lands in range,
	// and refusing one first keeps the narrowing to int exact on a 32-bit int.
	if p.Years > spanYears || p.Months > 12*spanYears || 7*p.Weeks+p.Days > 366*spanYears {
		return time.Time{}, errNotAMoment
	}
	sign := -1
	if dir == Ahead {
		sign = 1
	}
	at := calendar(now.UTC(), sign*int(p.Years), sign*int(p.Months))
	at = at.AddDate(0, 0, sign*int(7*p.Weeks+p.Days))
	return at.Add(time.Duration(sign) * clock), nil
}

// calendar is t moved by years and months, a day past the end of the target
// month clamped to its last day: AddDate would roll 31 March back to 3 March.
func calendar(t time.Time, years, months int) time.Time {
	at := t.AddDate(years, months, 0)
	if at.Day() != t.Day() {
		// The day overflowed into the next month; step back to the target's last day.
		at = at.AddDate(0, 0, -at.Day())
	}
	return at
}

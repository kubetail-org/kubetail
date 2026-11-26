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
	"testing"
	"time"

	_ "time/tzdata" // the DST case's zone, on every runner

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// now is the clock every case reads a duration on: 15 March 2027, 12:00 UTC.
var now = time.Date(2027, 3, 15, 12, 0, 0, 0, time.UTC)

// An RFC 3339 time is the moment it names, in UTC, whichever way the field runs.
func TestATimeIsReturnedInUTC(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 10, 18, 8, 14, 20, 5e8, time.UTC)
	for _, dir := range []Direction{Behind, Ahead} {
		got, err := MomentOf("2026-10-18T09:14:20.5+01:00", now, Since, dir)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, time.UTC, got.Location())
	}
}

// A time names the interval it spells, a day, a minute, a second or a fraction:
// Since takes its first nanosecond and Until its last.
func TestATimeIsItsInterval(t *testing.T) {
	t.Parallel()
	at := func(h, m, s, ns int) time.Time { return time.Date(2026, 10, 21, h, m, s, ns, time.UTC) }
	for s, want := range map[string][2]time.Time{
		"2026-10-21":                     {at(0, 0, 0, 0), at(23, 59, 59, 999999999)},
		"2026-10-21T05:00Z":              {at(5, 0, 0, 0), at(5, 0, 59, 999999999)},
		"2026-10-21T08:00+03:00":         {at(5, 0, 0, 0), at(5, 0, 59, 999999999)},
		"2026-10-21T05:00:00Z":           {at(5, 0, 0, 0), at(5, 0, 0, 999999999)},
		"2026-10-21T05:00:00.5Z":         {at(5, 0, 0, 5e8), at(5, 0, 0, 599999999)},
		"2026-10-21T05:00:00.120Z":       {at(5, 0, 0, 12e7), at(5, 0, 0, 120999999)},
		"2026-10-21T05:00:00.000000007Z": {at(5, 0, 0, 7), at(5, 0, 0, 7)},
	} {
		since, err := MomentOf(s, now, Since, Behind)
		require.NoError(t, err, s)
		until, err := MomentOf(s, now, Until, Behind)
		require.NoError(t, err, s)
		assert.Equal(t, want, [2]time.Time{since, until}, s)
	}
}

// A duration is exact, the same moment for either end.
func TestADurationIsTheSameForEitherEnd(t *testing.T) {
	t.Parallel()
	since, err := MomentOf("PT15M", now, Since, Behind)
	require.NoError(t, err)
	until, err := MomentOf("PT15M", now, Until, Behind)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-15*time.Minute), since)
	assert.Equal(t, since, until)
}

// An hour alone is no form, nor is a date with an offset, which says a date is UTC.
func TestAnHourAloneOrADatedOffsetIsRefused(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"2026-10-21T05Z", "2026-10-21T05", "2026-10-21T05:00", "2026-10-21T05:00:00.Z", "2026-10-21T05:00:00.1234567891Z"} {
		_, err := MomentOf(s, now, Since, Behind)
		assert.Error(t, err, s)
	}
	_, err := MomentOf("2026-10-21+03:00", now, Since, Behind)
	assert.ErrorIs(t, err, errDatedOffset)
}

// whole is each duration in whole units and how far it runs.
var whole = map[string]time.Duration{
	"P1W":     7 * 24 * time.Hour,
	"P3D":     3 * 24 * time.Hour,
	"PT15M":   15 * time.Minute,
	"PT1H30M": 90 * time.Minute,
	"P1DT12H": 36 * time.Hour,
	"P1W3D":   10 * 24 * time.Hour,
}

// A duration Behind is that far back from now.
func TestADurationBehindIsBackFromNow(t *testing.T) {
	t.Parallel()
	for s, d := range whole {
		got, err := MomentOf(s, now, Since, Behind)
		assert.NoError(t, err, s)
		assert.Equal(t, now.Add(-d), got, s)
	}
}

// A duration Ahead is that far on from now.
func TestADurationAheadIsOnFromNow(t *testing.T) {
	t.Parallel()
	for s, d := range whole {
		got, err := MomentOf(s, now, Since, Ahead)
		assert.NoError(t, err, s)
		assert.Equal(t, now.Add(d), got, s)
	}
}

// A fraction of a second reads as its digits padded to nanoseconds.
func TestAFractionOfASecondIsExact(t *testing.T) {
	t.Parallel()
	for s, d := range map[string]time.Duration{
		"PT1.5S":         1500 * time.Millisecond,
		"PT0.001S":       time.Millisecond,
		"PT0.123456789S": 123456789,
		"PT1M0.5S":       time.Minute + 500*time.Millisecond,
	} {
		got, err := MomentOf(s, now, Since, Behind)
		assert.NoError(t, err, s)
		assert.Equal(t, now.Add(-d), got, s)
	}
}

// day is midnight UTC on the date.
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Years and months move on the calendar, and a day past the end of the target
// month is that month's last day. Days are applied after the clamp.
func TestAMonthIsOnTheCalendar(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		s        string
		from, to time.Time
	}{
		{"P1M", day(2027, 3, 15), day(2027, 2, 15)},
		{"P1M", day(2027, 3, 31), day(2027, 2, 28)},
		{"P1M", day(2028, 3, 31), day(2028, 2, 29)},
		{"P1Y", day(2028, 2, 29), day(2027, 2, 28)},
		{"P1D", day(2027, 3, 31), day(2027, 3, 30)},
		{"P1M3D", day(2027, 3, 31), day(2027, 2, 25)},
	} {
		got, err := MomentOf(c.s, c.from, Since, Behind)
		assert.NoError(t, err, c.s)
		assert.Equal(t, c.to, got, "%s from %s", c.s, c.from)
	}
	got, _ := MomentOf("P1M", day(2027, 1, 31), Since, Ahead)
	assert.Equal(t, day(2027, 2, 28), got, "ahead clamps too")
}

// A day is 24 hours on a clock in a zone whose day that is not: the maths runs
// in UTC.
func TestADayIsADayAcrossDaylightSaving(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// New York's clocks went forward at 2am on 14 March 2027, a 23-hour day.
	local := time.Date(2027, 3, 14, 12, 0, 0, 0, ny)

	got, err := MomentOf("P1D", local, Since, Behind)
	assert.NoError(t, err)
	assert.Equal(t, local.UTC().Add(-24*time.Hour), got)
	assert.Equal(t, time.UTC, got.Location())
}

// What is neither a time nor an ISO 8601 duration naming a moment is refused: Go's
// durations, a sign, no unit, a T with no time unit, every unit zero, units out of
// order or twice, and a fraction outside seconds.
func TestNotAMomentIsRefused(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"", "15m", "-PT15M", "P", "PT", "P1DT", "PT0S", "P0D", "PT5M1H", "PT1M2M",
		"P1.5D", "PT1.5H", "P1.5DT1H", "PT1234567890S", "yesterday",
	} {
		for _, dir := range []Direction{Behind, Ahead} {
			_, err := MomentOf(s, now, Since, dir)
			assert.Error(t, err, s)
		}
	}
}

// A time part past what a time.Duration holds is refused rather than wrapped, as
// is a moment before the epoch going back or after 9999 going ahead.
func TestAMomentOutOfBoundsIsRefused(t *testing.T) {
	t.Parallel()
	nearEpoch := day(1970, 1, 2)
	near9999 := day(9999, 12, 30)
	for _, c := range []struct {
		s   string
		now time.Time
		dir Direction
	}{
		{"PT999999999H", now, Behind},
		{"PT999999999M", now, Ahead},
		{"PT2562047H47M16.854775808S", near9999, Behind},
		{"P999999999Y", nearEpoch, Behind},
		{"P999999999M", nearEpoch, Behind},
		{"P999999999W", nearEpoch, Behind},
		{"P10001Y", near9999, Behind},
		{"P2D", nearEpoch, Behind},
		{"P999999999Y", near9999, Ahead},
		{"P2D", near9999, Ahead},
	} {
		_, err := MomentOf(c.s, c.now, Since, c.dir)
		assert.Error(t, err, "%s from %s", c.s, c.now)
	}

	for _, s := range []string{"1969-12-31", "1969-12-31T23:59:59Z"} {
		_, err := MomentOf(s, now, Since, Behind)
		assert.Error(t, err, s)
	}

	got, err := MomentOf("P1D", nearEpoch, Since, Behind)
	assert.NoError(t, err)
	assert.Equal(t, time.Unix(0, 0).UTC(), got, "the epoch itself is in bounds")
	got, err = MomentOf("PT999999S", now, Since, Behind)
	assert.NoError(t, err)
	assert.Equal(t, now.Add(-999999*time.Second), got)
}

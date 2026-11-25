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

package iso8601

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each unit reads as its own count, and a fraction of a second as its digits
// padded to nanoseconds.
func TestParseReadsEachUnit(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]Parts{
		"P2Y":            {Years: 2},
		"P3M":            {Months: 3},
		"P1W":            {Weeks: 1},
		"P4D":            {Days: 4},
		"PT5H":           {Hours: 5},
		"PT6M":           {Minutes: 6},
		"PT7S":           {Seconds: 7},
		"PT1.5S":         {Seconds: 1, Nanos: 500_000_000},
		"PT0.123456789S": {Nanos: 123_456_789},
		"P1Y2M3W4DT5H6M7.8S": {
			Years: 1, Months: 2, Weeks: 3, Days: 4, Hours: 5, Minutes: 6, Seconds: 7, Nanos: 800_000_000,
		},
	} {
		got, err := Parse(s)
		require.NoError(t, err, s)
		assert.Equal(t, want, got, s)
	}
}

// What is not an ISO 8601 duration naming some time is refused: Go's durations, a
// sign, no unit, a T with no time unit, every unit zero, units out of order or
// twice, a fraction outside seconds, and a count over nine digits.
func TestParseRefusesWhatIsNotADuration(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"", "15m", "-PT15M", "P", "PT", "P1DT", "PT0S", "P0D", "PT5M1H", "PT1M2M",
		"P1.5D", "PT1.5H", "P1.5DT1H", "PT1234567890S", "PT1.1234567890S", "pt5m", "yesterday",
	} {
		_, err := Parse(s)
		assert.Error(t, err, s)
	}
}

// A span is a fixed length of time: weeks, days and the time part summed.
func TestSpanIsAFixedLength(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]time.Duration{
		"P1W":            7 * 24 * time.Hour,
		"P1DT12H":        36 * time.Hour,
		"PT5M":           5 * time.Minute,
		"PT1M0.5S":       time.Minute + 500*time.Millisecond,
		"PT0.000000001S": 1,
	} {
		p, err := Parse(s)
		require.NoError(t, err, s)
		got, err := Span(p, week)
		require.NoError(t, err, s)
		assert.Equal(t, want, got, s)
	}
}

// week is the cap the span tests read under.
const week = 7 * 24 * time.Hour

// Years and months have no fixed length, so a span refuses them; and a span over
// its cap is refused, however far over, without wrapping.
func TestSpanRefusesTheCalendarAndWhatIsOverItsCap(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"P1Y", "P1M", "P1MT1H", "P1WT0.000000001S", "P8D", "P999999999W", "PT999999999H"} {
		p, err := Parse(s)
		require.NoError(t, err, s)
		_, err = Span(p, week)
		assert.Error(t, err, s)
	}
	p, _ := Parse("P1W")
	got, err := Span(p, week)
	require.NoError(t, err)
	assert.Equal(t, week, got, "the cap itself is in")
}

// At the widest cap a time.Duration holds, a fraction past it is refused rather
// than wrapped to a negative length, and the cap itself is in.
func TestSpanRefusesAFractionPastTheWidestCap(t *testing.T) {
	t.Parallel()
	p, err := Parse("PT2562047H47M16.854775808S")
	require.NoError(t, err)
	_, err = Span(p, math.MaxInt64)
	assert.Error(t, err)

	p, err = Parse("PT2562047H47M16.854775807S")
	require.NoError(t, err)
	got, err := Span(p, math.MaxInt64)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(math.MaxInt64), got)
}

// Format is the shortest duration naming d, days first and no weeks; no span is
// the empty string.
func TestFormatIsTheShortestForm(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		0:                           "",
		week:                        "P7D",
		36 * time.Hour:              "P1DT12H",
		24 * time.Hour:              "P1D",
		300 * time.Second:           "PT5M",
		90 * time.Minute:            "PT1H30M",
		1500 * time.Millisecond:     "PT1.5S",
		time.Minute + time.Second/2: "PT1M0.5S",
		1:                           "PT0.000000001S",
		time.Hour + time.Second:     "PT1H1S",
	} {
		assert.Equal(t, want, Format(d), d)
	}
}

// Every unit goes through Format, Parse and Span to the length it started as.
func TestFormatRoundTrips(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{week, 24 * time.Hour, 3 * time.Hour, 7 * time.Minute, 9 * time.Second, 123456789, 49*time.Hour + 30*time.Minute + 1500*time.Millisecond} {
		p, err := Parse(Format(d))
		require.NoError(t, err, d)
		got, err := Span(p, week)
		require.NoError(t, err, d)
		assert.Equal(t, d, got)
	}
}

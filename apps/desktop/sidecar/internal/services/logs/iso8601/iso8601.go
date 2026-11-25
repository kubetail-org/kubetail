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

// Package iso8601 reads and writes ISO 8601 durations, the one way a span of
// time is spelled on the wire and to the model.
package iso8601

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parts is an ISO 8601 duration as written: each unit's count, Nanos the
// seconds' fraction.
type Parts struct {
	Years, Months, Weeks, Days, Hours, Minutes, Seconds, Nanos int64
}

// duration is an ISO 8601 duration: each unit at most once, in the standard's
// order, a fraction in seconds alone. Every count has at most nine digits. What
// it cannot check, Parse does: a T has a time unit after it, and some unit is not
// zero.
var duration = regexp.MustCompile(`^P(?:(\d{1,9})Y)?(?:(\d{1,9})M)?(?:(\d{1,9})W)?(?:(\d{1,9})D)?(?:T(?:(\d{1,9})H)?(?:(\d{1,9})M)?(?:(\d{1,9})(?:\.(\d{1,9}))?S)?)?$`)

var (
	errSyntax   = errors.New("not an ISO 8601 duration such as PT5M: each unit at most once, in order, a fraction in seconds alone")
	errNothing  = errors.New("a duration names some time")
	errCalendar = errors.New("a span has no years or months, which have no fixed length")
)

// Parse is s read as an ISO 8601 duration. It refuses a unit out of order or
// repeated, a count over nine digits, a fraction outside seconds, and a duration
// of nothing.
func Parse(s string) (Parts, error) {
	m := duration.FindStringSubmatch(s)
	if m == nil || strings.Contains(s, "T") && m[5]+m[6]+m[7] == "" {
		return Parts{}, errSyntax
	}
	count := func(digits string) int64 {
		n, _ := strconv.ParseInt(digits, 10, 64) // nine digits at most, or "" for 0
		return n
	}
	p := Parts{
		Years: count(m[1]), Months: count(m[2]), Weeks: count(m[3]), Days: count(m[4]),
		Hours: count(m[5]), Minutes: count(m[6]), Seconds: count(m[7]),
		// The fraction's digits are padded on the right, so the 5 of 1.5 is 500,000,000 ns.
		Nanos: count((m[8] + "000000000")[:9]),
	}
	if p == (Parts{}) {
		return Parts{}, errNothing
	}
	return p, nil
}

// Span is p as a fixed length of time, refusing years and months, which have
// none, and anything over max.
func Span(p Parts, max time.Duration) (time.Duration, error) {
	if p.Years != 0 || p.Months != 0 {
		return 0, errCalendar
	}
	secs := (7*p.Weeks+p.Days)*86400 + p.Hours*3600 + p.Minutes*60 + p.Seconds
	// Compared in parts before d is built: a time.Duration holds about 292 years,
	// and past that d would wrap negative and pass a cap.
	maxSecs, maxNanos := int64(max/time.Second), int64(max%time.Second)
	if secs > maxSecs || secs == maxSecs && p.Nanos > maxNanos {
		return 0, fmt.Errorf("a span is at most %s", Format(max))
	}
	return time.Duration(secs)*time.Second + time.Duration(p.Nanos), nil
}

// Format is d as the shortest ISO 8601 duration that names it, days first: 36
// hours is P1DT12H, 300 seconds PT5M, 1.5 seconds PT1.5S. Zero, no span, is "".
func Format(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	days, d := d/(24*time.Hour), d%(24*time.Hour)
	hours, d := d/time.Hour, d%time.Hour
	minutes, d := d/time.Minute, d%time.Minute
	seconds, nanos := d/time.Second, d%time.Second

	var b strings.Builder
	b.WriteString("P")
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	if hours+minutes+seconds+nanos == 0 {
		return b.String()
	}
	b.WriteString("T")
	if hours > 0 {
		fmt.Fprintf(&b, "%dH", hours)
	}
	if minutes > 0 {
		fmt.Fprintf(&b, "%dM", minutes)
	}
	if seconds+nanos > 0 {
		b.WriteString(strconv.FormatInt(int64(seconds), 10))
		if nanos > 0 {
			b.WriteString("." + strings.TrimRight(fmt.Sprintf("%09d", nanos), "0"))
		}
		b.WriteString("S")
	}
	return b.String()
}

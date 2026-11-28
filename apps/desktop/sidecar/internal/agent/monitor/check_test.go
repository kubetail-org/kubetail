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

package monitor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// An observation over a bound is dropped and its signal reads invalid, its
// other observations with it; the reading's other signals fold as they came.
func TestAnObservationOverABoundMakesItsSignalInvalid(t *testing.T) {
	for name, over := range map[string]func(*Observation){
		"label": func(o *Observation) { o.Subject.Label = strings.Repeat("x", maxLabelBytes+1) },
		"attributes": func(o *Observation) {
			o.Subject.Attributes = map[string]string{"a": strings.Repeat("x", maxAttributesBytes)}
		},
		"detail": func(o *Observation) { o.Detail = map[string]string{"d": strings.Repeat("x", maxDetailBytes)} },
	} {
		t.Run(name, func(t *testing.T) {
			r := seeing(crash.Name, "web", "api")
			over(&r.Observations[0])
			r.Observations = append(r.Observations, seeing(flip.Name, "db").Observations...)

			got := check(testRegistration(), r)

			assert.Equal(t, map[string]ReadErrorCode{crash.Name: CodeInvalid}, got.Unknown)
			assert.Equal(t, seeing(flip.Name, "db").Observations, got.Observations)
		})
	}
}

// A name no registration holds is dropped, in an observation or in Unknown, so
// the target's state names only registered signals.
func TestANameTheIntegrationDidNotRegisterIsDropped(t *testing.T) {
	r := seeing("fake.other", "web")
	r.Unknown = map[string]ReadErrorCode{"fake.gone": CodeNotReady, flip.Name: CodeUnavailable}

	got := check(testRegistration(), r)

	assert.Empty(t, got.Observations)
	assert.Equal(t, map[string]ReadErrorCode{flip.Name: CodeUnavailable}, got.Unknown)
}

// A code the core lacks reads as invalid, so every stored code has a value on the wire.
func TestACodeTheCoreLacksReadsAsInvalid(t *testing.T) {
	r := Reading{UID: "uid", Unknown: map[string]ReadErrorCode{flip.Name: "throttled", crash.Name: CodeTimeout}}

	got := check(testRegistration(), r)

	assert.Equal(t, map[string]ReadErrorCode{flip.Name: CodeInvalid, crash.Name: CodeTimeout}, got.Unknown)
}

// A reading that names no UID judges nothing: every signal it gave no code reads
// not_ready, and its observations are dropped.
func TestAReadingWithNoUIDJudgesNothing(t *testing.T) {
	r := seeing(crash.Name, "web")
	r.UID = ""
	r.Unknown = map[string]ReadErrorCode{flip.Name: CodeDisabled}

	got := check(testRegistration(), r)

	assert.Empty(t, got.Observations)
	assert.Equal(t, map[string]ReadErrorCode{crash.Name: CodeNotReady, flip.Name: CodeDisabled}, got.Unknown)
}

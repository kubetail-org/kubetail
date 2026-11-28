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
	"encoding/json"
	"log/slog"
)

// The bounds a reading's text keeps. The check is the one place they are
// enforced: no table tests a length, so a value let through cannot fail a commit.
const (
	maxLabelBytes      = 256
	maxAttributesBytes = 4 << 10 // as JSON
	maxDetailBytes     = 16 << 10
)

// check is r as the fold may take it. An observation over a bound is dropped and
// its signal is unknown with CodeInvalid; an observation of a signal r leaves
// unknown is dropped; a name reg does not hold is dropped and logged, and a code
// the core lacks reads as CodeInvalid. A reading
// that names no UID judges nothing, its signals not_ready unless it gave a code.
func check(reg *registration, r Reading) Reading {
	unknown := map[string]ReadErrorCode{}
	for name, code := range r.Unknown {
		if !reg.names[name] {
			slog.Warn("monitor: a reading names a signal its integration did not register", "integration", reg.Name, "signal", name)
			continue
		}
		if !code.known() {
			slog.Warn("monitor: a reading gives a code the core lacks", "integration", reg.Name, "signal", name, "code", string(code))
			code = CodeInvalid
		}
		unknown[name] = code
	}
	if r.UID == "" {
		for _, s := range reg.Signals {
			if _, ok := unknown[s.Name]; !ok {
				unknown[s.Name] = CodeNotReady
			}
		}
	}
	var kept []Observation
	for _, o := range r.Observations {
		if !reg.names[o.Signal] {
			slog.Warn("monitor: a reading observes a signal its integration did not register", "integration", reg.Name, "signal", o.Signal)
			continue
		}
		if !withinBounds(o) {
			if _, ok := unknown[o.Signal]; !ok {
				unknown[o.Signal] = CodeInvalid
			}
			continue
		}
		kept = append(kept, o)
	}
	// Last, since an invalid observation makes its signal's others unjudged too.
	out := Reading{UID: r.UID, Unknown: unknown}
	for _, o := range kept {
		if _, ok := unknown[o.Signal]; !ok {
			out.Observations = append(out.Observations, o)
		}
	}
	return out
}

// withinBounds is whether o's label, attributes and detail each fit their bound.
func withinBounds(o Observation) bool {
	return len(o.Subject.Label) <= maxLabelBytes &&
		jsonLen(o.Subject.Attributes) <= maxAttributesBytes &&
		jsonLen(o.Detail) <= maxDetailBytes
}

// jsonLen is m's length as the JSON object it is stored as.
func jsonLen(m map[string]string) int {
	b, _ := json.Marshal(m) // a map of strings always marshals
	return len(b)
}

// unknownAll is a reading that judged nothing, every signal of reg under code.
func unknownAll(reg *registration, code ReadErrorCode) Reading {
	unknown := make(map[string]ReadErrorCode, len(reg.Signals))
	for _, s := range reg.Signals {
		unknown[s.Name] = code
	}
	return Reading{Unknown: unknown}
}

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
	"testing"

	"github.com/stretchr/testify/assert"
)

// An occurrence that starts alerts or queues a triage as the setting, the
// occurrence and its incident say: with automatic triage off, the incident's
// first ordinary one alerts unless an automatic triage is queued; on, only the incident's first ordinary occurrence and its third queue
// one, a queued automatic triage answers for it, and a queued manual one alerts.
func TestAnOccurrenceAlertsOrQueuesByTheRule(t *testing.T) {
	opens := incidentAtStart{opens: true}
	stillBaseline := incidentAtStart{baseline: true}
	ordinary := incidentAtStart{}
	for name, c := range map[string]struct {
		automatic, baseline bool
		number              int
		incident            incidentAtStart
		want                startRule
	}{
		"off, ordinary":                       {number: 1, incident: opens, want: startRule{alerts: true}},
		"off, a later ordinary":               {number: 2, incident: ordinary},
		"off, the first ordinary":             {number: 2, incident: stillBaseline, want: startRule{alerts: true}},
		"off, an automatic triage queued":     {number: 1, incident: incidentAtStart{opens: true, queued: triggerAutomatic}},
		"off, a manual triage queued":         {number: 1, incident: incidentAtStart{opens: true, queued: triggerManual}, want: startRule{alerts: true}},
		"off, baseline":                       {baseline: true, number: 1, incident: opens},
		"on, baseline":                        {automatic: true, baseline: true, number: 1, incident: opens},
		"on, a baseline third":                {automatic: true, baseline: true, number: 3, incident: stillBaseline},
		"on, one that opens":                  {automatic: true, number: 1, incident: opens, want: startRule{queues: true}},
		"on, the first ordinary":              {automatic: true, number: 2, incident: stillBaseline, want: startRule{queues: true}},
		"on, the second":                      {automatic: true, number: 2, incident: ordinary},
		"on, the third":                       {automatic: true, number: 3, incident: ordinary, want: startRule{queues: true}},
		"on, a first ordinary that is third":  {automatic: true, number: 3, incident: stillBaseline, want: startRule{queues: true}},
		"on, the fourth":                      {automatic: true, number: 4, incident: ordinary},
		"on, an automatic triage queued":      {automatic: true, number: 3, incident: incidentAtStart{queued: triggerAutomatic}},
		"on, a manual triage queued":          {automatic: true, number: 2, incident: incidentAtStart{baseline: true, queued: triggerManual}, want: startRule{alerts: true}},
		"on, a manual triage queued, a later": {automatic: true, number: 4, incident: incidentAtStart{queued: triggerManual}},
	} {
		assert.Equal(t, c.want, ruleAtStart(c.automatic, c.baseline, c.number, c.incident), name)
	}
}

// An automatic triage alerts when it answers attention or fails; a dismiss and
// every manual triage raise nothing.
func TestATriagesOutcomeAlertsByTheRule(t *testing.T) {
	attention := triageOutcome{state: TriageStateDone, verdict: TriageVerdictAttention}
	dismiss := triageOutcome{state: TriageStateDone, verdict: TriageVerdictDismiss}
	failed := failedOutcome(TriageCodeRunFailed)
	assert.True(t, attention.alerts(triggerAutomatic))
	assert.True(t, failed.alerts(triggerAutomatic))
	assert.False(t, dismiss.alerts(triggerAutomatic))
	for _, o := range []triageOutcome{attention, dismiss, failed} {
		assert.False(t, o.alerts(triggerManual))
	}
}

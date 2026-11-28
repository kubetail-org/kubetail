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

// The alert rule. With automatic triage off, an incident alerts at its first
// ordinary occurrence, unless an automatic triage queued before the switch
// still answers for it. On, an automatic triage decides: its incident alerts when
// the triage answers attention or fails, and an occurrence that queues none, for
// a manual triage already queued, alerts at once. An incident alerts at most
// once, since alerted_at is written by coalesce.

// triageTrigger is who asked for a triage, as incidents.triage_trigger holds it.
type triageTrigger string

const (
	triggerManual    triageTrigger = "manual"    // the user's press
	triggerAutomatic triageTrigger = "automatic" // the triage setting, at an occurrence's start
)

// recurringOccurrence is the occurrence that queues an automatic triage again.
const recurringOccurrence = 3

// incidentAtStart is what the rule reads of the incident an occurrence starts in.
type incidentAtStart struct {
	opens    bool          // the occurrence opens it
	baseline bool          // every occurrence it has had was baseline
	queued   triageTrigger // who asked for its queued triage; "" for none
}

// startRule is what an occurrence that starts does: raise its incident's alert,
// or queue an automatic triage of it.
type startRule struct{ alerts, queues bool }

// ruleAtStart is the rule for occurrence number, baseline or not, starting in
// incident, with automatic triage on or off. A baseline occurrence does
// neither. With it off, the incident's first ordinary occurrence alerts, unless
// an automatic triage is queued: so a later one never alerts for an incident a
// triage dismissed. With it on, the incident's first ordinary occurrence and its third
// queue a triage, unless one is queued: an automatic one answers for it, and a
// manual one, whose report the user reads, lets the incident alert untriaged.
func ruleAtStart(automatic, baseline bool, number int, incident incidentAtStart) startRule {
	firstOrdinary := incident.opens || incident.baseline
	switch {
	case baseline:
		return startRule{}
	case !automatic:
		// A started automatic triage still answers for its incident.
		return startRule{alerts: firstOrdinary && incident.queued != triggerAutomatic}
	case !firstOrdinary && number != recurringOccurrence:
		return startRule{}
	case incident.queued == triggerAutomatic:
		return startRule{}
	case incident.queued == triggerManual:
		return startRule{alerts: true}
	}
	return startRule{queues: true}
}

// alerts is whether a triage asked for by trigger that ended as o raises its
// incident's alert: an automatic one that answered attention or failed.
func (o triageOutcome) alerts(trigger triageTrigger) bool {
	return trigger == triggerAutomatic && (o.state == TriageStateFailed || o.verdict == TriageVerdictAttention)
}

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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// The ids the reads serve, each a UUID string.
type (
	MonitorTargetID      string
	IncidentID           string
	IncidentOccurrenceID string
)

// ReadState is what a target's last reading could judge.
type ReadState string

const (
	ReadStateNeverRead ReadState = "never_read" // no reading has committed
	ReadStateOk        ReadState = "ok"         // every signal was judged
	ReadStatePartial   ReadState = "partial"    // some signals were not
	ReadStateFailed    ReadState = "failed"     // no signal was
)

// The served records are compared by value, so the watches' diff is ==: a
// nullable value is a zero value beside a flag, never a pointer, and JSON is
// held as the string it is stored as, for the resolver to decode.

// MonitorTarget is a target as the targets watch serves it.
type MonitorTarget struct {
	ID          MonitorTargetID
	Integration string
	Key         string
	Label       string
	CurrentUID  string // "" before a reading named one
	Enabled     bool
	ReadState   ReadState
	// UnknownJSON is each registered signal the last reading could not judge, by
	// name, with its ReadErrorCode, as a JSON object with sorted keys.
	UnknownJSON string
}

// MonitorSignal is a registered signal as the list serves it.
type MonitorSignal struct {
	Name        string
	Integration string
	Label       string
	Severity    Severity
}

// Incident is an incident as the incidents watch serves it.
type Incident struct {
	ID              IncidentID
	TargetID        MonitorTargetID
	TargetUID       string
	SubjectLabel    string
	AttributesJSON  string // incidents.subject_attributes
	Signal          string
	SignalLabel     string
	Severity        Severity
	FirstSeenAt     time.Time
	Open            bool
	OccurrenceCount int
	LastStartedAt   time.Time // its newest occurrence's start
	LastSeenAt      time.Time // zero while open
	LastEndReason   EndReason // "" while open
	Baseline        bool      // no occurrence of it has been ordinary yet
}

// IncidentOccurrence is an occurrence as the occurrence feed serves it.
type IncidentOccurrence struct {
	ID         IncidentOccurrenceID
	IncidentID IncidentID
	Number     int
	StartedAt  time.Time
	EndedAt    time.Time // zero while open
	EndReason  EndReason // "" while open
	DetailJSON string    // incident_occurrences.detail
}

// MonitorTargetCollectionWatchFrame is one frame on the targets watch. Target is
// nil on the Bookmark.
type MonitorTargetCollectionWatchFrame struct {
	Type   apimeta.CollectionWatchFrameType
	Target *MonitorTarget
}

// IncidentCollectionWatchFrame is one frame on the incidents watch. Incident is
// nil on the Bookmark.
type IncidentCollectionWatchFrame struct {
	Type     apimeta.CollectionWatchFrameType
	Incident *Incident
}

// IncidentOccurrenceEventFeedFrameType classifies a frame on the occurrence feed.
type IncidentOccurrenceEventFeedFrameType string

const (
	// IncidentOccurrenceEventFeedFrameOccurrence carries an occurrence to upsert by id.
	IncidentOccurrenceEventFeedFrameOccurrence IncidentOccurrenceEventFeedFrameType = "Occurrence"
	// IncidentOccurrenceEventFeedFrameBookmark closes the snapshot, once, carrying none.
	IncidentOccurrenceEventFeedFrameBookmark IncidentOccurrenceEventFeedFrameType = "Bookmark"
)

// IncidentOccurrenceEventFeedFrame is one frame on an incident's occurrence feed.
// IncidentID is the feed's scope, on every frame; Occurrence is nil on the Bookmark.
type IncidentOccurrenceEventFeedFrame struct {
	Type       IncidentOccurrenceEventFeedFrameType
	IncidentID IncidentID
	Occurrence *IncidentOccurrence
}

// ListSignals is every registered signal, in registration order.
func (m *Monitor) ListSignals() []MonitorSignal {
	out := []MonitorSignal{}
	for _, reg := range m.regs {
		for _, s := range reg.Signals {
			out = append(out, MonitorSignal{Name: s.Name, Integration: reg.Name, Label: s.Label, Severity: s.Severity})
		}
	}
	return out
}

// SetEnabled turns a target's monitoring on or off, in its own transaction, and
// answers the target as the switch left it. ErrNoTarget for no such target, or
// one whose integration the build lacks.
func (m *Monitor) SetEnabled(ctx context.Context, id MonitorTargetID, enabled bool) (MonitorTarget, error) {
	row, err := m.store.setEnabled(ctx, string(id), enabled)
	if err != nil {
		return MonitorTarget{}, err
	}
	return m.servedTarget(row)
}

// servedTarget is row as served: its unknown signals and read state derived
// against the signals its integration registers, so an entry for a signal the
// build lacks is left out.
func (m *Monitor) servedTarget(row storedTarget) (MonitorTarget, error) {
	var stored map[string]ReadErrorCode
	if err := json.Unmarshal([]byte(row.unknown), &stored); err != nil {
		return MonitorTarget{}, fmt.Errorf("read unknown signals: %w", err)
	}
	reg := m.byName[row.integration]
	unknown := map[string]ReadErrorCode{}
	for name, code := range stored {
		if reg.names[name] {
			unknown[name] = code
		}
	}
	var state ReadState
	switch {
	case !row.read:
		state = ReadStateNeverRead
	case len(unknown) == 0:
		state = ReadStateOk
	case len(unknown) == len(reg.names):
		state = ReadStateFailed
	default:
		state = ReadStatePartial
	}
	b, _ := json.Marshal(unknown) // a map of strings always marshals, its keys sorted
	return MonitorTarget{
		ID: MonitorTargetID(row.id), Integration: row.integration, Key: row.key, Label: row.label,
		CurrentUID: row.uid, Enabled: row.enabled, ReadState: state, UnknownJSON: string(b),
	}, nil
}

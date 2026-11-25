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

// Package logline is what a log line is: a Record, the cursor that names it, the
// bounds a read keeps within, and how its text is cleaned, matched and shown to
// the model.
package logline

import "time"

// Record is one log line as the sidecar read it: when the container runtime
// stamped it, the message cleaned by KeepColor, the log it came from and where
// that log's pod runs, the runtime id of the instance it came from, and its
// Ordinal, its index among that instance's lines at that moment. A cursor carries
// the id and the ordinal, and the wire neither. Every string came from the
// cluster. Gap is the hole before it, nil with none: every marked hole between it
// and the line of its stream before it that the view passes.
type Record struct {
	Timestamp   time.Time
	Message     string
	Stream      StreamKey
	Place       Place
	ContainerID string
	Ordinal     int
	Gap         *Gap
	// TruncatedBytes is the bytes a request's MaxMessageBytes cut off Message, 0
	// when whole. The file's line is taken as the whole message.
	TruncatedBytes int
}

// Gap is a range of a stream whose lines may be missing: neither the cluster nor the
// file holds them. After is the stamp of the last known line
// before it, Before that of the first known line past it.
type Gap struct {
	After  time.Time `json:"after"`
	Before time.Time `json:"before"`
}

// StreamKey names one log a line came from: a pod's container, and whether it is
// the instance before the last restart.
type StreamKey struct {
	Namespace string
	Pod       string
	Container string
	Previous  bool
}

// Place is where a pod runs: its node and the node's region, zone, OS and
// architecture, empty where the mirror does not know. It describes a stream and
// takes no part in naming it.
type Place struct {
	Node   string
	Region string
	Zone   string
	OS     string
	Arch   string
}

// Cursor is the cursor naming rec, which a fetch or a follow resumes past.
func (rec Record) Cursor() string { return Encode(CursorOf(rec)) }

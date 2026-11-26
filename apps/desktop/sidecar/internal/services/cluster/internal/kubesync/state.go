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

// What a sweep and a kind stand behind, and this leaf's reason vocabulary. The two states
// differ in shape because the two differ in kind: a sweep is a set of passes, and a kind's
// sync is a live stream.
package kubesync

import (
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
)

// The supervisor's bookkeeping, aliased rather than copied so an Observation carries exactly
// what it recorded — the same aliases kubeconn declares, for the same reason. Reason
// stays this package's vocabulary; the supervisor treats it as opaque.
type (
	Observation[T any] = supervisor.JobObservation[T]
	Attempt            = supervisor.Attempt
	Attempts           = supervisor.Attempts
	Verdict            = supervisor.Verdict
	Reason             = supervisor.Reason
)

// The discovery vocabulary. There is no Watching or Stale here: nothing behind a sweep
// can prove itself live, so Discovering/Discovered are what Syncing/Watching are for a
// collection that can only be listed.
const (
	// ReasonNoConnection: nothing has reached the server, so the session is suspended.
	ReasonNoConnection = "NoConnection"
	// ReasonIdentityMismatch: the context's connection does not answer as ServerUID.
	ReasonIdentityMismatch = "IdentityMismatch"
	// ReasonDiscovering: a sweep is running and none has answered yet.
	ReasonDiscovering = "Discovering"
	// ReasonDiscovered: a sweep has answered; the kind set is current.
	ReasonDiscovered = "Discovered"
	// ReasonPartial: the fan-out committed what it read, and some group-versions did not
	// answer. Its own verdict because neither neighbour is honest about a sweep where
	// twelve of fourteen legs worked — Discovered would reset the backoff ladder over an
	// aggregated API that is down, and DiscoveryFailed would climb it while most of the
	// catalog refreshed fine.
	ReasonPartial = "Partial"
	// ReasonDiscoveryFailed: the sweep failed and is retrying.
	ReasonDiscoveryFailed = "DiscoveryFailed"
	// ReasonStoreFailed: the cache's file would not open, so nothing under it is armed.
	// A failing read like DiscoveryFailed, and it outranks "has yet to answer" the same
	// way — but it clears on its own never: the cache stays broken until someone clears it.
	ReasonStoreFailed = "StoreFailed"
)

// The per-kind vocabulary. A kind reports NoConnection and IdentityMismatch too: every run
// under a session suspends at the same gate, and each says so for itself.
const (
	// ReasonResyncing: the cache holds this kind's rows, but the position they were current
	// at is one the server no longer serves from, so they are being reconciled against a
	// fresh list. Its own reason because the rows are still served throughout — unlike
	// Syncing, where there is nothing to serve.
	ReasonResyncing = "Resyncing"
	// ReasonSyncing: cold-listing a kind with nothing cached.
	ReasonSyncing = "Syncing"
	// ReasonResuming: re-establishing from a cookie, and slow enough to be worth saying.
	ReasonResuming = "Resuming"
	// ReasonWatching: caught up and streaming deltas, proven live.
	ReasonWatching = "Watching"
	// ReasonStale: caught up, but a stream that has sent a bookmark has since gone
	// bookmarkStaleAfter without a frame. A stream that sends none never reads Stale.
	ReasonStale = "Stale"
	// ReasonSyncFailed: the run failed and is retrying.
	ReasonSyncFailed = "SyncFailed"
)

// DiscoveryState is the sweep's standing answer for one cache: a verdict, and the three
// reads behind it.
//
// **It carries nothing a sweep found.** The kinds go to kind_catalog and are read back
// from there, and which group-versions a cluster serves is that table's business too.
// What is left is how discovery is DOING.
//
// Each read is accounted for on its own, so one failing says so without dragging the
// others' verdict with it, and Observation carries the supervisor's own record — the last
// attempt's verdict, reason and message, the next attempt, the failure streak — so
// nothing here restates it.
type DiscoveryState struct {
	// Reason and Message are the whole sweep's verdict, and the one thing here not
	// readable off a probe: which of them decides is a PRECEDENCE rule — a suspended
	// session over a failing read, a failing read over one that has yet to answer. It is
	// made here because the news feed has to gate on it either way, and a boundary
	// folding its own would fold it differently.
	Reason  string
	Message string

	// What each read commits is what the next one needs, and no more. Resources commits
	// the FINGERPRINT it wrote to kind_catalog rather than the catalog itself: the rows
	// belong on disk, and a fingerprint is all "the answer moved" requires.
	APIVersions Observation[[]string] // GET /api — the core group's versions
	APIGroups   Observation[[]string] // GET /apis — the group-versions served, the fan-out's input
	Resources   Observation[uint64]   // the fan-out — the catalog fingerprint it committed
}

// KindState is one kind's standing answer — a live stream's, where DiscoveryState is a
// set of passes, and the difference decides every field here. It carries no identity: the
// caller named the kind to read it.
//
// **Nothing stores it.** It is assembled at read from the three things that own its parts: the
// reason the worker committed, the supervisor's own pacing, and the session's stamps. A stored
// copy would be a stale duplicate of all three.
//
// The worker's value is the REASON alone. Everything else a live stream stands behind is already
// the supervisor's — when the reason last moved is when it last committed, and the pacing is its
// Attempts — so a value carrying them would be a second copy to keep in step.
//
// **It carries no Observation, where DiscoveryState carries three**, and the rule behind that is
// what makes each shape right. A sweep is three independent reads, each separately interesting,
// and each one's Attempts means exactly what it says — so handing them over is the answer. One
// worker's record is only correct through this seam's gates: its last exit describes the kind
// while it is DOWN and is the failure it recovered from once it is up again, and its next attempt
// is the schedule a running stream was dispatched on rather than a retry. Those are the two facts
// a reader is likeliest to get wrong, so they are folded here rather than published raw.
//
// A pass is judged by its last attempt. A stream is judged by whether it is established
// now and has recently proven itself alive, which is a different question and needs
// different evidence:
//
//   - **Silence is ambiguous.** A watch sending nothing is either a quiet collection or a
//     wedged connection. LastLiveAt is what separates them — a delta, a bookmark, or a
//     frameless stream that stood a whole window, stamped at its open, since not every
//     collection sends bookmarks — and Stale is a stream that has bookmarked and then gone
//     silent, since only its silence is evidence.
//   - **A healthy stream has no next attempt.** NextRetryAt is set only while a run is
//     down, where a pass always has one scheduled.
//   - **Flapping hides at an instant.** A watch that reconnects every thirty seconds reads
//     Watching whenever it is read; Restarts is the only field that says otherwise.
type KindState struct {
	Reason  string
	Message string
	// Failure is a SyncFailed run's class of failure, ReasonUnknown when the failure is not a
	// request's or the kind is not failing.
	Failure kubeconn.Reason
	// SinceAt is when Reason last moved — "watching since 10:02", which is what a stream
	// has instead of a last-attempt stamp.
	SinceAt time.Time

	// Live reports that the stream is UP right now, which is what the supervisor means by a
	// worker being live. Kept rather than folded away because it is the one join a consumer
	// cannot redo: reconstructing it means enumerating which reasons a running stream reports,
	// and that set is this package's to change. Stale is live — the run is up and serving its
	// rows while its watch is silent — and Syncing is not, since the cold list is still running.
	Live bool

	// LastUpdateAt is when data last arrived; LastLiveAt the last proof the stream is
	// live, which is the later of the two and the only one that distinguishes idle from
	// wedged.
	//
	// No row count: kind_counts is trigger-maintained, so one here would be a stale copy
	// of a number the database keeps authoritatively, bought with a query per commit. A
	// consumer reads Store.Kinds, which joins every kind's count in one go.
	LastUpdateAt time.Time
	LastLiveAt   time.Time

	// Restarts counts how many times the stream has come back within the current healthy
	// stretch — the flapping question the retry streak cannot answer, since a watch that
	// rotates every thirty seconds never fails. The streak itself is readable as a non-zero
	// NextRetryAt, which is when a run that is DOWN will be tried again and zero while one is
	// up.
	Restarts    int
	NextRetryAt time.Time
}

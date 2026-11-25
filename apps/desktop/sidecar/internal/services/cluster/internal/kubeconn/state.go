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

// What the probes read about the server behind one context: the observations, the identity they
// add up to, and this package's reason vocabulary. Pure data — nothing here reaches the pool or
// the kubeconfig.
package kubeconn

import (
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
)

// The run bookkeeping is the supervisor's, aliased rather than copied so an Observation carries
// exactly what the supervisor recorded. Reason stays this package's vocabulary — the supervisor treats
// it as opaque and owns only Succeeded, DependencyFailed, and Internal, the same words with the
// same values as the constants below.
type (
	Reason   = supervisor.Reason
	Verdict  = supervisor.Verdict
	Attempt  = supervisor.Attempt
	Attempts = supervisor.Attempts
)

const (
	VerdictNone      = supervisor.VerdictNone
	VerdictSucceeded = supervisor.VerdictSucceeded
	VerdictFailed    = supervisor.VerdictFailed
	VerdictSuspended = supervisor.VerdictSuspended
)

// The reasons classify the most recent attempt at one probe. Ours, in the style of a Kubernetes
// condition reason: CamelCase, stable, safe to switch on. Free-form detail belongs in Message.
//
// **The set spans layers on purpose.** A probe dies at the transport, in an API response, or on
// a rule of ours, and a caller asks why once — so a TLS failure and a 403 and a skipped probe
// are one vocabulary. Some names match metav1.StatusReason because that is the same word for
// the same thing, not because this is that set: it has no word for a certificate we rejected.
//
// Assigned when the attempt completes, because the classification is not recoverable later:
// Err arrives wrapped, and does not survive the trip to a caller that only sees a copy.
//
// **Classify from the typed error where there is one.** Dynamic returns *apierrors.StatusError,
// which carries the API's own reason; only the raw endpoints (/readyz, /version) leave a status
// code as the sole evidence. One switch over status codes for both would discard what the typed
// half already knows.
const (
	// ReasonUnknown is the zero value: no attempt has completed.
	ReasonUnknown Reason = ""
	// ReasonSucceeded means the attempt returned usable data.
	ReasonSucceeded Reason = "Succeeded"

	// ReasonUnreachable is DNS failure, connection refused, or no route — nothing answered.
	ReasonUnreachable Reason = "Unreachable"
	// ReasonTLSInvalid means the server answered and its certificate was rejected.
	ReasonTLSInvalid Reason = "TLSInvalid"
	// ReasonTimeout means the probe's own deadline expired. News about the cluster — the
	// deadline was ours, and the caller is still waiting. A run ending because the caller went
	// away records nothing at all.
	ReasonTimeout Reason = "Timeout"

	// ReasonContextNotFound means the kubeconfig stopped naming the context, so there is
	// nothing to reach. The user's own edit, not a fault: the probe suspends with no failure
	// streak, and the file naming the context again is what re-arms it.
	ReasonContextNotFound Reason = "ContextNotFound"
	// ReasonResolveFailed means the file names the context and will not yield credentials from
	// it — a missing cluster entry, a CA file that will not open. Nothing was dialed. Told
	// apart from ReasonContextNotFound because the remedy is to fix the file rather than to
	// accept a context that is gone.
	ReasonResolveFailed Reason = "ResolveFailed"

	// ReasonUnauthorized is a 401: credentials absent, malformed, or expired.
	ReasonUnauthorized Reason = "Unauthorized"
	// ReasonForbidden is a 403: authenticated, and not permitted. A grant to fix rather than
	// an outage to wait out, which is why callers render it differently.
	ReasonForbidden Reason = "Forbidden"

	// ReasonNotFound means the object a probe asked for is absent, such as kube-system.
	// News about the cluster, and a probe that keeps running.
	ReasonNotFound Reason = "NotFound"
	// ReasonUnsupported means the endpoint itself is absent — SelfSubjectReview before v1.27,
	// or a managed distribution withholding /readyz. Terminal: the probe suspends.
	//
	// The trap: both arrive as a 404, and only the probe knows which it asked for. Classifying
	// on the status code alone suspends a probe that should have kept running.
	ReasonUnsupported Reason = "Unsupported"
	// ReasonInternalError is a 500: the API server hit a fault serving the request.
	ReasonInternalError Reason = "InternalError"
	// ReasonServiceUnavailable is a 503, the routine one — a control plane restarting or
	// mid-upgrade. Told apart from ReasonInternalError because it is expected and passes on
	// its own, which is what a caller renders differently and what a backoff waits out.
	ReasonServiceUnavailable Reason = "ServiceUnavailable"
	// ReasonThrottled is a 429, or a client-side rate limiter delay past the probe timeout.
	ReasonThrottled Reason = "Throttled"
	// ReasonMalformed means a response arrived and would not parse, which usually means a
	// proxy or captive portal between us and the API server.
	ReasonMalformed Reason = "Malformed"

	// ReasonComponentsFailing means /readyz answered and named components not ok. The probe
	// worked and the cluster is not fit to use; ComponentStatus.Failing says which.
	ReasonComponentsFailing Reason = "ComponentsFailing"
	// ReasonDependencyFailed means the probe was recorded rather than attempted, because the
	// connection it needs was down. The supervisor records it once per outage, which is what keeps
	// a dead cluster costing one timeout per cycle instead of one per probe.
	ReasonDependencyFailed Reason = "DependencyFailed"
	// ReasonInternal is a bug here — a request we could not build, or a probe that panicked.
	ReasonInternal Reason = "Internal"
)

// Observation is the supervisor's record of one probe: the value beside the bookkeeping for the
// probe that read it. Aliased for the same reason Attempts is. What this package's probes add:
//
// **A zero NextAttempt means the probe is suspended** — nothing is due, and the last answer
// stands. The four probes behind the connection are suspended while it is down, since a server
// nothing reached cannot answer them, and re-armed when it recovers; a probe whose last attempt
// was ReasonUnsupported stays suspended for the life of the connection, because the endpoint is
// absent rather than failing. So "ready, as of 10:00, nothing due" is a state a reader renders,
// not one it treats as stalled.
//
// **Why it is suspended is LastAttempt.Reason**, which needs no field of its own because a probe
// suspends over what its last attempt found.
type Observation[T any] = supervisor.JobObservation[T]

// ComponentStatus is what /readyz named when it answered. Empty on a server that is ready.
type ComponentStatus struct {
	Failing []string // components not reporting ok
}

// VersionInfo is the API server's reported version.
type VersionInfo struct {
	GitVersion string // "v1.31.4"
	Major      string
	Minor      string // carries a "+" suffix on some managed distributions
}

// Principal is who these credentials authenticate as, per SelfSubjectReview.
type Principal struct {
	Username string
	Groups   []string
}

// Identity is the three scalars a connection is scoped to. Comparable on purpose: retiring a
// connection is a == against what the last probe read, and errors are what would break it — so
// why a part is missing stays on the Observation that could not read it.
type Identity struct {
	ServerUID     string
	ServerVersion string
	Username      string
}

// State is what the last pass read about the server behind one context: five probes that
// succeed, fail, and go stale independently. Reachability is the only one the others depend on.
//
// It says nothing about the connection's own life — whether one is built, retiring, or how many
// claims hold it. Connection.Done is where that surfaces.
//
// A value copy, but a shallow one — the slices inside are the probes' and must not be mutated,
// since every watcher holds the same backing array.
type State struct {
	Connection    Observation[string] // the API server URL that answered
	Readiness     Observation[ComponentStatus]
	ServerUID     Observation[string] // kube-system's UID, conventionally the cluster's
	ServerVersion Observation[VersionInfo]
	Principal     Observation[Principal]
}

// Phase is how far the last attempt to reach the server got. The other four probes report
// themselves; this one is separate because they are only worth reading once it is PhaseProbed.
type Phase int

const (
	// PhasePending means nothing has completed yet.
	PhasePending Phase = iota
	// PhaseUnreached means the last attempt did not reach the server.
	PhaseUnreached
	// PhaseProbed means it did.
	PhaseProbed
)

// Phase reports how far the last attempt got.
func (s State) Phase() Phase {
	switch {
	case !s.Connection.LastAttempt.Done():
		return PhasePending
	case s.Connection.OK():
		return PhaseProbed
	default:
		return PhaseUnreached
	}
}

// Identity is the scope built from the probes that have answered. A part no probe could read
// is empty, which is how it compares equal to another connection missing the same part.
func (s State) Identity() Identity {
	return Identity{
		ServerUID:     s.ServerUID.Value,
		ServerVersion: s.ServerVersion.Value.GitVersion,
		Username:      s.Principal.Value.Username,
	}
}

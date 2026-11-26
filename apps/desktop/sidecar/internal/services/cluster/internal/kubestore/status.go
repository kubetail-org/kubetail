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

package kubestore

import (
	"database/sql"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The only kind-specific code in the write path, feeding the materialized objects columns
// (status_summary, ready/total/restart, host). Those exist to make the cache QUERYABLE in
// SQL ("which pods are not ready") without unpacking a blob per row — the dashboard does
// NOT render them; it derives cells client-side from the verbatim body. Being wrong here
// is a query nuance, never correctness: every column is nullable, the body is
// authoritative, and an unknown kind falls back to the conditions convention.
//
// It runs per object write across a cache's hundred-plus workers, so it reads through
// NestedFieldNoCopy rather than the deep-copying Nested{Slice,Map} helpers, and mutates
// nothing it reads.

// statusReading is one object's status projection. A struct, not five return values, since
// most readings produce only a summary. The counts are NullInt64: the columns are nullable
// (a count that doesn't apply must stay NULL, not read as "none ready") and a value type
// keeps the per-object path allocation-free.
type statusReading struct {
	Summary string
	Ready   sql.NullInt64
	Total   sql.NullInt64
	Restart sql.NullInt64
	Host    string
}

// conditionTrue and conditionFalse are a condition's status values. Spelled out here because
// importing them from k8s.io/api links every API group into the binary.
const (
	conditionTrue  = "True"
	conditionFalse = "False"
)

func count(n int64) sql.NullInt64 { return sql.NullInt64{Int64: n, Valid: true} }

// summaryOnly is the reading for a kind whose status has no count worth materializing.
func summaryOnly(summary string) statusReading { return statusReading{Summary: summary} }

// readyOverTotal is the workload kinds' shape: a "verdict x/y" summary plus its counts.
func readyOverTotal(verdict string, ready, total int64) statusReading {
	return statusReading{
		Summary: verdict + " " + strconv.FormatInt(ready, 10) + "/" + strconv.FormatInt(total, 10),
		Ready:   count(ready),
		Total:   count(total),
	}
}

// boolReading is the shape for one-bit readiness (Node, PVC), counted 0-or-1 of 1 so one
// "not ready" query spans them and the workload kinds.
func boolReading(summary string, ok bool) statusReading {
	ready := int64(0)
	if ok {
		ready = 1
	}
	return statusReading{Summary: summary, Ready: count(ready), Total: count(1)}
}

// statusReaders maps a built-in's identity to its reading, keyed on api GROUP and Kind —
// never Kind alone: a CRD named "Pod" in its own group is that user's kind and must reach
// the conditions fallback, not a built-in reader hunting fields it lacks.
var statusReaders = map[struct{ group, kind string }]func(*unstructured.Unstructured) statusReading{
	{"", "Pod"}:                   statusPod,
	{"", "Service"}:               statusService,
	{"", "Node"}:                  statusNode,
	{"", "Namespace"}:             statusNamespace,
	{"", "PersistentVolumeClaim"}: statusPVC,
	{"apps", "Deployment"}:        statusReplicaController,
	{"apps", "StatefulSet"}:       statusReplicaController,
	{"apps", "ReplicaSet"}:        statusReplicaController,
	{"apps", "DaemonSet"}:         statusDaemonSet,
	{"batch", "Job"}:              statusJob,
	{"batch", "CronJob"}:          statusCronJob,
}

// extractStatus projects one object's status, falling back for a kind with no reader
// (most CRDs) to the conventional .status.conditions[] form. The identity comes from the
// BODY, so how a collection was addressed can't change how its objects are read.
func extractStatus(u *unstructured.Unstructured) statusReading {
	if read, ok := statusReaders[struct{ group, kind string }{apiGroup(u.GetAPIVersion()), u.GetKind()}]; ok {
		return read(u)
	}
	return summaryOnly(statusFromConditions(u))
}

// apiGroup returns the group half of an apiVersion ("apps/v1" → "apps"; core "v1" → "").
func apiGroup(apiVersion string) string {
	group, _, ok := strings.Cut(apiVersion, "/")
	if !ok {
		return ""
	}
	return group
}

// nestedSlice reads a slice without apimachinery's deep copy; callers only read scalars
// out of the elements, so the shared view is safe.
func nestedSlice(obj map[string]any, fields ...string) []any {
	v, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return nil
	}
	s, _ := v.([]any)
	return s
}

// containerScan is what one list of container statuses contributes to a pod's reading.
type containerScan struct {
	ready    int64
	total    int64
	restarts int64
	// reason is the first waiting/terminated reason found, which the summary surfaces —
	// "CrashLoopBackOff" beats "Running" for a pod whose only container is crashing.
	reason string
}

// scanContainers reads one containerStatuses-shaped list, shared by the app and init
// passes so the waiting-then-terminated-unless-Completed precedence can't drift.
func scanContainers(list []any) containerScan {
	out := containerScan{total: int64(len(list))}
	for _, c := range list {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if r, _, _ := unstructured.NestedBool(m, "ready"); r {
			out.ready++
		}
		if n, _, _ := unstructured.NestedInt64(m, "restartCount"); n > 0 {
			out.restarts += n
		}
		if out.reason != "" {
			continue
		}
		// state carries at most one of waiting/running/terminated, so one lookup serves
		// both probes. A terminated "Completed" is a normal exit, not a reason.
		state, _, _ := unstructured.NestedFieldNoCopy(m, "state")
		sm, _ := state.(map[string]any)
		if reason, _, _ := unstructured.NestedString(sm, "waiting", "reason"); reason != "" {
			out.reason = reason
		} else if reason, _, _ := unstructured.NestedString(sm, "terminated", "reason"); reason != "" && reason != "Completed" {
			out.reason = reason
		}
	}
	return out
}

func statusPod(u *unstructured.Unstructured) statusReading {
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	host, _, _ := unstructured.NestedString(u.Object, "spec", "nodeName")

	app := scanContainers(nestedSlice(u.Object, "status", "containerStatuses"))
	// A pod blocked in init reports its real reason here while containerStatuses still
	// looks Pending, so an init reason wins and is prefixed "Init:" to match kubectl.
	init := scanContainers(nestedSlice(u.Object, "status", "initContainerStatuses"))

	worstReason := app.reason
	if init.reason != "" {
		worstReason = "Init:" + init.reason
	}
	summary := phase
	if worstReason != "" && phase != "Succeeded" {
		summary = worstReason
	}
	return statusReading{
		Summary: summary,
		Ready:   count(app.ready),
		Total:   count(app.total),
		Restart: count(app.restarts + init.restarts),
		Host:    host,
	}
}

func statusReplicaController(u *unstructured.Unstructured) statusReading {
	// spec.replicas, not status.replicas: the observed count lags during scale-up, so a
	// desired-3 workload would read "ScaledToZero 0/0". Omitted defaults to 1.
	desired, found, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
	if !found {
		desired = 1
	}
	ready, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")

	verdict := "Progressing"
	switch {
	case desired == 0:
		verdict = "ScaledToZero"
	case ready == desired:
		verdict = "Available"
	}
	return readyOverTotal(verdict, ready, desired)
}

func statusDaemonSet(u *unstructured.Unstructured) statusReading {
	desired, _, _ := unstructured.NestedInt64(u.Object, "status", "desiredNumberScheduled")
	ready, _, _ := unstructured.NestedInt64(u.Object, "status", "numberReady")
	verdict := "Progressing"
	if ready == desired && desired > 0 {
		verdict = "Available"
	}
	return readyOverTotal(verdict, ready, desired)
}

func statusNode(u *unstructured.Unstructured) statusReading {
	isReady := false
	var pressure string
	for _, c := range nestedSlice(u.Object, "status", "conditions") {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(m, "type")
		s, _, _ := unstructured.NestedString(m, "status")
		switch t {
		case "Ready":
			isReady = s == conditionTrue
		case "DiskPressure", "MemoryPressure", "PIDPressure", "NetworkUnavailable":
			if s == conditionTrue {
				pressure = t
			}
		}
	}
	summary := "NotReady"
	if isReady {
		summary = "Ready"
	}
	if pressure != "" {
		summary += " (" + pressure + ")"
	}
	return boolReading(summary, isReady)
}

func statusService(u *unstructured.Unstructured) statusReading {
	t, _, _ := unstructured.NestedString(u.Object, "spec", "type")
	ip, _, _ := unstructured.NestedString(u.Object, "spec", "clusterIP")
	if t == "" {
		t = "ClusterIP"
	}
	return summaryOnly(t + " " + ip)
}

func statusNamespace(u *unstructured.Unstructured) statusReading {
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	return summaryOnly(phase)
}

func statusPVC(u *unstructured.Unstructured) statusReading {
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
	return boolReading(phase, phase == "Bound")
}

func statusJob(u *unstructured.Unstructured) statusReading {
	succeeded, _, _ := unstructured.NestedInt64(u.Object, "status", "succeeded")
	failed, _, _ := unstructured.NestedInt64(u.Object, "status", "failed")
	for _, c := range nestedSlice(u.Object, "status", "conditions") {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(m, "type")
		s, _, _ := unstructured.NestedString(m, "status")
		if s != conditionTrue {
			continue
		}
		switch t {
		case "Complete":
			return boolReading("Complete", true)
		case "Failed":
			return boolReading("Failed ("+strconv.FormatInt(failed, 10)+")", false)
		}
	}
	return boolReading("Running ("+strconv.FormatInt(succeeded, 10)+" succeeded)", false)
}

func statusCronJob(u *unstructured.Unstructured) statusReading {
	active := strconv.Itoa(len(nestedSlice(u.Object, "status", "active")))
	last, _, _ := unstructured.NestedString(u.Object, "status", "lastScheduleTime")
	if last != "" {
		return summaryOnly(active + " active, last " + last)
	}
	return summaryOnly(active + " active")
}

// statusFromConditions is the CRD fallback: the type of the most-recently-True condition,
// else the first False one suffixed with its reason (e.g. "Synced (OutOfSync)").
func statusFromConditions(u *unstructured.Unstructured) string {
	var lastTrue, lastTrueAt, lastFalseType, lastFalseReason string
	haveTrue := false
	for _, c := range nestedSlice(u.Object, "status", "conditions") {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(m, "type")
		s, _, _ := unstructured.NestedString(m, "status")
		switch s {
		case conditionTrue:
			// The haveTrue guard is load-bearing: a True condition with a missing or
			// malformed lastTransitionTime would never beat the empty lastTrueAt and
			// would be dropped for a False/empty summary. Compared as a STRING —
			// lastTransitionTime is fixed-width UTC RFC3339, which orders
			// lexicographically, and this runs per condition per object.
			lt, _, _ := unstructured.NestedString(m, "lastTransitionTime")
			if !haveTrue || lt > lastTrueAt {
				haveTrue = true
				lastTrueAt = lt
				lastTrue = t
			}
		case conditionFalse:
			if lastFalseType == "" {
				lastFalseType = t
				lastFalseReason, _, _ = unstructured.NestedString(m, "reason")
			}
		}
	}
	switch {
	case lastTrue != "":
		return lastTrue
	case lastFalseType != "" && lastFalseReason != "":
		return lastFalseType + " (" + lastFalseReason + ")"
	default:
		return lastFalseType
	}
}

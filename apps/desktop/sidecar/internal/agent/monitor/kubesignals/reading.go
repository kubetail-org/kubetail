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

package kubesignals

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/monitor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// The kinds the signals require, as the cache's health names them.
var (
	kindPod         = cluster.KindRef{APIVersion: "v1", Resource: "pods"}
	kindNode        = cluster.KindRef{APIVersion: "v1", Resource: "nodes"}
	kindDeployment  = cluster.KindRef{APIVersion: "apps/v1", Resource: "deployments"}
	kindStatefulSet = cluster.KindRef{APIVersion: "apps/v1", Resource: "statefulsets"}
	kindDaemonSet   = cluster.KindRef{APIVersion: "apps/v1", Resource: "daemonsets"}
	kindReplicaSet  = cluster.KindRef{APIVersion: "apps/v1", Resource: "replicasets"}
)

// statement is one statement of a reading and the signals its answer judges. A
// signal is judged only while every kind it requires is watching; the owner
// walk reads more kinds, but one that is not watching only moves a subject.
type statement struct {
	sql      string
	signals  []string
	requires []cluster.KindRef
	observe  func(rows [][]any) []monitor.Observation
}

var workloadKinds = []cluster.KindRef{kindDeployment, kindStatefulSet, kindDaemonSet, kindReplicaSet, kindPod}

var statements = []statement{
	{stmtCrashLoop, []string{SignalCrashLoop}, []cluster.KindRef{kindPod}, func(rows [][]any) []monitor.Observation {
		return podRows(rows, SignalCrashLoop, "pod", "node", "container", "image", "restarts", "lastReason", "lastExitCode")
	}},
	{stmtImagePull, []string{SignalImagePull}, []cluster.KindRef{kindPod}, func(rows [][]any) []monitor.Observation {
		return podRows(rows, SignalImagePull, "pod", "node", "container", "image", "reason")
	}},
	{stmtPodPending, []string{SignalPodPending}, []cluster.KindRef{kindPod}, func(rows [][]any) []monitor.Observation {
		return podRows(rows, SignalPodPending, "pod", "node", "reason", "message")
	}},
	{stmtNodeNotReady, []string{SignalNodeNotReady}, []cluster.KindRef{kindNode}, observeNodes},
	{stmtWorkloads, []string{SignalReplicasUnavailable, SignalRolloutStalled}, workloadKinds, observeWorkloads},
}

// read is one reading of a cache q admitted, which reaches the server uid: each
// statement whose kinds all watch, and each signal of the rest unknown with the
// code of its first kind that does not.
func read(q cluster.CacheReading, uid string) (monitor.Reading, error) {
	r := monitor.Reading{UID: uid, Unknown: map[string]monitor.ReadErrorCode{}}
	health := q.Health()
	for _, st := range statements {
		code, judged := judge(health, st.requires)
		if judged {
			res, err := q.Query(st.sql)
			if err != nil {
				return monitor.Reading{}, fmt.Errorf("read %s: %w", st.signals[0], err)
			}
			if !res.More {
				r.Observations = append(r.Observations, st.observe(res.Rows)...)
				continue
			}
			code = monitor.CodeTruncated
		}
		for _, s := range st.signals {
			r.Unknown[s] = code
		}
	}
	return r, nil
}

// judge is whether every kind of requires is watching, and the code of the first
// that is not.
func judge(h cluster.ClusterCacheHealth, requires []cluster.KindRef) (monitor.ReadErrorCode, bool) {
	// A cache whose cluster sync is off reads Paused and lists no kind.
	if h.Reason == cluster.ReasonPaused && len(h.WatchingKindRefs) == 0 {
		return monitor.CodeDisabled, false
	}
	for _, k := range requires {
		if code, ok := kindCode(h, k); !ok {
			return code, false
		}
	}
	return "", true
}

// kindCode is a kind's code: true while it watches, disabled while paused, its
// state's code while behind, and not_ready while discovery has yet to find it.
func kindCode(h cluster.ClusterCacheHealth, k cluster.KindRef) (monitor.ReadErrorCode, bool) {
	if slices.Contains(h.WatchingKindRefs, k) {
		return "", true
	}
	if slices.Contains(h.PausedKindRefs, k) {
		return monitor.CodeDisabled, false
	}
	for _, u := range h.UnhealthyKindRefs {
		if u.KindRef == k {
			return stateCode(u), false
		}
	}
	return monitor.CodeNotReady, false
}

// stateCode is the code of a kind that is not watching, by its state.
func stateCode(u cluster.UnhealthyKindRef) monitor.ReadErrorCode {
	switch u.Reason {
	case cluster.KindReasonSyncing, cluster.KindReasonResuming, cluster.KindReasonResyncing:
		return monitor.CodeNotReady
	case cluster.KindReasonSyncFailed:
		switch u.Failure {
		case cluster.FailureForbidden:
			return monitor.CodePermissionDenied
		case cluster.FailureUnauthorized:
			return monitor.CodeUnauthenticated
		}
	}
	return monitor.CodeUnavailable
}

// podRows groups a Pod signal's rows by subject into one observation each: its
// detail the first row's, keys naming the columns from the sixth on, and pods how
// many Pods hold the signal.
func podRows(rows [][]any, signal string, keys ...string) []monitor.Observation {
	var out []monitor.Observation
	for i := 0; i < len(rows); {
		first := rows[i]
		pods := map[string]bool{}
		j := i
		for ; j < len(rows) && sameSubject(rows[j], first); j++ {
			pods[text(rows[j][4])] = true
		}
		pairs := make([]any, 0, 2*len(keys))
		for k, key := range keys {
			pairs = append(pairs, key, first[5+k])
		}
		d := details(pairs...)
		d["pods"] = strconv.Itoa(len(pods))
		out = append(out, monitor.Observation{Signal: signal, Subject: subjectOf(text(first[0]), text(first[1]), text(first[2]), text(first[3])), Detail: d})
		i = j
	}
	return out
}

// sameSubject is whether two rows of a Pod signal open with one subject.
func sameSubject(a, b []any) bool {
	return text(a[0]) == text(b[0]) && text(a[1]) == text(b[1]) && text(a[2]) == text(b[2]) && text(a[3]) == text(b[3])
}

// observeNodes is an observation per row of stmtNodeNotReady.
func observeNodes(rows [][]any) []monitor.Observation {
	out := make([]monitor.Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, monitor.Observation{
			Signal: SignalNodeNotReady, Subject: subjectOf("v1", "Node", "", text(r[0])),
			Detail: details("status", r[1], "reason", r[2], "message", r[3]),
		})
	}
	return out
}

// observeWorkloads is an observation per row of stmtWorkloads: rollout_stalled
// for a workload that is rolling, replicas_unavailable for the rest.
func observeWorkloads(rows [][]any) []monitor.Observation {
	out := make([]monitor.Observation, 0, len(rows))
	for _, r := range rows {
		o := monitor.Observation{Signal: SignalReplicasUnavailable, Subject: subjectOf("apps/v1", text(r[0]), text(r[1]), text(r[2]))}
		if cluster.CellInteger(r[6]) != 0 {
			o.Signal, o.Detail = SignalRolloutStalled, details("ready", r[3], "desired", r[4], "updated", r[5])
		} else {
			o.Detail = details("ready", r[3], "desired", r[4])
		}
		out = append(out, o)
	}
	return out
}

// subjectOf is the subject an object names: its key the group, kind, namespace
// and name joined with /, which no Kubernetes name holds.
func subjectOf(apiVersion, kind, namespace, name string) monitor.Subject {
	group := ""
	if g, _, ok := strings.Cut(apiVersion, "/"); ok {
		group = g
	}
	label := kind + " " + name
	if namespace != "" {
		label = kind + " " + namespace + "/" + name
	}
	return monitor.Subject{
		Key:   strings.Join([]string{group, kind, namespace, name}, "/"),
		Label: cutLabel(label),
		Attributes: map[string]string{
			"apiGroup": cutAttribute(group), "kind": cutAttribute(kind),
			"namespace": cutAttribute(namespace), "name": cutAttribute(name),
		},
	}
}

// details is a detail of key and cell pairs, each value cut, leaving out a value
// the cluster did not give.
func details(pairs ...any) map[string]string {
	d := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if v := text(pairs[i+1]); v != "" {
			d[pairs[i].(string)] = cutDetail(v)
		}
	}
	return d
}

// text is a cell as text: a string as it is, an integer in decimal, "" for NULL.
func text(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}

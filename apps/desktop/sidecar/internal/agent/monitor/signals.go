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
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// Signal is one failing state the monitor detects.
type Signal string

const (
	CrashLoopBackOff    Signal = "CrashLoopBackOff"
	ImagePullBackOff    Signal = "ImagePullBackOff"
	PodPending          Signal = "PodPending"
	NodeNotReady        Signal = "NodeNotReady"
	ReplicasUnavailable Signal = "ReplicasUnavailable"
)

var signals = []Signal{CrashLoopBackOff, ImagePullBackOff, PodPending, NodeNotReady, ReplicasUnavailable}

// Subject is what a signal is about, named so a recreated object keeps its name.
type Subject struct {
	Group, Kind, Namespace, Name string
}

// Pair is one signal true of one subject.
type Pair struct {
	Subject Subject
	Signal  Signal
}

var (
	pods         = cluster.KindRef{APIVersion: "v1", Resource: "pods"}
	nodes        = cluster.KindRef{APIVersion: "v1", Resource: "nodes"}
	replicaSets  = cluster.KindRef{APIVersion: "apps/v1", Resource: "replicasets"}
	deployments  = cluster.KindRef{APIVersion: "apps/v1", Resource: "deployments"}
	statefulSets = cluster.KindRef{APIVersion: "apps/v1", Resource: "statefulsets"}
	daemonSets   = cluster.KindRef{APIVersion: "apps/v1", Resource: "daemonsets"}
)

// kindsRead is the kinds whose rows decide each signal. A pod signal's owner walk
// reads more, but an owner kind not Watching only moves its subject, as a custom
// controller's does, so it does not hold back every pod signal: a user who may not
// list CronJobs still hears of a crash loop.
var kindsRead = map[Signal][]cluster.KindRef{
	CrashLoopBackOff:    {pods},
	ImagePullBackOff:    {pods},
	PodPending:          {pods},
	NodeNotReady:        {nodes},
	ReplicasUnavailable: {deployments, statefulSets, daemonSets, replicaSets, pods},
}

// judgedBy is the signals health lets a reading judge: those whose every kind is
// Watching. A paused kind keeps its rows, which may be old.
func judgedBy(health cluster.ClusterCacheHealth) map[Signal]bool {
	judged := map[Signal]bool{}
	// A cache whose sync is off is frozen whole and lists no kind.
	if health.Reason == cluster.ReasonPaused {
		return judged
	}
	notWatching := slices.Clone(health.PausedKindRefs)
	for _, ref := range health.UnhealthyKindRefs {
		notWatching = append(notWatching, ref.KindRef)
	}
	for _, signal := range signals {
		reads := func(ref cluster.KindRef) bool { return slices.Contains(kindsRead[signal], ref) }
		if !slices.ContainsFunc(notWatching, reads) {
			judged[signal] = true
		}
	}
	return judged
}

// reading is what one reading of one cluster found: the pairs true now, and the
// signals it could judge. A pair whose signal is not judged is not in pairs.
type reading struct {
	pairs  map[Pair]bool
	judged map[Signal]bool
	// rolling is the pairs of workloads mid-rollout, which wait a longer window.
	rolling map[Pair]bool
	// server is the UID of the server the cache mirrors.
	server string
}

// reader reads a cluster's cache, each statement cut at maxRows and maxBytes.
type reader struct {
	data     cluster.CachedData
	maxRows  int
	maxBytes int
}

func newReader(data cluster.CachedData, maxRows, maxBytes int) reader {
	return reader{data: data, maxRows: maxRows, maxBytes: maxBytes}
}

// read is one reading of cluster id. ok is false for a cache that answers no trusted
// rows, which judges nothing.
func (r reader) read(ctx context.Context, id cluster.ClusterID) (reading, bool, error) {
	var out reading
	err := r.data.ReadActive(ctx, id, r.maxRows, r.maxBytes, func(q cluster.CacheReading, active cluster.ActiveCluster) error {
		// ReadActive may call this twice, so each call sets out whole.
		var err error
		out, err = signalsIn(q)
		out.server = active.Cache.Spec.ServerUID
		return err
	})
	if errors.Is(err, cluster.ErrNoCache) || errors.Is(err, cluster.ErrSyncing) {
		return reading{}, false, nil
	}
	if err != nil {
		return reading{}, false, err
	}
	return out, true, nil
}

// signalsIn runs every statement over one snapshot of the cache. A cut answer would
// read as signals gone, so its signals are not judged.
func signalsIn(q cluster.CacheReading) (reading, error) {
	waiting, err := readPods(q, stmtWaitingPods)
	if err != nil {
		return reading{}, err
	}
	pending, err := readPods(q, stmtPendingPods)
	if err != nil {
		return reading{}, err
	}
	notReady, nodesCut, err := readNodes(q)
	if err != nil {
		return reading{}, err
	}
	// A workload whose pod holds a pod signal is that signal's subject already.
	ownsFailingPod := func(uid string) bool { return waiting.owners[uid] || pending.owners[uid] }
	short, err := readWorkloads(q, ownsFailingPod)
	if err != nil {
		return reading{}, err
	}

	judged := judgedBy(q.Health())
	if waiting.cut {
		delete(judged, CrashLoopBackOff)
		delete(judged, ImagePullBackOff)
	}
	if pending.cut {
		delete(judged, PodPending)
	}
	if nodesCut {
		delete(judged, NodeNotReady)
	}
	// A cut pod answer is missing owners, so a short workload may be one of them.
	if short.cut || waiting.cut || pending.cut {
		delete(judged, ReplicasUnavailable)
	}

	found := map[Pair]bool{}
	for _, pairs := range []map[Pair]bool{waiting.pairs, pending.pairs, notReady, short.pairs} {
		maps.Copy(found, pairs)
	}
	maps.DeleteFunc(found, func(p Pair, _ bool) bool { return !judged[p.Signal] })
	return reading{pairs: found, judged: judged, rolling: short.rolling}, nil
}

// readNodes is each Node that is not ready, and whether the answer was cut.
func readNodes(q cluster.CacheReading) (map[Pair]bool, bool, error) {
	res, err := q.Query(stmtNotReadyNodes)
	if err != nil {
		return nil, false, err
	}
	pairs := make(map[Pair]bool, len(res.Rows))
	for _, row := range res.Rows {
		pairs[Pair{Subject: Subject{Kind: "Node", Name: cluster.CellText(row[0])}, Signal: NodeNotReady}] = true
	}
	return pairs, res.More, nil
}

// readWorkloads is each workload short of ready replicas that skip does not name by
// uid, but a Deployment mid-rollout.
func readWorkloads(q cluster.CacheReading, skip func(uid string) bool) (workloadAnswer, error) {
	res, err := q.Query(stmtShortWorkloads)
	if err != nil {
		return workloadAnswer{}, err
	}
	answer := workloadAnswer{pairs: map[Pair]bool{}, rolling: map[Pair]bool{}, cut: res.More}
	for _, row := range res.Rows {
		uid, kind := cluster.CellText(row[0]), cluster.CellText(row[1])
		rolling := rollingOut(kind, cluster.CellText(row[4]), cluster.CellText(row[5]))
		// A Deployment's rollout that stalls passes its progress deadline, which
		// rollingOut reads as no rollout.
		if skip(uid) || rolling && kind == "Deployment" {
			continue
		}
		pair := Pair{Subject: Subject{Group: "apps", Kind: kind, Namespace: cluster.CellText(row[2]), Name: cluster.CellText(row[3])}, Signal: ReplicasUnavailable}
		answer.pairs[pair] = true
		if rolling {
			answer.rolling[pair] = true
		}
	}
	return answer, nil
}

// workloadAnswer is the short workloads a reading found, those mid-rollout among
// them, and whether the answer was cut.
type workloadAnswer struct {
	pairs, rolling map[Pair]bool
	cut            bool
}

// podAnswer is what a pod statement found: a pair per signal on each pod's top
// controller, and the uid of every owner on a walk.
type podAnswer struct {
	pairs  map[Pair]bool
	owners map[string]bool
	cut    bool
}

// readPods runs a pod statement, whose rows are each pod's walk up its controllers,
// one row per step, in ownerWalk's columns.
func readPods(q cluster.CacheReading, statement string, args ...any) (podAnswer, error) {
	res, err := q.Query(statement, args...)
	if err != nil {
		return podAnswer{}, err
	}
	type key struct {
		pod    string
		signal Signal
	}
	type top struct {
		depth   int64
		subject Subject
	}
	tops := map[key]top{}
	owners := map[string]bool{}
	for _, row := range res.Rows {
		text := func(col int) string { return cluster.CellText(row[col]) }
		k := key{pod: text(0), signal: Signal(text(1))}
		depth := cluster.CellInteger(row[2])
		if depth > 0 {
			owners[text(3)] = true
		}
		if t, ok := tops[k]; ok && t.depth >= depth {
			continue
		}
		tops[k] = top{depth: depth, subject: subjectOf(text(4), text(5), text(6), text(7))}
	}
	pairs := make(map[Pair]bool, len(tops))
	for k, t := range tops {
		pairs[Pair{Subject: t.subject, Signal: k.signal}] = true
	}
	return podAnswer{pairs: pairs, owners: owners, cut: res.More}, nil
}

// subjectOf names an object by its group, not its version, so a subject outlives an
// API version's move.
func subjectOf(apiVersion, kind, namespace, name string) Subject {
	group, _, ok := strings.Cut(apiVersion, "/")
	if !ok {
		group = ""
	}
	return Subject{Group: group, Kind: kind, Namespace: namespace, Name: name}
}

type condition struct {
	Type, Status, Reason string
}

// workloadStatus is the part of a workload's status that tells a rollout.
type workloadStatus struct {
	Conditions             []condition `json:"conditions"`
	CurrentRevision        string      `json:"currentRevision"`
	UpdateRevision         string      `json:"updateRevision"`
	UpdatedReplicas        int64       `json:"updatedReplicas"`
	DesiredNumberScheduled int64       `json:"desiredNumberScheduled"`
	UpdatedNumberScheduled int64       `json:"updatedNumberScheduled"`
}

// workloadSpec is the part of a workload's spec that says how far an update goes.
type workloadSpec struct {
	Replicas       *int64 `json:"replicas"`
	UpdateStrategy struct {
		Type          string `json:"type"`
		RollingUpdate struct {
			Partition int64 `json:"partition"`
		} `json:"rollingUpdate"`
	} `json:"updateStrategy"`
}

// rollingOut reports whether a workload of kind, with status and spec, is mid-rollout,
// when fewer ready replicas than desired is expected. A Deployment past its progress
// deadline is not: its Progressing condition is False. Nor is an update that went as
// far as it was meant to: an OnDelete strategy waits on each pod being deleted by
// hand, and a StatefulSet updates only the pods at or above its partition. A status
// that will not parse reads as not rolling out.
func rollingOut(kind, status, spec string) bool {
	var st workloadStatus
	if json.Unmarshal([]byte(status), &st) != nil {
		return false
	}
	var sp workloadSpec
	_ = json.Unmarshal([]byte(spec), &sp) // a spec that will not parse reads as the defaults
	if sp.UpdateStrategy.Type == "OnDelete" {
		return false
	}
	switch kind {
	case "Deployment":
		return slices.ContainsFunc(st.Conditions, func(c condition) bool {
			return c.Type == "Progressing" && c.Status == "True" && c.Reason != "NewReplicaSetAvailable"
		})
	case "StatefulSet":
		replicas := int64(1)
		if sp.Replicas != nil {
			replicas = *sp.Replicas
		}
		return st.UpdateRevision != st.CurrentRevision && st.UpdatedReplicas < replicas-sp.UpdateStrategy.RollingUpdate.Partition
	case "DaemonSet":
		return st.UpdatedNumberScheduled < st.DesiredNumberScheduled
	}
	return false
}

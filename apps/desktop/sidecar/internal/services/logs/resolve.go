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

package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// MaxStreams is the most containers one view reads at once: each is its own
// request to the API server, held open for as long as a tail runs.
const MaxStreams = 100

// The mirror reads' bounds. No statement reads more than MaxStreams plus one rows,
// a view's nodes being no more than its streams, each a few names long, so neither
// bound cuts an answer the statement allows.
const (
	maxRows  = MaxStreams + 1
	maxBytes = 1 << 20
)

// SourceState is what the mirror holds of a source now. A view follows each
// source as its object and pods come and go, so no state refuses it.
type SourceState int

const (
	// Reading is a source with a stream to read.
	Reading SourceState = iota
	// Missing is a source whose object the mirror does not hold yet.
	Missing
	// Idle is a source whose object the mirror holds with nothing to read yet: no
	// pods, none with a log of a container named, none of those with a previous
	// instance, or none on a node the filters keep.
	Idle
)

// The well-known labels a node's place is read from.
const (
	labelRegion = "topology.kubernetes.io/region"
	labelZone   = "topology.kubernetes.io/zone"
	labelOS     = "kubernetes.io/os"
	labelArch   = "kubernetes.io/arch"
)

// target is what a view reads on one cluster: the server the mirror's pods came
// from, one stream per container, in the order the sources were asked, and each
// source's state and its object's UID, by the source's index, the UID empty for one
// the mirror lacks. pods is the UID of each Pod source's pod the mirror holds, by
// "<ns>/<name>", whether or not it has a log to read, defaults its default container,
// places where it runs now, and current the runtime id of each of its containers'
// current instance, by "<ns>/<pod>/<container>". noNodes is a label filter the
// read could not apply for want of nodes, which a page or a tail answers only
// when the file holds nothing of the view either. For a tail, starting is whether a container the view
// reads has no log yet, which the tail waits for.
type target struct {
	cacheID    cluster.ClusterCacheID
	serverUID  string
	streams    []stream
	states     []SourceState
	sourceUIDs []string
	starting   bool
	pods       map[string]string
	defaults   map[string]string
	places     map[string]logline.Place
	noNodes    error
	current    map[string]string
}

// stream is one container a view reads: the log it names, where its pod runs,
// and the generation of that log; and the snapshot its file row keeps, since the
// lines outlive the pod: the pod's labels as a JSON object, whether the container is
// the pod's default, when the instance read started and finished in unix
// nanoseconds, zero when unknown or while it runs.
type stream struct {
	key        logline.StreamKey
	place      logline.Place
	gen        generation
	labels     string
	isDefault  bool
	startedAt  int64
	finishedAt int64
	// previous and previousFinishedAt are the instance before the one read and when it
	// finished, when the stream reads the current one and the container restarted.
	previous           string
	previousFinishedAt int64
	// owners is the UID of each controller above the pod, nearest first.
	owners []string
}

// generation is which log a stream's path serves now: the pod's UID, since a pod
// replaced under the same name has a new one; the runtime id of the instance read,
// since a restart starts a new one, which names the stream in each line's cursor;
// and the container's state, since a running container's log can still grow and
// a finished one's cannot. A tail reopens a stream whose generation moved.
type generation struct {
	uid      string
	instance string
	state    string
}

// finished is whether the instance st reads has stopped, so its log never grows:
// a previous instance, or a container terminated or waiting, whose log is the last
// instance's.
func (st stream) finished() bool {
	return st.key.Previous || st.gen.state == "terminated" || st.gen.state == "waiting"
}

// log is st's log as a backend reads it.
func (st stream) log() kubelet.Log {
	return kubelet.Log{Key: st.key, Place: st.place, Instance: st.gen.instance, Finished: st.finished()}
}

// run is st's container and runtime id, at no moment: what orders its lines
// against another stream's at one moment.
func (st stream) run() logline.Cursor {
	return logline.Cursor{ContainerName: logline.ContainerName{Namespace: st.key.Namespace, Pod: st.key.Pod, Container: st.key.Container}, RunPlace: logline.RunPlace{ContainerID: st.gen.instance}}
}

// resolve reads view over one reading of cluster id's active cache, and for a
// tail whether a container of it is starting. A cache missing or withholding its
// rows is refused as the LogsView tool refuses it, and so is a kind the cache does
// not mirror and a view of more than MaxStreams containers. A resolve pinned to
// serverUID ("" for any) reads no cache of another server: ErrIdentityMismatch.
func resolve(ctx context.Context, svc cluster.Service, id cluster.ClusterID, serverUID string, scope Scope, forTail bool) (target, error) {
	var t target
	err := svc.CachedData().ReadActive(ctx, id, serverUID, maxRows, maxBytes, func(q cluster.CacheReading, active cluster.ActiveCluster) error {
		r := reading{q}
		streams, states, uids, err := r.streamsOf(scope)
		var deferred error
		if _, ok := errors.AsType[refusal.NoNodes](err); ok {
			// The file's rows of a gone pod may still hold the filter's place.
			deferred, err = err, nil
		}
		if err != nil {
			return err
		}
		if err := r.place(streams); err != nil {
			return err
		}
		// After the streams, so a view of too many containers is refused as that.
		pods, current, defaults, places, err := r.podSources(scope)
		if err != nil {
			return err
		}
		starting := false
		if forTail {
			if starting, err = r.starting(scope); err != nil {
				return err
			}
		}
		// Set whole, never appended to: ReadActive may run this twice.
		t = target{cacheID: active.Cache.ID, serverUID: active.Cache.Spec.ServerUID, streams: streams, states: states, sourceUIDs: uids, starting: starting, pods: pods, current: current, defaults: defaults, places: places, noNodes: deferred}
		return nil
	})
	if err != nil {
		return target{}, mirrorErr(err)
	}
	return t, nil
}

// mirrorErr is err, a reading of the mirror's, as the service answers it: a cache
// missing or withholding its rows is refused as the LogsView tool refuses it.
func mirrorErr(err error) error {
	switch {
	case errors.Is(err, cluster.ErrNoCache):
		return ErrNoCache
	case errors.Is(err, cluster.ErrSyncing):
		return ErrSyncing
	}
	return err
}

// serves is whether the pod st reads still runs the instance st was resolved as: the
// pod of its name still the one of its UID, and the instance the kubelet serves for
// st, the current one or with Previous the last, still st's.
func (r reading) serves(st stream) (bool, error) {
	rows, err := r.rows(`SELECT p.uid, coalesce(c.container_id, ''), coalesce(c.last_container_id, '')
FROM objects p JOIN containers c ON c.pod_uid = p.uid
WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND p.namespace = ? AND p.name = ? AND c.name = ?`,
		st.key.Namespace, st.key.Pod, st.key.Container)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	served := cluster.CellText(rows[0][1])
	if st.key.Previous || served == "" {
		served = cluster.CellText(rows[0][2])
	}
	return cluster.CellText(rows[0][0]) == st.gen.uid && served == st.gen.instance, nil
}

// Check is each source's state as resolve would read view over q, refusing the
// view as resolve would. The LogsView tool checks through it, so a view it opens
// is one resolve reads.
func Check(q cluster.CacheReading, scope Scope) ([]SourceState, error) {
	_, states, _, err := reading{q}.streamsOf(scope)
	return states, err
}

// reading is one reading of a cache, queried through its views.
type reading struct{ cluster.CacheReading }

// rows is sql's rows with args bound to its placeholders, refusing an answer cut
// at its bounds: read as every row, it would drop sources, or nodes a filter
// needs, without a word.
func (r reading) rows(sql string, args ...any) ([][]any, error) {
	res, err := r.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	if res.More {
		return nil, errCut
	}
	return res.Rows, nil
}

// streamsOf is every stream of view's sources, each once, in the order asked,
// with the filters applied, and each source's state and UID.
func (r reading) streamsOf(scope Scope) ([]stream, []SourceState, []string, error) {
	if err := checkFilters(scope.Filters); err != nil {
		return nil, nil, nil, err
	}
	mirrored, err := r.mirrors(mirrorKinds[SourcePod])
	if err != nil {
		return nil, nil, nil, err
	}
	if !mirrored {
		return nil, nil, nil, refusal.Unmirrored{Refusal: refusal.New(refusal.CodeNotMirrored, "Kubetail's cache of this cluster holds no pods, so it reads no logs"), Source: -1}
	}
	var streams []stream
	var states []SourceState
	var uids []string
	seen := map[logline.StreamKey]bool{}
	for i, src := range scope.Sources {
		found, state, uid, err := r.streams(i, src, scope.Filters)
		if err != nil {
			return nil, nil, nil, err
		}
		states, uids = append(states, state), append(uids, uid)
		for _, s := range found {
			if !seen[s.key] {
				seen[s.key] = true
				streams = append(streams, s)
			}
		}
		if len(streams) > MaxStreams {
			return nil, nil, nil, ErrTooMany
		}
	}
	if len(streams) == 0 {
		// The UIDs go with NoNodes, which a read defers while the file may hold a
		// workload's gone pods.
		if err := r.nodesListed(scope.Filters); err != nil {
			return nil, nil, uids, err
		}
	}
	return streams, states, uids, nil
}

// checkFilters refuses a filter on a field Kubetail has no label for.
func checkFilters(filters []ScopeFilter) error {
	for _, f := range filters {
		if _, ok := filterLabels[f.Field]; !ok && f.Field != FilterNode {
			return refusal.New(refusal.CodeBadScope, fmt.Sprintf("Kubetail cannot filter by %q", f.Field))
		}
	}
	return nil
}

// nodesListed refuses a filter on a label over a mirror with no nodes: the
// credentials could not list them, and an empty answer would read as no lines.
// Only an empty target asks, since every label filter leaves out every pod then.
func (r reading) nodesListed(filters []ScopeFilter) error {
	var byLabel []string
	for _, f := range filters {
		if f.Field != FilterNode {
			byLabel = append(byLabel, string(f.Field))
		}
	}
	if len(byLabel) == 0 {
		return nil
	}
	rows, err := r.rows(`SELECT n.name FROM objects n WHERE n.api_version = 'v1' AND n.kind = 'Node' LIMIT 1`)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return refusal.NoNodes{Refusal: refusal.New(refusal.CodeNoNodes, "Kubetail cannot read this cluster's nodes, so it cannot filter by "+strings.Join(byLabel, " or "))}
	}
	return nil
}

// mirrors is whether the cache mirrors kind: the kinds view lists what its sync
// serves, with or without an object of it.
func (r reading) mirrors(kind mirrorKind) (bool, error) {
	rows, err := r.rows(`SELECT 1 FROM kinds WHERE api_version = ? AND kind = ?`, kind.apiVersion, kind.kind)
	return len(rows) > 0, err
}

// streams is one stream per container of each of src's pods that the filters
// keep and whose runtime id a line cursor takes, the source at index i, src's
// state and its object's UID. A kind the cache does not mirror
// is refused, since no object of it could ever appear to follow. The statement
// reads at most one row past MaxStreams, which is enough to refusal.New the view.
func (r reading) streams(i int, src ScopeSource, filters []ScopeFilter) ([]stream, SourceState, string, error) {
	kind, err := kindOf(src)
	if err != nil {
		return nil, 0, "", err
	}
	mirrored, err := r.mirrors(kind)
	if err != nil {
		return nil, 0, "", err
	}
	if !mirrored {
		return nil, 0, "", refusal.Unmirrored{Refusal: refusal.New(refusal.CodeNotMirrored, fmt.Sprintf("Kubetail's cache of this cluster holds no %s, so it cannot follow %s/%s in %s", kind.resource, kind.resource, src.Name, src.Namespace)), Source: i}
	}
	uid, err := r.uidOf(src, kind)
	if err != nil {
		return nil, 0, "", err
	}
	sql, args := podsStatement(src, kind, filters)
	rows, err := r.rows(sql, args...)
	if err != nil {
		return nil, 0, "", err
	}
	if len(rows) == 0 {
		if uid == "" {
			return nil, Missing, "", nil
		}
		return nil, Idle, uid, nil
	}
	streams := make([]stream, 0, len(rows))
	for _, row := range rows {
		instance := cluster.CellText(row[4])
		last := src.Previous || instance == ""
		if last {
			instance = cluster.CellText(row[5])
		}
		// The id is the cluster's text, and a page's cursor carries it.
		if !logline.ValidContainerID(instance) {
			continue
		}
		var owners []string
		if err := json.Unmarshal([]byte(cluster.CellText(row[10])), &owners); err != nil { // coverage:ignore SQLite's json_group_array always builds a list
			return nil, 0, "", err
		}
		started, finished := instanceTimes(cluster.CellText(row[9]), last)
		var previous string
		var previousFinished int64
		if !last {
			previous = cluster.CellText(row[5])
			_, previousFinished = instanceTimes(cluster.CellText(row[9]), true)
		}
		streams = append(streams, stream{
			key:                logline.StreamKey{Namespace: src.Namespace, Pod: cluster.CellText(row[0]), Container: cluster.CellText(row[1]), Previous: src.Previous},
			place:              logline.Place{Node: cluster.CellText(row[2])},
			gen:                generation{uid: cluster.CellText(row[3]), instance: instance, state: cluster.CellText(row[6])},
			labels:             cluster.CellText(row[7]),
			isDefault:          cluster.CellInteger(row[8]) == 1,
			startedAt:          started,
			finishedAt:         finished,
			previous:           previous,
			previousFinishedAt: previousFinished,
			owners:             owners,
		})
	}
	if len(streams) == 0 {
		return nil, Idle, uid, nil
	}
	return streams, Reading, uid, nil
}

// uidOf is the UID of src's object, empty when the mirror does not hold it.
func (r reading) uidOf(src ScopeSource, kind mirrorKind) (string, error) {
	rows, err := r.rows(`SELECT uid FROM objects WHERE api_version = ? AND kind = ? AND namespace = ? AND name = ?`, kind.apiVersion, kind.kind, src.Namespace, src.Name)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return cluster.CellText(rows[0][0]), nil
}

// place fills in each stream's node labels, reading only the nodes the streams
// run on.
func (r reading) place(streams []stream) error {
	var names []string
	seen := map[string]bool{}
	for _, s := range streams {
		if s.place.Node != "" && !seen[s.place.Node] {
			seen[s.place.Node] = true
			names = append(names, s.place.Node)
		}
	}
	if len(names) == 0 {
		return nil
	}
	args := append([]any{labelRegion, labelZone, labelOS, labelArch}, asArgs(names)...)
	rows, err := r.rows(`SELECT n.name,
(SELECT value FROM labels l WHERE l.uid = n.uid AND l.key = ?),
(SELECT value FROM labels l WHERE l.uid = n.uid AND l.key = ?),
(SELECT value FROM labels l WHERE l.uid = n.uid AND l.key = ?),
(SELECT value FROM labels l WHERE l.uid = n.uid AND l.key = ?)
FROM objects n WHERE n.api_version = 'v1' AND n.kind = 'Node' AND n.name IN (`+cluster.Placeholders(len(names))+`)`, args...)
	if err != nil {
		return err
	}
	nodes := map[string]logline.Place{}
	for _, row := range rows {
		name := cluster.CellText(row[0])
		nodes[name] = logline.Place{Node: name, Region: cluster.CellText(row[1]), Zone: cluster.CellText(row[2]), OS: cluster.CellText(row[3]), Arch: cluster.CellText(row[4])}
	}
	for i := range streams {
		if p, ok := nodes[streams[i].place.Node]; ok {
			streams[i].place = p
		}
	}
	return nil
}

// podsStatement is the statement of src's pods' containers, each row the pod's
// name, the container's name, the pod's node, the pod's UID, the container's
// runtime id and its last one, its state, the pod's labels, whether it is the pod's
// default container, its status off the pod's body and the controllers above the pod
// as a JSON array of UIDs, nearest first, init containers first, as they run and as
// kubectl lists them. The containers src names, having a log the
// kubelet serves — a previous instance when src asks for one, else a running or
// finished instance, the last one standing in while the container waits, as
// kubectl logs reads it — and the filters narrow it, so the row cap counts only
// what the view reads, and nothing else applies them.
func podsStatement(src ScopeSource, kind mirrorKind, filters []ScopeFilter) (string, []any) {
	where, args := containersWhere(src, kind, filters)
	if src.Previous {
		where += " AND c.last_container_id <> ''"
	} else {
		where += " AND (c.container_id <> '' OR c.last_container_id <> '')"
	}
	return `SELECT p.name, c.name, p.node, p.uid, c.container_id, c.last_container_id, c.state,
coalesce(p.body -> '$.metadata.labels', '{}'),
c.name = ` + defaultContainer + `,
(SELECT s.value FROM json_each(p.body,
	CASE c.init WHEN 1 THEN '$.status.initContainerStatuses' ELSE '$.status.containerStatuses' END) s
WHERE s.value ->> '$.name' = c.name),
` + podOwners + `
FROM objects p JOIN containers c ON c.pod_uid = p.uid
WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND ` + where + fmt.Sprintf(` ORDER BY p.name, c.init DESC, c.position LIMIT %d`, MaxStreams+1), args
}

// podOwners is the controllers above the pod p as a JSON array of UIDs, nearest
// first: the walk podsOf makes down, read up, to the same depth.
const podOwners = `(SELECT json_group_array(uid) FROM (WITH RECURSIVE up(uid, depth) AS (
SELECT r.owner_uid, 1 FROM owners r WHERE r.uid = p.uid AND r.controller = 1
UNION ALL
SELECT r.owner_uid, up.depth + 1 FROM up JOIN owners r ON r.uid = up.uid WHERE r.controller = 1 AND up.depth < 8
) SELECT uid FROM up ORDER BY depth))`

// containersWhere is the condition on the container c of the pod p that src reads
// it and the filters keep it, with the args it binds.
func containersWhere(src ScopeSource, kind mirrorKind, filters []ScopeFilter) (string, []any) {
	pods, args := podsOf(src, kind)
	containers, containerArgs := containersOf(src)
	where := pods + containers
	args = append(args, containerArgs...)
	for _, f := range filters {
		cond, condArgs := filterWhere(f)
		where += " AND " + cond
		args = append(args, condArgs...)
	}
	return where, args
}

// starting is whether a container view reads has no log yet: its pod not yet
// started, or the container waiting for its first instance. A previous instance
// never starts.
func (r reading) starting(scope Scope) (bool, error) {
	for _, src := range scope.Sources {
		if src.Previous {
			continue
		}
		kind, err := kindOf(src)
		if err != nil { // coverage:ignore streamsOf refused an unknown kind before resolve asks
			return false, err
		}
		where, args := containersWhere(src, kind, scope.Filters)
		rows, err := r.rows(`SELECT 1 FROM objects p JOIN containers c ON c.pod_uid = p.uid
WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND `+where+` AND coalesce(c.container_id, '') = '' AND coalesce(c.last_container_id, '') = '' LIMIT 1`, args...)
		if err != nil || len(rows) > 0 {
			return len(rows) > 0, err
		}
	}
	return false, nil
}

// podSources is the UID, the default container and the place of each of view's Pod
// sources' pods the mirror holds, by "<ns>/<name>", and the runtime id of the current
// instance of each of their containers, by "<ns>/<pod>/<container>": the one a read
// with no Previous serves, so the instance before while the container waits. Each pod is one
// row, its containers folded into it, so what a pod holds never counts against the
// row cap, which bounds what a view selects.
func (r reading) podSources(scope Scope) (uids, current, defaults map[string]string, places map[string]logline.Place, err error) {
	uids, current, defaults, places = map[string]string{}, map[string]string{}, map[string]string{}, map[string]logline.Place{}
	var held []stream
	for _, src := range scope.Sources {
		if src.Kind != SourcePod {
			continue
		}
		rows, err := r.rows(`SELECT p.uid, coalesce(`+defaultContainer+`, ''), p.node,
(SELECT json_group_object(c.name, coalesce(nullif(c.container_id, ''), c.last_container_id, '')) FROM containers c WHERE c.pod_uid = p.uid)
FROM objects p WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND p.namespace = ? AND p.name = ?`, src.Namespace, src.Name)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if len(rows) == 0 {
			continue
		}
		key := src.Namespace + "/" + src.Name
		uids[key], defaults[key] = cluster.CellText(rows[0][0]), cluster.CellText(rows[0][1])
		held = append(held, stream{key: logline.StreamKey{Namespace: src.Namespace, Pod: src.Name}, place: logline.Place{Node: cluster.CellText(rows[0][2])}})
		var ids map[string]string
		if err := json.Unmarshal([]byte(cluster.CellText(rows[0][3])), &ids); err != nil { // coverage:ignore SQLite's json_group_object always builds an object
			return nil, nil, nil, nil, err
		}
		for name, id := range ids {
			current[containerKey(src.Namespace, src.Name, name)] = id
		}
	}
	if err := r.place(held); err != nil {
		return nil, nil, nil, nil, err
	}
	for _, st := range held {
		places[st.key.Namespace+"/"+st.key.Pod] = st.place
	}
	return uids, current, defaults, places, nil
}

// containerKey is how target.current names a container: "<ns>/<pod>/<container>".
func containerKey(namespace, pod, container string) string {
	return namespace + "/" + pod + "/" + container
}

// instanceTimes is when the instance a container's status names started and
// finished, in unix nanoseconds, zero where the status does not say: the current
// instance's, or with last the one before it.
func instanceTimes(status string, last bool) (started, finished int64) {
	type times struct {
		StartedAt  time.Time `json:"startedAt"`
		FinishedAt time.Time `json:"finishedAt"`
	}
	var st struct {
		State struct {
			Running    *times `json:"running"`
			Terminated *times `json:"terminated"`
		} `json:"state"`
		LastState struct {
			Terminated *times `json:"terminated"`
		} `json:"lastState"`
	}
	if json.Unmarshal([]byte(status), &st) != nil {
		return 0, 0
	}
	t := st.State.Running
	if st.State.Terminated != nil {
		t = st.State.Terminated
	}
	if last {
		t = st.LastState.Terminated
	}
	if t == nil {
		return 0, 0
	}
	return unixNano(t.StartedAt), unixNano(t.FinishedAt)
}

// unixNano is t in unix nanoseconds, zero for the zero time.
func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// podsOf is the condition on the pod p that it is one of src's pods, with the
// args it binds: the pod itself, or the pods walked down from a workload through
// controller references alone, so the walk reads only the objects it controls.
// Depth 8 bounds the walk, as the ancestors view's does, so an owner cycle ends.
func podsOf(src ScopeSource, kind mirrorKind) (string, []any) {
	if src.Kind == SourcePod {
		return "p.namespace = ? AND p.name = ?", []any{src.Namespace, src.Name}
	}
	return `p.namespace = ? AND p.uid IN (WITH RECURSIVE down(uid, depth) AS (
SELECT o.uid, 0 FROM objects o WHERE o.api_version = ? AND o.kind = ? AND o.namespace = ? AND o.name = ?
UNION ALL
SELECT r.uid, down.depth + 1 FROM down JOIN owners r ON r.owner_uid = down.uid WHERE r.controller = 1 AND down.depth < 8
) SELECT uid FROM down)`, []any{src.Namespace, kind.apiVersion, kind.kind, src.Namespace, src.Name}
}

// containersOf is the condition on the container c of the pod p that src reads
// it, with the args it binds: one it names; with none named, any when it asks
// for every one, else p's default container.
func containersOf(src ScopeSource) (string, []any) {
	switch {
	case len(src.Containers) > 0:
		return " AND c.name IN (" + cluster.Placeholders(len(src.Containers)) + ")", asArgs(src.Containers)
	case src.AllContainers:
		return "", nil
	}
	return " AND c.name = " + defaultContainer, nil
}

// defaultContainer is the name of the pod p's default container, as kubectl logs
// picks it: the one the annotation names when p has it, else p's first.
const defaultContainer = `coalesce(
(SELECT d.name FROM containers d WHERE d.pod_uid = p.uid AND d.name = p.body ->> '$.metadata.annotations."` + DefaultContainerAnnotation + `"'),
(SELECT d.name FROM containers d WHERE d.pod_uid = p.uid AND d.init = 0 ORDER BY d.position LIMIT 1))`

// kindOf is src's kind in the mirror, refusing one outside the enum.
func kindOf(src ScopeSource) (mirrorKind, error) {
	kind, ok := mirrorKinds[src.Kind]
	if !ok {
		return mirrorKind{}, refusal.New(refusal.CodeBadScope, fmt.Sprintf("Kubetail cannot read the logs of a %q", src.Kind))
	}
	return kind, nil
}

// PodCount is a workload source's pods, and those with a previous instance of a
// container it reads.
type PodCount struct {
	Pods     int
	Previous int
}

// CountPods counts src's pods as resolve walks to them, for the LogsView tool's
// receipt.
func CountPods(q cluster.CacheReading, src ScopeSource) (PodCount, error) {
	kind, err := kindOf(src)
	if err != nil {
		return PodCount{}, err
	}
	containers, containerArgs := containersOf(src)
	pods, podArgs := podsOf(src, kind)
	rows, err := reading{q}.rows(`SELECT count(*),
coalesce(sum(EXISTS (SELECT 1 FROM containers c WHERE c.pod_uid = p.uid AND c.last_container_id <> ''`+containers+`)), 0)
FROM objects p WHERE p.api_version = 'v1' AND p.kind = 'Pod' AND `+pods, slices.Concat(containerArgs, podArgs)...)
	if err != nil {
		return PodCount{}, err
	}
	return PodCount{Pods: int(cluster.CellInteger(rows[0][0])), Previous: int(cluster.CellInteger(rows[0][1]))}, nil
}

// filterLabels is the node label each filter field other than the node reads.
var filterLabels = map[FilterField]string{
	FilterRegion: labelRegion,
	FilterZone:   labelZone,
	FilterOS:     labelOS,
	FilterArch:   labelArch,
}

// filterWhere is f as a condition on the pod p, with the args it binds: its node
// among f's values, or a node the mirror holds whose label f reads holds one of
// them, which leaves out a pod whose node the mirror lacks.
func filterWhere(f ScopeFilter) (string, []any) {
	in := cluster.Placeholders(len(f.Values))
	if f.Field == FilterNode {
		return "p.node IN (" + in + ")", asArgs(f.Values)
	}
	return `p.node IN (SELECT n.name FROM objects n JOIN labels l ON l.uid = n.uid
WHERE n.api_version = 'v1' AND n.kind = 'Node' AND l.key = ? AND l.value IN (` + in + "))",
		append([]any{filterLabels[f.Field]}, asArgs(f.Values)...)
}

// asArgs is list as a statement's args.
func asArgs(list []string) []any {
	args := make([]any, len(list))
	for i, s := range list {
		args[i] = s
	}
	return args
}

var (
	// ErrNoCache is a cluster with no cache to resolve a view against.
	ErrNoCache = refusal.New(refusal.CodeNoCache, "this cluster has no cache yet")
	// ErrSyncing is a cache whose verdict withholds its rows: a pod missing from it
	// may still exist.
	ErrSyncing = refusal.New(refusal.CodeSyncing, "this cluster's cache is still syncing")
	// ErrTooMany is a view of more than MaxStreams containers.
	ErrTooMany = refusal.New(refusal.CodeTooMany, fmt.Sprintf("this view reads more than %d containers, the most Kubetail reads at once; filter it by node, or narrow it to fewer sources, containers or pods", MaxStreams))
	// errCut is a mirror answer cut at its bounds. Asking again does not shrink it.
	errCut = refusal.New(refusal.CodeTooMany, "the cluster's cache answered more than Kubetail reads at once")
)

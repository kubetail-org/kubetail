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

// Package logs serves a scope's log lines: it resolves the scope's sources against
// the mirror, reads each container's log through the API server with the user's
// credentials, merges the lines by time, and strips terminal control sequences
// before anything leaves it.
package logs

import "slices"

// Scope is what a read covers: the Sources whose lines are merged by timestamp, in
// the order asked; the Filters on those lines, none keeping every line; and the
// Grep, a regular expression each line's message must match, empty for none. The
// LogsView tool's action carries it, so the tool and the backend share one shape.
type Scope struct {
	Sources []ScopeSource `json:"sources"`
	Filters []ScopeFilter `json:"filters"`
	Grep    string        `json:"grep"`
}

// ScopeSource is one resource a scope reads, in its Namespace, by Kind and Name: its
// Containers by exact name; with none named, every container when AllContainers
// is set, else each pod's default container; and Previous for the instance
// before the last restart, which is finished.
type ScopeSource struct {
	Namespace     string     `json:"namespace"`
	Kind          SourceKind `json:"kind"`
	Name          string     `json:"name"`
	Containers    []string   `json:"containers"`
	AllContainers bool       `json:"allContainers"`
	Previous      bool       `json:"previous"`
}

// DefaultContainerAnnotation is kubectl's: the container a pod's author made the
// default, on the pod or on a workload's pod template.
const DefaultContainerAnnotation = "kubectl.kubernetes.io/default-container"

// SourceKind is a kind the backend reads: a pod, or a workload whose pods it
// follows. The GraphQL LogsViewSourceKind enum binds member by member onto it.
type SourceKind string

const (
	SourcePod         SourceKind = "pod"
	SourceDeployment  SourceKind = "deployment"
	SourceStatefulSet SourceKind = "statefulset"
	SourceDaemonSet   SourceKind = "daemonset"
	SourceJob         SourceKind = "job"
	SourceCronJob     SourceKind = "cronjob"
	SourceReplicaSet  SourceKind = "replicaset"
)

// ScopeFilter is one filter on a scope's lines: the Values its Field may take, never
// empty.
type ScopeFilter struct {
	Field  FilterField `json:"field"`
	Values []string    `json:"values"`
}

// FilterField is what a line can be filtered by: the node it ran on, and the node's
// labels. The GraphQL LogsViewFilterField enum binds member by member onto it.
type FilterField string

const (
	FilterNode   FilterField = "node"
	FilterRegion FilterField = "region"
	FilterZone   FilterField = "zone"
	FilterOS     FilterField = "os"
	FilterArch   FilterField = "arch"
)

// mirrorKind is a source kind's api_version and Kind in the mirror, and its
// plural resource, as kubectl and a refusal name it.
type mirrorKind struct{ apiVersion, kind, resource string }

// mirrorKinds is each source kind's mirrorKind.
var mirrorKinds = map[SourceKind]mirrorKind{
	SourcePod:         {"v1", "Pod", "pods"},
	SourceDeployment:  {"apps/v1", "Deployment", "deployments"},
	SourceStatefulSet: {"apps/v1", "StatefulSet", "statefulsets"},
	SourceDaemonSet:   {"apps/v1", "DaemonSet", "daemonsets"},
	SourceReplicaSet:  {"apps/v1", "ReplicaSet", "replicasets"},
	SourceJob:         {"batch/v1", "Job", "jobs"},
	SourceCronJob:     {"batch/v1", "CronJob", "cronjobs"},
}

// MirrorKind is kind's api_version and Kind in the mirror.
func MirrorKind(kind SourceKind) (apiVersion, k string) {
	m := mirrorKinds[kind]
	return m.apiVersion, m.kind
}

// empty is whether v names nothing to read: no source, filter or grep.
func (v Scope) empty() bool {
	return len(v.Sources) == 0 && len(v.Filters) == 0 && v.Grep == ""
}

// namespaces is each namespace v's sources read, once: what a tail's pods watch
// is narrowed to.
func (v Scope) namespaces() []string {
	var out []string
	for _, src := range v.Sources {
		if !slices.Contains(out, src.Namespace) {
			out = append(out, src.Namespace)
		}
	}
	return out
}

// controls is the kind a workload kind's controller makes, which podsOf walks
// through to its pods.
var controls = map[SourceKind]SourceKind{SourceDeployment: SourceReplicaSet, SourceCronJob: SourceJob}

// workloadKinds is each kind v's workload sources walk through to their pods,
// once: a source's own, and the kind it controls.
func (v Scope) workloadKinds() []mirrorKind {
	var out []mirrorKind
	add := func(kind SourceKind) {
		if m := mirrorKinds[kind]; !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	for _, src := range v.Sources {
		if src.Kind == SourcePod {
			continue
		}
		add(src.Kind)
		if child, ok := controls[src.Kind]; ok {
			add(child)
		}
	}
	return out
}

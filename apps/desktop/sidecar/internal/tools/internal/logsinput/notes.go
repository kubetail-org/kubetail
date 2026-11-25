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

package logsinput

import (
	"fmt"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// SourcesText is the sources as a tool names them: the first, in its namespace,
// and how many more.
func SourcesText(typed []Source) string {
	out := nameOf(typed[0]) + " in " + typed[0].Namespace
	if more := len(typed) - 1; more == 1 {
		out += " and 1 more source"
	} else if more > 1 {
		out += fmt.Sprintf(" and %d more sources", more)
	}
	return out
}

// SourcesOf is a scope's sources as a call types them, for a tool to name the
// scope a token holds.
func SourcesOf(sources []tools.LogsViewSource) []Source {
	out := make([]Source, len(sources))
	for i, src := range sources {
		out[i] = Source{Namespace: src.Namespace, Kind: src.Kind, Name: src.Name}
	}
	return out
}

// NotesText is what a tool says of each source beyond its name, one string per
// source: its sentences, each under the source's name when there are several.
func NotesText(typed []Source, sources []tools.LogsViewSource, notes []Note) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		var b strings.Builder
		for _, line := range noteLines(typed[i], sources[i], n) {
			if len(typed) > 1 {
				line = nameOf(typed[i]) + ": " + line
			} else {
				line = strings.ToUpper(line[:1]) + line[1:]
			}
			b.WriteString(" " + line + ".")
		}
		out[i] = b.String()
	}
	return out
}

// nameOf is a source as kubectl names it, <resource>/<name>.
func nameOf(s Source) string { return resourceOf[s.Kind] + "/" + s.Name }

// resourceOf is each kind's plural resource, as a tool names a source.
var resourceOf = map[tools.LogsSourceKind]string{
	tools.LogsSourcePod: "pods", tools.LogsSourceDeployment: "deployments", tools.LogsSourceStatefulSet: "statefulsets",
	tools.LogsSourceDaemonSet: "daemonsets", tools.LogsSourceJob: "jobs", tools.LogsSourceCronJob: "cronjobs",
	tools.LogsSourceReplicaSet: "replicasets",
}

// noteLines is what a tool says of one source beyond its name: the container
// defaulted among several and the others, the containers named or every one, the
// default container of an object it cannot see yet, the previous instance, of how
// many of a workload's pods, and whether its object is missing or has nothing to
// read yet.
func noteLines(typed Source, src tools.LogsViewSource, n Note) []string {
	var lines []string
	switch {
	case n.defaulted != "":
		lines = append(lines, "defaulted container "+n.defaulted+" out of "+strings.Join(n.containers, ", "))
	case src.AllContainers && len(n.containers) > 0:
		lines = append(lines, "containers "+strings.Join(n.containers, ", "))
	case src.AllContainers:
		lines = append(lines, "every container")
	case len(src.Containers) > 1:
		lines = append(lines, "containers "+strings.Join(src.Containers, ", "))
	case len(src.Containers) == 1:
		lines = append(lines, "container "+src.Containers[0])
	case len(n.containers) == 0:
		lines = append(lines, "default container")
	}
	switch {
	case !src.Previous:
	case typed.Kind == tools.LogsSourcePod, n.state == logs.Missing:
		lines = append(lines, "previous instance")
	default:
		lines = append(lines, fmt.Sprintf("previous instance of %d of %d pods", n.restarted, n.pods))
	}
	switch {
	case n.state == logs.Missing:
		lines = append(lines, "not in Kubetail's cache of the cluster yet; the view picks it up when it appears")
	case n.state == logs.Idle && src.Previous:
		lines = append(lines, "nothing to read yet; the view picks up its containers as they restart")
	case n.state == logs.Idle:
		lines = append(lines, "nothing to read yet; the view picks up its containers as they start")
	}
	return lines
}

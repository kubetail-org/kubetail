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
	"encoding/json"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// found is what the mirror holds of a source's object: its containers first and
// its init containers after them, and the default-container annotation's value.
type found struct {
	uid       string
	names     []string
	annotated string
}

// templateOf is where kind's pod template is in its body, "" for a pod's own spec.
func templateOf(kind tools.LogsSourceKind) string {
	switch kind {
	case tools.LogsSourcePod:
		return ""
	case tools.LogsSourceCronJob:
		return "$.spec.jobTemplate.spec.template"
	}
	return "$.spec.template"
}

// object is the source's object as the mirror holds it, a zero uid for none: a
// pod's containers off the containers table, a workload's off its pod template.
func object(q cluster.CacheReading, s Source) (found, error) {
	apiVersion, kind := logs.MirrorKind(s.Kind)
	template := templateOf(s.Kind)
	const where = " WHERE o.api_version = ? AND o.kind = ? AND o.namespace = ? AND o.name = ?"
	args := []any{apiVersion, kind, s.Namespace, s.Name}
	if template == "" {
		res, err := q.Query(`SELECT o.uid, o.body ->> '$.metadata.annotations."`+logs.DefaultContainerAnnotation+`"', c.name
FROM objects o LEFT JOIN containers c ON c.pod_uid = o.uid`+where+` ORDER BY c.init, c.position`, args...)
		if err != nil || len(res.Rows) == 0 {
			return found{}, err
		}
		f := found{uid: cluster.CellText(res.Rows[0][0]), annotated: cluster.CellText(res.Rows[0][1])}
		for _, row := range res.Rows {
			if name := cluster.CellText(row[2]); name != "" {
				f.names = append(f.names, name)
			}
		}
		return f, nil
	}
	res, err := q.Query(`SELECT o.uid, o.body ->> '`+template+`.metadata.annotations."`+logs.DefaultContainerAnnotation+`"',
o.body -> '`+template+`.spec.containers', o.body -> '`+template+`.spec.initContainers'
FROM objects o`+where, args...)
	if err != nil || len(res.Rows) == 0 {
		return found{}, err
	}
	f := found{uid: cluster.CellText(res.Rows[0][0]), annotated: cluster.CellText(res.Rows[0][1])}
	f.names = append(containerNames(cluster.CellText(res.Rows[0][2])), containerNames(cluster.CellText(res.Rows[0][3]))...)
	return f, nil
}

// containerNames is each container's name in a template's list, as JSON.
func containerNames(list string) []string {
	var containers []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal([]byte(list), &containers) // a body the mirror holds is valid JSON; anything else is no container
	var names []string
	for _, c := range containers {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	return names
}

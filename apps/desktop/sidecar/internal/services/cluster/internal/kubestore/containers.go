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

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// containerRow is one row of the containers table: a Pod's container, its spec and its
// status joined by name. The JSON names are the columns, since the rows bind as one JSON
// argument; a nil is a field the body did not say.
type containerRow struct {
	Init            int      `json:"init"`
	Position        int      `json:"position"`
	Name            string   `json:"name"`
	Sidecar         int      `json:"sidecar"`
	Image           *string  `json:"image"`
	ImageID         *string  `json:"image_id"`
	ContainerID     *string  `json:"container_id"`
	Ready           *int     `json:"ready"`
	Restarts        *int64   `json:"restarts"`
	State           *string  `json:"state"`
	Reason          *string  `json:"reason"`
	ExitCode        *int64   `json:"exit_code"`
	LastReason      *string  `json:"last_reason"`
	LastExitCode    *int64   `json:"last_exit_code"`
	LastContainerID *string  `json:"last_container_id"`
	CPURequest      *float64 `json:"cpu_request"`
	CPULimit        *float64 `json:"cpu_limit"`
	MemoryRequest   *float64 `json:"memory_request"`
	MemoryLimit     *float64 `json:"memory_limit"`
}

// containersOf reads a core Pod's containers, init containers first, each in spec order.
// Ephemeral containers are left out, and any other kind has none.
func containersOf(u *unstructured.Unstructured) []containerRow {
	if !isCorePod(u) {
		return nil
	}
	var rows []containerRow
	for _, list := range []struct {
		init          int
		field, status string
	}{{1, "initContainers", "initContainerStatuses"}, {0, "containers", "containerStatuses"}} {
		statuses := statusesByName(nestedSlice(u.Object, "status", list.status))
		for i, c := range nestedSlice(u.Object, "spec", list.field) {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			row := containerRow{Init: list.init, Position: i}
			row.Name, _, _ = unstructured.NestedString(m, "name")
			row.Image = nestedStringPtr(m, "image")
			if list.init == 1 {
				if policy, _, _ := unstructured.NestedString(m, "restartPolicy"); policy == "Always" {
					row.Sidecar = 1
				}
			}
			row.CPURequest = quantityAt(m, "resources", "requests", "cpu")
			row.CPULimit = quantityAt(m, "resources", "limits", "cpu")
			row.MemoryRequest = quantityAt(m, "resources", "requests", "memory")
			row.MemoryLimit = quantityAt(m, "resources", "limits", "memory")
			if st, ok := statuses[row.Name]; ok {
				readStatus(&row, st)
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// statusesByName keys a list of container statuses by the container they describe.
func statusesByName(list []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(list))
	for _, s := range list {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, _, _ := unstructured.NestedString(m, "name"); name != "" {
			out[name] = m
		}
	}
	return out
}

// containerStates are the keys a status's state names one of, in the order they are looked
// for.
var containerStates = []string{"running", "waiting", "terminated"}

// readStatus fills the row's status columns from the container's status.
func readStatus(row *containerRow, st map[string]any) {
	if ready, ok, err := unstructured.NestedBool(st, "ready"); ok && err == nil {
		row.Ready = new(int)
		if ready {
			*row.Ready = 1
		}
	}
	row.Restarts = nestedInt64Ptr(st, "restartCount")
	row.ImageID = nestedStringPtr(st, "imageID")
	for _, state := range containerStates {
		if _, ok, _ := unstructured.NestedFieldNoCopy(st, "state", state); ok {
			row.State = &state
			break
		}
	}
	row.Reason = nestedStringPtr(st, "state", "waiting", "reason")
	if row.Reason == nil {
		row.Reason = nestedStringPtr(st, "state", "terminated", "reason")
	}
	row.ExitCode = nestedInt64Ptr(st, "state", "terminated", "exitCode")
	row.ContainerID = stateContainerID(st, row.State)
	row.LastReason = nestedStringPtr(st, "lastState", "terminated", "reason")
	row.LastExitCode = nestedInt64Ptr(st, "lastState", "terminated", "exitCode")
	row.LastContainerID = nestedStringPtr(st, "lastState", "terminated", "containerID")
}

// stateContainerID is the runtime id of the instance st's state describes: the
// status's own while it runs, the terminated state's once it ends, and none while it
// waits, whatever the status's own still names. The kubelet serves a log by the same
// rule, so the id is the log's.
func stateContainerID(st map[string]any, state *string) *string {
	switch {
	case state == nil:
		return nil
	case *state == "running":
		return nestedStringPtr(st, "containerID")
	case *state == "terminated":
		return nestedStringPtr(st, "state", "terminated", "containerID")
	}
	return nil
}

// nestedInt64Ptr is the integer at fields, or nil where there is none.
func nestedInt64Ptr(m map[string]any, fields ...string) *int64 {
	n, ok, err := unstructured.NestedInt64(m, fields...)
	if !ok || err != nil {
		return nil
	}
	return &n
}

// isCorePod says the body is a core Pod, by its own apiVersion and kind.
func isCorePod(u *unstructured.Unstructured) bool {
	return u.GetAPIVersion() == "v1" && u.GetKind() == "Pod"
}

// nestedStringPtr is the string at fields, or nil where there is none.
func nestedStringPtr(m map[string]any, fields ...string) *string {
	s, ok, err := unstructured.NestedString(m, fields...)
	if !ok || err != nil {
		return nil
	}
	return &s
}

// quantityAt is the quantity at fields through parseQuantity, or nil where there is none
// or the parse refuses it.
func quantityAt(m map[string]any, fields ...string) *float64 {
	s := nestedStringPtr(m, fields...)
	if s == nil {
		return nil
	}
	f, ok := parseQuantity(*s)
	if !ok {
		return nil
	}
	return &f
}

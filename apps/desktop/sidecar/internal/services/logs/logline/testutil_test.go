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

package logline

import (
	"fmt"
	"time"
)

// at is a moment on 2026-10-08 at 10:00, offset by d.
func at(d time.Duration) time.Time {
	return time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC).Add(d)
}

// containerID is the runtime id of the instance-th instance of the container
// name in the pod uid, as containerStatus writes it.
func containerID(uid, name string, instance int) string {
	return fmt.Sprintf("containerd://%s-%s-%d", uid, name, instance)
}

// line is a record of pod/container in prod at d past 10:00, from the first
// instance of a fixture pod, the first line of its moment, and nth the
// ordinal-th.
func line(pod, container string, d time.Duration, msg string) Record {
	return nth(pod, container, d, 0, msg)
}

func nth(pod, container string, d time.Duration, ordinal int, msg string) Record {
	return Record{Timestamp: at(d), Message: msg, Stream: StreamKey{Namespace: "prod", Pod: pod, Container: container}, ContainerID: containerID("uid-"+pod, container, 0), Ordinal: ordinal}
}

// lineAt is the line cursor of key's line at d past 10:00, from the first instance
// of a fixture pod, the ordinal-th of its moment.
func lineAt(key StreamKey, d time.Duration, ordinal int) Cursor {
	return Cursor{ContainerName{key.Namespace, key.Pod, key.Container}, RunPlace{containerID("uid-"+key.Pod, key.Container, 0), at(d), ordinal}}
}

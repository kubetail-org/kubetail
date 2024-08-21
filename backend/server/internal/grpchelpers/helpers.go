// Copyright 2024 Andres Morey
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

package grpchelpers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var agentLabelSet = labels.Set{
	"app.kubernetes.io/name":      "kubetail",
	"app.kubernetes.io/component": "agent",
}

var agentLabelSelectorString = labels.SelectorFromSet(agentLabelSet).String()

// Check if pod is running
func isPodRunning(pod *corev1.Pod) bool {
	if pod.ObjectMeta.DeletionTimestamp != nil {
		// terminating
		return false
	}
	return pod.Status.Phase == corev1.PodRunning
}

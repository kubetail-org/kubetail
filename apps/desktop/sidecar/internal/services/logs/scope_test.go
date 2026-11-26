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
	"testing"

	"github.com/stretchr/testify/assert"
)

// A scope names each namespace its sources read once, in the order asked.
func TestAScopeNamesEachNamespaceOnce(t *testing.T) {
	t.Parallel()
	view := Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "web-1"},
		{Namespace: "staging", Kind: SourcePod, Name: "web-1"},
		{Namespace: "prod", Kind: SourceDeployment, Name: "web"},
	}}
	assert.Equal(t, []string{"prod", "staging"}, view.namespaces())
	assert.Empty(t, Scope{}.namespaces())
}

// The workload kinds a view watches are each source's own and the kind it
// controls, once each, and none for a pod.
func TestAViewWatchesTheKindsItsSourcesWalk(t *testing.T) {
	t.Parallel()
	view := Scope{Sources: []ScopeSource{
		{Namespace: "prod", Kind: SourcePod, Name: "web-1"},
		{Namespace: "prod", Kind: SourceDeployment, Name: "web"},
		{Namespace: "prod", Kind: SourceReplicaSet, Name: "web-7d9f"},
		{Namespace: "prod", Kind: SourceCronJob, Name: "nightly"},
		{Namespace: "prod", Kind: SourceStatefulSet, Name: "db"},
	}}
	var resources []string
	for _, kind := range view.workloadKinds() {
		resources = append(resources, kind.resource)
	}
	assert.Equal(t, []string{"deployments", "replicasets", "cronjobs", "jobs", "statefulsets"}, resources)
}

// Each source kind names its api_version and Kind in the mirror.
func TestEverySourceKindHasAMirrorKind(t *testing.T) {
	t.Parallel()
	for kind, want := range map[SourceKind][2]string{
		SourcePod:         {"v1", "Pod"},
		SourceDeployment:  {"apps/v1", "Deployment"},
		SourceStatefulSet: {"apps/v1", "StatefulSet"},
		SourceDaemonSet:   {"apps/v1", "DaemonSet"},
		SourceReplicaSet:  {"apps/v1", "ReplicaSet"},
		SourceJob:         {"batch/v1", "Job"},
		SourceCronJob:     {"batch/v1", "CronJob"},
	} {
		apiVersion, k := MirrorKind(kind)
		assert.Equal(t, want, [2]string{apiVersion, k}, kind)
	}
}

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

package bash

import (
	"context"
	"errors"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
)

// target is how a sandboxed run reaches the chat's cluster: the cluster, whose
// mode its grant reads, the kube-context its kubeconfig names, the context its
// grant's rules match, the directory
// its kubectl cache lives in, and the server UID the cache is keyed on and the
// claim vouches for, "" for a cluster never identified.
type target struct {
	id           apimeta.ClusterID
	context      string
	scopeContext string
	cacheDir     string
	serverUID    string
}

// errClusterGone is a chat's cluster that is gone or being deleted.
var errClusterGone = errors.New("the chat's cluster is gone")

// unnamedContext names the run's one context for a record that names no
// kube-context. The card names none then, and the context is current, so a
// command reaches it without --context.
const unnamedContext = "kubetail"

// target reads the cluster's record from the cluster service, makes the
// kubectl cache for the server it serves, and names the kube-context off it.
func (t *Tool) target(ctx context.Context, id apimeta.ClusterID) (target, error) {
	cluster, err := t.clusterSvc.Clusters().Get(ctx, id)
	if err != nil {
		return target{}, err
	}
	if cluster == nil || cluster.DeletionRequestedAt != nil {
		return target{}, errClusterGone
	}
	uid := ""
	if cluster.Status.Server.UID != nil {
		uid = *cluster.Status.Server.UID
	}
	cacheDir, err := makeKubectlCache(t.kubectlDir, id, uid)
	if err != nil {
		return target{}, err
	}
	name := clustercard.ContextName(cluster)
	if name == "" {
		name = unnamedContext
	}
	return target{id: id, context: name, scopeContext: cluster.KubeContext(), cacheDir: cacheDir, serverUID: uid}, nil
}

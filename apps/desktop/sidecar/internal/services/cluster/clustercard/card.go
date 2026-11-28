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

package clustercard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// ClusterCards renders the card for a chat's cluster over the cluster service. It is
// chat's ClusterCards.
type ClusterCards struct{ svc cluster.Service }

func New(svc cluster.Service) *ClusterCards { return &ClusterCards{svc: svc} }

// ClusterCard is the card for clusterID, or Unavailable when none can be rendered: the
// record is gone, a read fails, the identity moves under the reading twice, or ctx
// ends first. The send is never refused over it.
func (c *ClusterCards) ClusterCard(ctx context.Context, clusterID apimeta.ClusterID) string {
	card, _ := c.ClusterCardAt(ctx, clusterID, "")
	return card
}

// ClusterCardAt is ClusterCard read at serverUID's server, and false with no card
// when the record names another or its identity moves under the reading: a reading
// that would describe it is refused before anything of its cache is read. "" reads whichever server the record names.
func (c *ClusterCards) ClusterCardAt(ctx context.Context, clusterID apimeta.ClusterID, serverUID string) (string, bool) {
	var f Facts
	err := c.svc.Clusters().ReadActive(ctx, clusterID, func(ctx context.Context, a cluster.ActiveCluster) error {
		if !a.ReadsServer(serverUID) {
			return cluster.ErrIdentityMismatch
		}
		var err error
		f, err = Read(ctx, c.svc, a)
		return err
	})
	switch err = cluster.PinnedReadErr(serverUID, err); {
	case errors.Is(err, cluster.ErrIdentityMismatch):
		return "", false
	case err != nil:
		slog.Debug("the cluster card is unavailable", "cluster", clusterID, "err", err)
		return Unavailable, true
	}
	return Render(f), true
}

// Read gathers the card's facts for the cluster it is handed: the record's fields,
// and with a cache its health, sync rows, kind catalog and namespace names.
func Read(ctx context.Context, svc cluster.Service, a cluster.ActiveCluster) (Facts, error) {
	cluster := a.Cluster
	f := Facts{
		Name: deref(cluster.Spec.Name), Context: ContextName(cluster), Version: deref(cluster.Status.Server.Version),
		Connection: connection(cluster), TLS: tlsPosture(cluster),
	}
	if a.Cache == nil {
		return f, nil
	}
	cf, err := cacheFacts(ctx, svc, cluster.ID, a.Cache.ID)
	if err != nil {
		return Facts{}, err
	}
	f.Cache = cf
	return f, nil
}

// ContextName is the one spelling of the cluster's kube-context, which the card
// shows as cluster.context and a sandboxed run's kubeconfig names its one context
// by: the record's kube-context cut as the card cuts it, empty for a record that
// names none. So the prompts' kubectl --context <context> selects it even when
// the card had to cut the name.
func ContextName(c *cluster.Cluster) string {
	return cut(c.KubeContext(), contextMax)
}

// tlsPosture is whether the server's certificate is verified, off the
// kubeconfig entry the status mirrors. The scheme is resolved the way client-go
// dials it — a schemeless server is https only when a CA or client certificate
// is named or verifying is skipped, else plain http — so a server without TLS
// is none ahead of the skip-verify flag. Empty when there is no entry or
// client-go could not parse it. The server URL itself stays out of the card.
func tlsPosture(c *cluster.Cluster) string {
	src := c.Status.Source.Kubeconfig
	if src == nil || src.Cluster.Entry == nil {
		return ""
	}
	entry := src.Cluster.Entry
	defaultTLS := entry.HasCertificateAuthority || src.User.HasClientCertificate || entry.InsecureSkipTLSVerify
	u, _, err := rest.DefaultServerURL(entry.Server, "", schema.GroupVersion{}, defaultTLS)
	switch {
	case err != nil:
		return ""
	case u.Scheme != "https":
		return tlsNone
	case entry.InsecureSkipTLSVerify:
		return tlsUnverified
	default:
		return tlsVerified
	}
}

// cacheFacts is one reading of the cache: its health, its sync rows folded onto the
// catalog, and its namespace names.
func cacheFacts(ctx context.Context, svc cluster.Service, id cluster.ClusterID, cacheID cluster.ClusterCacheID) (*CacheFacts, error) {
	health, ok, err := svc.Caches().Health(ctx, id, cacheID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("read cache %d health: the cache is gone", cacheID)
	}
	sync, ok, err := svc.Caches().SyncState(ctx, id, cacheID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("read cache %d sync status: the cache is gone", cacheID)
	}
	kinds, err := svc.CachedData().ListKinds(ctx, id, cacheID, "")
	if err != nil {
		return nil, err
	}
	// A cache with no file yet lists nothing, which the card renders as syncing;
	// only a reader that wants a count has to tell the two apart.
	namespaces, _, err := svc.CachedData().ListObjects(ctx, id, cacheID, "v1", "namespaces")
	if err != nil {
		return nil, err
	}

	cf := &CacheFacts{Health: health, Discovery: sync.Discovery.Reason}
	type kindKey struct{ apiVersion, resource string }
	reasons := map[kindKey]string{}
	for _, row := range sync.Kinds {
		reasons[kindKey{row.APIVersion, row.Resource}] = row.Reason
	}
	for _, k := range kinds {
		cf.Kinds = append(cf.Kinds, Kind{
			APIVersion: k.APIVersion, Kind: k.Kind, Resource: k.Resource, Scope: k.Scope,
			Reason: reasons[kindKey{k.APIVersion, k.Resource}],
		})
	}
	for _, ns := range namespaces {
		cf.Namespaces = append(cf.Namespaces, ns.Name)
	}
	return cf, nil
}

// connection is the record's verdict in one reason: the identity's when the
// connection is up but the identity is not, else the connection's own.
func connection(c *cluster.Cluster) string {
	connected := cluster.FindCondition(c.Conditions, cluster.ConditionConnected)
	identified := cluster.FindCondition(c.Conditions, cluster.ConditionIdentified)
	if connected != nil && connected.Status == cluster.ConditionTrue &&
		identified != nil && identified.Status != cluster.ConditionTrue {
		return identified.Reason
	}
	if connected != nil {
		return connected.Reason
	}
	return cluster.ReasonConnecting
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

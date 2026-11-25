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

// The ClusterSource kind: one discovery anchor per source variant, naming the
// clusters that source declares. Its beehive shapes, its controller — the importer,
// which writes the clusters table — and the bootstrap that creates the anchors.
// Internal to this package — no GraphQL type binds it.
package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amorey/beehive"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// ClusterSourceGroupKind identifies the discovery-anchor kind: one object per source
// variant, which is what a discovery pass is scheduled, retried, and settled against.
var ClusterSourceGroupKind = beehive.GroupKind{Kind: "ClusterSource"}

// ClusterSourceNameKubeconfig names the kubeconfig anchor. Exactly one per variant,
// so creation is idempotent under name-uniqueness dedup; a creation key only, never
// an identity.
const ClusterSourceNameKubeconfig = "clustersource/kubeconfig"

// clusterSourceNames is every anchor the package keeps. A new source variant is one
// entry here, one in clusterSourceNameFor, plus the controller branch that reads it.
var clusterSourceNames = []string{ClusterSourceNameKubeconfig}

// clusterSourceNameFor names the anchor that imports rows of source; false for a
// source no anchor imports.
func clusterSourceNameFor(source string) (string, bool) {
	if source == SourceKubeconfig {
		return ClusterSourceNameKubeconfig, true
	}
	return "", false
}

// clusterSourceResyncInterval paces the discovery pass, as the kind's individual pass
// (registered in service.go). A backstop, not the mechanism: kubeconfigTrigger is
// what makes a file change prompt, and this is what covers a kick that was never
// delivered. A pass observing an unchanged snapshot writes nothing, so the tick wakes
// no dependent.
//
// Registered rather than requeued on the result, because the poll is the anchor's
// correctness: every return path would otherwise have to re-arm it, and the one that
// forgot would stop discovery for the life of the process.
const clusterSourceResyncInterval = 10 * time.Minute

// ClusterSourceSpec is empty. An anchor's identity is its name and a source has
// nothing the user configures; the object exists to give the discovery pass a place
// in the control plane rather than to hold desired state.
type ClusterSourceSpec struct{}

// ClusterSourceStatus is the anchor's propagation channel and nothing else. Every
// Cluster from this source depends on the anchor, so writing this wakes all of them —
// which is the point, since their observation reads the source rather than the object
// beehive handed them, and no store write would otherwise reach it.
type ClusterSourceStatus struct {
	// Fingerprint identifies what the last pass observed. It changes exactly when
	// something a dependent would observe changed, so an unchanged snapshot leaves it
	// alone; a stamp that moved every pass would wake every Cluster every pass.
	Fingerprint string `json:"fingerprint"`
}

// clusterSourceController runs one source's discovery pass: it imports the clusters
// the source declares as rows, then publishes what it observed for the runtime
// records to wake on.
//
// It never writes a Cluster's status. That is the Cluster controller's, which is why
// this pass ends at a fingerprint rather than an observation — set membership here,
// per-object interpretation there.
type clusterSourceController struct {
	lifecycle.None
	deps
}

func (c *clusterSourceController) Reconcile(
	ctx context.Context,
	client beehive.ControllerClient[ClusterSourceStatus],
	obj *beehive.Object[ClusterSourceSpec, ClusterSourceStatus],
) beehive.Result {
	// An anchor on its way out has no set to maintain, and beehive collects it either
	// way. Nothing deletes one today.
	if obj.DeletionRequestedAt != nil {
		return beehive.Settled()
	}

	// Unreachable through the composition root, which starts the kubeconfig service
	// ahead of this one and reads synchronously. It stays because the pre-read config
	// is empty and indistinguishable from a file with no contexts: importing against
	// it would fingerprint an empty set and wake every record to observe its context
	// absent, so the guard is what keeps the pass correct if that ordering ever stops
	// holding.
	cfg, loaded := c.kubeconfigSvc.Get()
	if !loaded {
		return beehive.Unsettled().RequeueAfter(startupRequeue)
	}

	// Runs unconditionally, not behind the fingerprint gate: an import that failed is
	// retried against the snapshot that failed, so a pass returning early on an
	// unchanged fingerprint would skip the retry and never import that context.
	//
	// Its error is carried to the end rather than returned here. The publish below is
	// the only thing that wakes a dependent, and the two describe different things —
	// the fingerprint says what this pass observed, not which rows it managed to
	// insert.
	createErr := importKubeconfigContexts(ctx, c.db, c.store, cfg, time.Now())

	fingerprint, err := kubeconfigFingerprint(cfg)
	if err != nil {
		return beehive.Fail(errors.Join(createErr, err))
	}

	// A repeat write would only marshal the same bytes and open a transaction to find
	// them equal; the settle below is what a pass that observed nothing new reports.
	if obj.Status == nil || obj.Status.Fingerprint != fingerprint {
		if err := client.UpdateStatus(ctx, ClusterSourceStatus{Fingerprint: fingerprint}); err != nil {
			return beehive.Fail(errors.Join(createErr, fmt.Errorf("update cluster source status: %w", err)))
		}
	}

	if createErr != nil {
		// A stuck create is retried by beehive's backoff ladder, which is tighter than
		// the resync pacing this kind.
		return beehive.Fail(createErr)
	}
	return beehive.Settled()
}

// importKubeconfigContexts gives every context in cfg a clusters row, in one
// transaction, and wakes the clusters watchers and the mirror if it inserted any.
//
// CREATION-ONLY: it writes the row's identity and source and never updates, orphans,
// or deletes. A departed context is orphaned by clusterController observing it absent
// (IsPresent=false), and a returning one finds its never-deleted row — which is what
// preserves the user's toggles across a context that comes and goes. A marked row
// keeps the claim too, so a context is not re-created mid-teardown. Ids are minted
// per pass; the ones a conflict discards are never seen.
func importKubeconfigContexts(ctx context.Context, db *appdb.DB, store *sqlstmt.Set[stmtID], cfg *api.Config, now time.Time) error {
	inserted := 0
	err := store.InTx(ctx, func(st stmts) error {
		for contextName := range cfg.Contexts {
			did, err := insertClusterIfAbsent(ctx, st, appdb.NewID(), contextName, now)
			if err != nil {
				return fmt.Errorf("importing context %q: %w", contextName, err)
			}
			if did {
				inserted++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if inserted > 0 {
		db.Notify(appdb.KeyClusters)
	}
	return nil
}

// ensureClusterSources gives every variant its anchor. The one creation in this
// package with nothing above it — every other record is created by its parent, and an
// anchor is what a parent would have been — so it runs from the service's startup
// rather than from a reconcile.
func ensureClusterSources(ctx context.Context, client beehive.Client[ClusterSourceSpec, ClusterSourceStatus]) error {
	for _, name := range clusterSourceNames {
		if _, _, err := client.GetOrCreate(ctx, name, ClusterSourceSpec{}); err != nil {
			return fmt.Errorf("create cluster source %s: %w", name, err)
		}
	}
	return nil
}

// kubeconfigFingerprint digests the whole snapshot: every field of every entry, whether
// or not a dependent reads it today.
//
// Deliberately coarser than what the records observe. A digest built from the folds
// themselves would wake nobody for a field the next fold starts reading, and keeping
// the two in step is a coupling nothing enforces — so this covers everything and pays
// for it in false positives instead. An edit no record cares about wakes them all, each
// to compare, find nothing moved, and settle.
func kubeconfigFingerprint(cfg *api.Config) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("digest kubeconfig: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

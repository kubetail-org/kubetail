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

// The kubectl cache a sandboxed run's KUBECACHEDIR names: one directory per
// cluster, one per server identity under it, shared by every chat on the
// cluster.
//
//	<cache>/kubectl/<cluster id>/<server>/
package bash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// unknownServer names the cache of a record whose server has no UID yet.
const unknownServer = "unknown"

// makeKubectlCache makes <cluster id>/<server> under dir and answers its path.
// Each level is made through a root on the one above it, so a link a command
// swaps in at any level is refused. dir is opened on each call: the OS or the
// user may clear the cache directory while Kubetail runs.
func makeKubectlCache(dir string, id apimeta.ClusterID, uid string) (string, error) {
	// The id names a directory, so one that is not a plain name would reach
	// another than the cluster's.
	name := string(id)
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("the kubectl cache of cluster %q: not a plain name", id)
	}
	key := serverKey(uid)
	root, err := rootdir.MakeRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open the kubectl cache: %w", err)
	}
	defer root.Close()
	cluster, err := rootdir.Open(root, name, true)
	if err != nil {
		return "", err
	}
	defer cluster.Close()
	server, err := rootdir.Open(cluster, key, true)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name, key), server.Close()
}

// serverKey is the name of a server identity's cache: the first 16 bytes of
// the SHA-256 of its UID in hex, or unknownServer for none. A context repointed
// at another cluster keeps its record, so the cache is split by server, and the
// UID is the cluster's text, so it names the directory only through the hash.
func serverKey(uid string) string {
	if uid == "" {
		return unknownServer
	}
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:16])
}

// sweepKubectlCache removes every entry of dir that names no cluster the
// service holds: what a deleted cluster left. It lists dir before reading the
// clusters, and a cache is made only for a cluster already read, so a new
// cluster's is never taken. A failure is logged, and the next start tries
// again.
func sweepKubectlCache(ctx context.Context, dir string, clusterSvc cluster.Service) {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		slog.Warn("could not open the kubectl cache to sweep it", "err", err)
		return
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil { // coverage:ignore a folder just opened for reading fails to list only on an I/O error
		slog.Warn("could not list the kubectl cache", "err", err)
		return
	}
	clusters, err := clusterSvc.Clusters().List(ctx)
	if err != nil {
		slog.Warn("could not read the clusters to sweep the kubectl cache", "err", err)
		return
	}
	live := make(map[string]bool, len(clusters))
	for _, c := range clusters {
		live[string(c.ID)] = true
	}
	rootdir.Sweep(root, entries, func(e fs.DirEntry) bool { return live[e.Name()] })
}

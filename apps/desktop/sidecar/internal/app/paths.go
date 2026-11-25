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

package app

import (
	"path/filepath"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// paths is every file and directory the sidecar keeps, each in the kind of
// directory it belongs to, grouped by the service that owns it. Each service
// is handed its own and names nothing else; each owner makes its own
// directories. A shipped path is never renamed or moved: nothing migrates the
// layout on upgrade, so it only grows (docs/persisted-data.md).
//
//	<data>/                                what a user would lose
//	  sidecar.lock                         app: held while a sidecar runs; never deleted
//	  app.db                               app
//	  beehive.db                           cluster
//	  chats/<chat id>/                     chat: results/, tasks/, workspace/
//	  monitor/<cluster id>/                chat: a monitor's results/, workspace/, toolhome/
//	<cache>/                               what Kubetail rebuilds
//	  kube/<cache id>.db                   cluster: the mirror
//	  logs/<cache id>.db                   logs: the cache's log lines
//	  kubectl/<cluster id>/<server>/       bash: the kubectl cache
//	  tmp/<pid>-*/, tmp/<pid>.lock         bash: a sandboxed run's TMPDIR, the sidecar's lock
//	<runtime>/                             what lives for a session
//	  kubetail-sidecar-<host pid>-<n>.sock   the host; one a killed sidecar left is swept at start
//	  shell/                               bash: the snapshot, and Windows' scripts
//	  runs/<pid>-*/, runs/<pid>.lock       bash: a run's kubeconfig and socket, the lock
//
// Each subfolder above that a service keeps its files in is made at start,
// and must be a folder, not a link (makeSubfolders).
type paths struct {
	LockFile   string
	AppDBFile  string
	ChatsDir   string
	MonitorDir string
	LogsDir    string
	Cluster    cluster.Paths
	Bash       bash.Paths
}

func pathsOf(cfg Config) paths {
	data, cache, runtime := cfg.Folders.Data, cfg.Folders.Cache, cfg.Folders.Runtime
	kubetail := []string{data, cache, runtime}
	// Every folder the layout names is fenced too: the host's config, state
	// and logs, and the app folders that hold the kinds on macOS and Windows.
	for _, dir := range cfg.Folders.Layout.Dirs() {
		if !slices.Contains(kubetail, dir) {
			kubetail = append(kubetail, dir)
		}
	}
	p := paths{
		LockFile:   filepath.Join(data, "sidecar.lock"),
		AppDBFile:  filepath.Join(data, "app.db"),
		ChatsDir:   filepath.Join(data, "chats"),
		MonitorDir: filepath.Join(data, "monitor"),
		LogsDir:    filepath.Join(cache, "logs"),
		Cluster: cluster.Paths{
			BeehiveDBFile: filepath.Join(data, "beehive.db"),
			KubestoreDir:  filepath.Join(cache, "kube"),
		},
		Bash: bash.Paths{
			ShellDir:   filepath.Join(runtime, "shell"),
			RunsDir:    filepath.Join(runtime, "runs"),
			TmpDir:     filepath.Join(cache, "tmp"),
			KubectlDir: filepath.Join(cache, "kubectl"),
		},
	}
	p.Bash.DeniedDirs = kubetail
	p.Bash.Links = cfg.Folders.Links
	return p
}

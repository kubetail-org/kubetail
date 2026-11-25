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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
)

// makeSubfolders makes each subfolder of Kubetail's folders that p names for a
// service, through an os.Root on the folder above it, so the check and the
// making are one step. A link there fails the start: Kubetail follows a link to
// its folders once, at start, and only at the layout's own.
func makeSubfolders(p paths) error {
	for _, dir := range []string{
		p.ChatsDir, p.MonitorDir, p.Cluster.KubestoreDir, p.LogsDir,
		p.Bash.KubectlDir, p.Bash.TmpDir, p.Bash.ShellDir, p.Bash.RunsDir,
	} {
		if err := makeSubfolder(dir); err != nil {
			return err
		}
	}
	return nil
}

func makeSubfolder(dir string) error {
	parent, name := filepath.Split(dir)
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	sub, err := rootdir.Open(root, name, true)
	if errors.Is(err, rootdir.ErrNotADirectory) {
		if info, lerr := root.Lstat(name); lerr == nil && realpath.IsLink(info) {
			return fmt.Errorf("%s is a link; link %s instead", dir, filepath.Clean(parent))
		}
		return fmt.Errorf("%s is not a folder", dir)
	}
	if err != nil {
		return err
	}
	return sub.Close()
}

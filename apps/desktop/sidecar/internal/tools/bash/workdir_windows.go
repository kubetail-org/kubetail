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

//go:build windows

package bash

import (
	"path/filepath"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

// checkWorkdir refuses a workdir no home can make valid: ~user, which only
// the shell can look up; C:x, relative to a directory only that drive
// remembers; and a rooted path on no drive, which only Git Bash can place (/tmp)
// or which is rooted nowhere (\x).
func checkWorkdir(workdir string) error {
	switch {
	case workdir == "~" || strings.HasPrefix(workdir, "~/") || strings.HasPrefix(workdir, `~\`):
		return nil
	case strings.HasPrefix(workdir, "~"):
		return errInput
	case filepath.IsAbs(workdir): // a drive path or a UNC path
		return nil
	case filepath.VolumeName(workdir) != "":
		return errInput
	}
	if _, ok := tools.GitBashDrive(workdir); ok {
		return nil
	}
	if strings.HasPrefix(workdir, "/") || strings.HasPrefix(workdir, `\`) {
		return errInput
	}
	return nil
}

// resolveWorkdir is the directory a command with this workdir starts in, in the
// native form CreateProcess takes: the workspace when it names none, and a
// relative workdir joined to it. ~ is the workspace for a sandboxed call and the
// home for one outside, and a sandboxed call must start under the workspace by
// name (errOutsideWorkspace). It is string work alone and never touches the
// filesystem: it runs before the user decides, and touching a UNC path would
// start an SMB login with the user's credentials.
func resolveWorkdir(home, workspace, workdir string, sandboxed bool) (string, error) {
	if err := checkWorkdir(workdir); err != nil {
		return "", err
	}
	tilde := home
	if sandboxed {
		tilde = workspace
	}
	var dir string
	switch {
	case workdir == "":
		dir = workspace
	case workdir == "~":
		dir = tilde
	case strings.HasPrefix(workdir, "~/") || strings.HasPrefix(workdir, `~\`):
		dir = filepath.Join(tilde, workdir[2:])
	case filepath.IsAbs(workdir):
		dir = filepath.Clean(workdir)
	default:
		if native, ok := tools.GitBashDrive(workdir); ok {
			dir = native
		} else {
			dir = filepath.Join(workspace, workdir)
		}
	}
	if _, under := fileguard.Under(workspace, dir); sandboxed && !under {
		return "", errOutsideWorkspace
	}
	return dir, nil
}

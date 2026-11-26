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

//go:build unix

package app

import (
	"context"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// shellPathResolver is Refresh PATH's resolver: the login shell run again in
// sb, with the denied-always list, read at each call, and Kubetail's directories
// shut to it, its TMPDIR under tmpDir.
func shellPathResolver(sb sandboxer, home string, kubetailDirs []string, tmpDir string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		return loginshell.Path(ctx, loginshell.In(sb, sb.Never(home), kubetailDirs, bash.TempDir(tmpDir)))
	}
}

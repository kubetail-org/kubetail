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

package kubeconfig

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// swapIn renames tmp over path, as an editor or a dotfiles manager saves, retrying
// while the rename is refused. os.Rename is MoveFileEx, which will not replace a file
// while any handle is open on it, whatever its share mode — and a running service
// opens path on every poll: Readlink and Stat open a link, a load opens a file. A
// Windows writer that saves by rename has to retry the same way.
func swapIn(t *testing.T, tmp, path string) {
	t.Helper()
	var err error
	require.Eventually(t, func() bool {
		err = os.Rename(tmp, path)
		return !errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}, testutil.Timeout, time.Millisecond)
	require.NoError(t, err)
}

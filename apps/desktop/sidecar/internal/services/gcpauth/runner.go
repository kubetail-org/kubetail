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

package gcpauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// maxOutput is the most of the helper's output read.
const maxOutput = 64 << 10

// errTooLong is output past maxOutput.
var errTooLong = errors.New("gcloud's answer is too long")

// runner runs program's config-helper with the sidecar's environment, so the
// account and configuration are those a GKE connection uses. Its stderr is
// discarded, and its arguments are constants. It never runs on native
// Windows, whose gcloud is a .cmd wrapper: that build has no sandbox and so
// no Google cloud platform (app's builtCloudPlatforms).
func runner(program string) func(ctx context.Context, refresh bool) ([]byte, error) {
	return func(ctx context.Context, refresh bool) ([]byte, error) {
		args := []string{"config", "config-helper", "--format=json", "--min-expiry=10m"}
		if refresh {
			args = append(args, "--force-auth-refresh")
		}
		cmd := exec.CommandContext(ctx, program, args...)
		cmd.WaitDelay = time.Second
		out := &capped{max: maxOutput}
		cmd.Stdout = out
		err := cmd.Run()
		if out.over {
			return nil, errTooLong
		}
		return out.buf.Bytes(), err
	}
}

// capped keeps at most max bytes, and fails the write past it, which ends the
// copy from the child. The buffer is a field, not embedded: its ReadFrom
// would let io.Copy go round Write.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		c.over = true
		return 0, io.ErrShortWrite
	}
	return c.buf.Write(p)
}

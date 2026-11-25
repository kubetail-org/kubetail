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

package azureauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// azBound is the longest one az runs.
const azBound = 30 * time.Second

// New is the service over the user's az, found on the sidecar's PATH at
// each call. It reads nothing until a run asks.
func New() *Service {
	return newService(commandRunner(exec.LookPath, azBound), configStamp(configDir), time.Now)
}

// ErrNotRun is an az that did not run to an answer: none found, or one cut
// off at its bound. Its failure says nothing of the user's login.
var ErrNotRun = errors.New("azureauth: az did not run to an answer")

// commandRunner runs the az look finds, which must be an absolute path, with
// the sidecar's environment, under bound, its standard error dropped.
func commandRunner(look func(string) (string, error), bound time.Duration) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		az, err := look("az")
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotRun, err)
		}
		if !filepath.IsAbs(az) {
			return nil, fmt.Errorf("%w: az resolved to %s, not an absolute path", ErrNotRun, az)
		}
		ctx, cancel := context.WithTimeout(ctx, bound)
		defer cancel()
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, az, args...)
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%w: %w", ErrNotRun, ctx.Err())
			}
			return nil, err
		}
		return out.Bytes(), nil
	}
}

// configDir is the user's az's configuration directory: AZURE_CONFIG_DIR,
// which the login shell sets on macOS, else ~/.azure.
func configDir() string {
	if dir := os.Getenv("AZURE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".azure")
}

// configStamp is a stamp of the user's azureProfile.json, config and
// clouds.config, which changes when any is written or appears.
func configStamp(dir func() string) func() string {
	return func() string {
		d := dir()
		var stamp string
		for _, name := range []string{"azureProfile.json", "config", "clouds.config"} {
			info, err := os.Stat(filepath.Join(d, name))
			if err != nil {
				stamp += "-|"
				continue
			}
			stamp += fmt.Sprintf("%d.%d|", info.ModTime().UnixNano(), info.Size())
		}
		return d + "|" + stamp
	}
}

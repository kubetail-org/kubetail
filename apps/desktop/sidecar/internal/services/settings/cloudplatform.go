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

package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// ErrNoCloudPlatformSwitch is a switch asked of a target that has none: the
// cluster.
var ErrNoCloudPlatformSwitch = errors.New("settings: no switch for this target")

// CloudPlatformSwitches is each cloud platform's switch: on, a sandboxed
// command reaches it as the user.
type CloudPlatformSwitches struct {
	AWS, GitHub, GCP, Azure bool
}

// On is target's switch; false for a target with none.
func (sw CloudPlatformSwitches) On(target permissions.Target) bool {
	switch target {
	case permissions.AWS:
		return sw.AWS
	case permissions.GitHub:
		return sw.GitHub
	case permissions.GCP:
		return sw.GCP
	case permissions.Azure:
		return sw.Azure
	}
	return false
}

// cloudPlatformSwitches is each cloud platform's switch, on until the user
// turns it off. Off is the strictest: a value Kubetail cannot use reaches no
// platform.
var cloudPlatformSwitches = map[permissions.Target]Entry[bool]{
	permissions.AWS:    {Key: "sandbox.aws.enabled", Default: true, Strictest: new(false)},
	permissions.GitHub: {Key: "sandbox.github.enabled", Default: true, Strictest: new(false)},
	permissions.GCP:    {Key: "sandbox.gcp.enabled", Default: true, Strictest: new(false)},
	permissions.Azure:  {Key: "sandbox.azure.enabled", Default: true, Strictest: new(false)},
}

// cloudPlatformSwitchKeys is the switches' keys, the arguments of their read.
func cloudPlatformSwitchKeys() []any {
	keys := make([]any, 0, len(cloudPlatformSwitches))
	for _, e := range cloudPlatformSwitches {
		keys = append(keys, e.Key)
	}
	return keys
}

// CloudPlatformSwitches reads every switch in one read, so no read fails
// partway.
func (s *Store) CloudPlatformSwitches(ctx context.Context) (CloudPlatformSwitches, error) {
	rows, err := s.stmts.Stmts().Query(ctx, stmtSelectCloudPlatformSwitches, cloudPlatformSwitchKeys()...)
	if err != nil {
		return CloudPlatformSwitches{}, fmt.Errorf("select cloud platform switches: %w", err)
	}
	stored, err := scanSettings[string](rows)
	if err != nil { // coverage:ignore scanSettings fails only on a scan or an I/O error, each exempt there
		return CloudPlatformSwitches{}, err
	}
	on := func(target permissions.Target) bool { return cloudPlatformSwitches[target].valueIn(stored) }
	return CloudPlatformSwitches{
		AWS:    on(permissions.AWS),
		GitHub: on(permissions.GitHub),
		GCP:    on(permissions.GCP),
		Azure:  on(permissions.Azure),
	}, nil
}

// SetCloudPlatformSwitch turns target's switch on or off.
func (s *Store) SetCloudPlatformSwitch(ctx context.Context, target permissions.Target, on bool) error {
	e, ok := cloudPlatformSwitches[target]
	if !ok {
		return ErrNoCloudPlatformSwitch
	}
	if err := writeSetting(ctx, s.stmts.Stmts(), e, "", on, s.now().UnixMilli()); err != nil {
		return fmt.Errorf("write cloud platform switch: %w", err)
	}
	return nil
}

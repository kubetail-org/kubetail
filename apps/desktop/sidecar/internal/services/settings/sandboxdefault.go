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
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// SandboxDefault is the sandbox switch a new chat starts with, in one scope.
// A chat copies it when it is created; nothing reads it for a chat that
// exists.
type SandboxDefault struct {
	// Disabled starts a new chat outside the sandbox.
	Disabled bool
	// Own is whether the cluster has a value of its own; false for every
	// cluster.
	Own bool
}

// ErrSandboxDefaultChanged is a reset that would land on a default other than
// the one its caller saw, so the caller would not have confirmed the one it
// lands on. Nothing is written.
var ErrSandboxDefaultChanged = errors.New("settings: the default a reset lands on changed")

// sandboxDefaultEntry is the default, sandboxed unless the user says
// otherwise. Sandboxed is the strictest: a value Kubetail cannot use starts a
// chat in the sandbox.
var sandboxDefaultEntry = Entry[bool]{Key: "sandbox.disabled", Default: false, Strictest: new(false), PerCluster: true}

// GetSandboxDefault is the default for cluster, "" for every cluster.
func (s *Store) GetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID) (SandboxDefault, error) {
	return readSandboxDefault(ctx, s.stmts.Stmts(), cluster)
}

// SetSandboxDefault writes the default for cluster, "" for every cluster, and
// answers the default it left. A cluster that is gone or awaiting deletion is
// ErrClusterGone.
func (s *Store) SetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID, disabled bool) (SandboxDefault, error) {
	return s.writeSandboxDefault(ctx, cluster, func(st stmts) error {
		return writeSetting(ctx, st, sandboxDefaultEntry, cluster, disabled, s.now().UnixMilli())
	}, nil)
}

// ResetSandboxDefault takes cluster back to every cluster's default, or every
// cluster's back to sandboxed, and answers the default it left. disabled is
// the default the caller saw it would land on; a reset that lands elsewhere
// is ErrSandboxDefaultChanged, since its caller confirmed a move outside, or
// skipped confirming one, for a value that is no longer the one it lands on.
func (s *Store) ResetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID, disabled bool) (SandboxDefault, error) {
	return s.writeSandboxDefault(ctx, cluster, func(st stmts) error {
		return resetSetting(ctx, st, sandboxDefaultEntry, cluster, s.now().UnixMilli())
	}, func(d SandboxDefault) error {
		if d.Disabled != disabled {
			return ErrSandboxDefaultChanged
		}
		return nil
	})
}

// writeSandboxDefault runs fn against cluster's row and reads back the default
// it left, in one transaction, so the answer is the write's. A non-nil check
// refuses the default it left, and the write with it.
func (s *Store) writeSandboxDefault(ctx context.Context, cluster apimeta.ClusterID, fn func(stmts) error, check func(SandboxDefault) error) (SandboxDefault, error) {
	var d SandboxDefault
	err := s.write(ctx, appdb.KeySandboxDefaults, func(st stmts) error {
		if err := requireCluster(ctx, st, cluster); err != nil {
			return err
		}
		if err := fn(st); err != nil {
			return err
		}
		var err error
		if d, err = readSandboxDefault(ctx, st, cluster); err != nil || check == nil {
			return err
		}
		return check(d)
	})
	return d, err
}

func readSandboxDefault(ctx context.Context, st stmts, cluster apimeta.ClusterID) (SandboxDefault, error) {
	disabled, scope, err := readSetting(ctx, st, sandboxDefaultEntry, cluster)
	if err != nil {
		return SandboxDefault{}, err
	}
	return SandboxDefault{Disabled: disabled, Own: scope == scopeOwn}, nil
}

// ListenForSandboxDefaultChanges notifies after each write of a default, in
// any scope. Listen before the first read.
func (s *Store) ListenForSandboxDefaultChanges() ChangeNotifications {
	return s.db.ListenForChanges(appdb.KeySandboxDefaults)
}

// requireCluster refuses a cluster that is gone or awaiting deletion; ""
// names every cluster and passes.
func requireCluster(ctx context.Context, st stmts, cluster apimeta.ClusterID) error {
	if cluster == "" {
		return nil
	}
	_, err := clusterHasKubeContext(ctx, st, cluster)
	return err
}

// GetSandboxDefault is the default for cluster; every cluster's is sandboxed on
// a machine with no sandbox, whatever a row says.
func (s *Service) GetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID) (SandboxDefault, error) {
	if s.zones == nil {
		return SandboxDefault{}, nil
	}
	return s.Store.GetSandboxDefault(ctx, cluster)
}

// SetSandboxDefault writes the default, ErrNoSandbox on a machine with none.
func (s *Service) SetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID, disabled bool) (SandboxDefault, error) {
	if s.zones == nil {
		return SandboxDefault{}, ErrNoSandbox
	}
	return s.Store.SetSandboxDefault(ctx, cluster, disabled)
}

// ResetSandboxDefault resets the default, ErrNoSandbox on a machine with none.
func (s *Service) ResetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID, disabled bool) (SandboxDefault, error) {
	if s.zones == nil {
		return SandboxDefault{}, ErrNoSandbox
	}
	return s.Store.ResetSandboxDefault(ctx, cluster, disabled)
}

// SelectSandboxDefault reads the default's rows for one cluster and every
// cluster: its arguments are SandboxDefaultArgs, and ScanSandboxDefault reads
// its rows. agent/chat reads it in the transaction that creates a chat.
const SelectSandboxDefault = selectScopedSettingQuery

// SandboxDefaultArgs is SelectSandboxDefault's arguments for cluster.
func SandboxDefaultArgs(cluster apimeta.ClusterID) []any {
	return []any{string(cluster), sandboxDefaultEntry.Key}
}

// ScanSandboxDefault is whether a new chat on cluster starts outside the
// sandbox, from SelectSandboxDefault's rows. It closes rows.
func ScanSandboxDefault(rows *sql.Rows, cluster apimeta.ClusterID) (bool, error) {
	scopes, err := scanSettings[apimeta.ClusterID](rows)
	if err != nil { // coverage:ignore scanSettings fails only past a row that read
		return false, fmt.Errorf("scan sandbox default: %w", err)
	}
	disabled, _ := sandboxDefaultEntry.valueFor(scopes, cluster)
	return disabled, nil
}

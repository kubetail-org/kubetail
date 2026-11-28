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

package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/deltafold"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// The triage setting is the model each new incident is triaged with, for every
// target; unset, nothing is triaged unasked. It is a row of settings that
// services/settings encodes, which the monitor reads in each fold's transaction
// and writes with the cancel its change owes.

// ErrInvalidTriageModel is a triage model with no provider or no model id.
var ErrInvalidTriageModel = errors.New("monitor: a triage model names its provider and its model")

// SetTriageModel writes the triage setting, nil to turn automatic triage off,
// and cancels the automatic triages queued under the one it replaces.
// ErrInvalidTriageModel for a model with no ids; ErrStopping once the monitor
// stops.
func (m *Monitor) SetTriageModel(ctx context.Context, model *TriageModel) error {
	if err := m.enter(); err != nil {
		return err
	}
	defer m.wg.Done()
	return m.store.setTriageModel(ctx, model)
}

// WatchTriageModelState streams the triage setting, nil while unset: at once,
// then again after each write.
func (m *Monitor) WatchTriageModelState(ctx context.Context) (*Stream[*TriageModel], error) {
	return stream(ctx, m, func(ctx context.Context, out chan<- *TriageModel) error {
		// Listen, then read: a value read first would miss a write in between.
		rx := m.store.db.ListenForChanges(appdb.KeySettings)
		defer rx.Close()
		for {
			model, err := m.store.triageModel(ctx, m.store.set.Stmts())
			if err != nil {
				return err
			}
			if !deltafold.Send(ctx, out, model) { // coverage:ignore a subscriber that leaves while a frame waits unread; no test can order its leaving after the read, and leaving between writes is tested
				return nil
			}
			if _, err := rx.RecvContext(ctx); err != nil {
				return nil
			}
		}
	})
}

// triageModel is the triage setting as st reads it, nil while unset or set to a
// value Kubetail cannot use.
func (s *store) triageModel(ctx context.Context, st stmts) (*TriageModel, error) {
	var raw any
	err := st.QueryRow(ctx, stmtSelectTriageModel).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read triage model: %w", err)
	}
	stored := settings.MonitoringTriageModelOf(raw)
	if stored == nil {
		return nil, nil
	}
	return &TriageModel{ProviderID: stored.ProviderID, ModelID: stored.ModelID, Effort: stored.Effort}, nil
}

// setTriageModel writes model as the triage setting in its own transaction, and
// notifies after the commit.
func (s *store) setTriageModel(ctx context.Context, model *TriageModel) error {
	var cancelled bool
	err := s.set.InTx(ctx, func(st stmts) error {
		var err error
		cancelled, err = s.writeTriageModel(ctx, st, model)
		return err
	})
	if err != nil {
		return err
	}
	s.notifyTriageModel(cancelled)
	return nil
}

// writeTriageModel writes model as the triage setting inside st, nil to unset.
// A write that changes it fails every queued automatic triage whose run has not
// started, as setting_changed, alerting its incident: a triage runs only under
// the setting it was queued under. It answers whether it failed any.
func (s *store) writeTriageModel(ctx context.Context, st stmts, model *TriageModel) (bool, error) {
	var stored *settings.MonitoringTriageModel
	if model != nil {
		stored = &settings.MonitoringTriageModel{ProviderID: model.ProviderID, ModelID: model.ModelID, Effort: model.Effort}
	}
	at := s.clock().UnixMilli()
	args, err := settings.MonitoringTriageModelValues(stored, at)
	if err != nil {
		return false, ErrInvalidTriageModel
	}
	current, err := s.triageModel(ctx, st)
	if err != nil { // coverage:ignore a read inside a write transaction fails only on a closed pool, which outlives the monitor
		return false, err
	}
	if _, err := st.Exec(ctx, stmtUpsertTriageModel, args...); err != nil { // coverage:ignore a write inside a write transaction fails only on a closed pool, which outlives the monitor
		return false, fmt.Errorf("write triage model: %w", err)
	}
	if sameTriageModel(current, model) {
		return false, nil
	}
	res, err := st.Exec(ctx, stmtCancelAutomaticTriages, at)
	if err != nil { // coverage:ignore a write inside a write transaction fails only on a closed pool, which outlives the monitor
		return false, fmt.Errorf("cancel automatic triages: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := st.Exec(ctx, stmtAdvanceAlertSeq); err != nil { // coverage:ignore a write inside a write transaction fails only on a closed pool, which outlives the monitor
		return false, fmt.Errorf("write alert seq: %w", err)
	}
	return n > 0, nil
}

// sameTriageModel is whether a and b name one model, or are both unset.
func sameTriageModel(a, b *TriageModel) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// notifyTriageModel notifies a committed write of the triage setting, and the
// incidents when it cancelled a triage.
func (s *store) notifyTriageModel(cancelled bool) {
	s.db.Notify(appdb.KeySettings)
	if cancelled {
		s.db.Notify(appdb.KeyIncidents)
	}
}

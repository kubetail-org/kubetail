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

import "errors"

// The monitoring triage model is the key monitoring.triage.model, the model
// each new incident is triaged with; unset, nothing is triaged unasked. Unset
// is the default and the strictest: a value Kubetail cannot use spends nothing.
// agent/monitor reads and writes it on its own transactions, since a write
// cancels queued triages in the same one, through what this file exports.

// MonitoringTriageModel is a model by the ids the catalog names it with.
type MonitoringTriageModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Effort     string `json:"effort,omitempty"`
}

var errNoTriageModel = errors.New("a triage model names its provider and its model")

const monitoringTriageModelKey = "monitoring.triage.model"

var monitoringTriageModel = Entry[MonitoringTriageModel]{
	Key:       monitoringTriageModelKey,
	Strictest: new(MonitoringTriageModel{}),
	Check: func(m MonitoringTriageModel) error {
		if m.ProviderID == "" || m.ModelID == "" {
			return errNoTriageModel
		}
		return nil
	},
}

// SelectMonitoringTriageModel reads the setting's value for every target, one
// row or none, through the index.
const SelectMonitoringTriageModel = `SELECT value FROM settings
	WHERE ifnull(cluster_id, '') = '' AND key = '` + monitoringTriageModelKey + `'`

// UpsertMonitoringTriageModel writes the setting; its arguments are
// MonitoringTriageModelValues.
const UpsertMonitoringTriageModel = upsertSettingQuery

// MonitoringTriageModelValues is UpsertMonitoringTriageModel's arguments for m,
// nil to unset: a SettingRefusal for a model with no provider or no model id.
func MonitoringTriageModelValues(m *MonitoringTriageModel, at int64) ([]any, error) {
	var raw any
	if m != nil {
		var err error
		if raw, err = monitoringTriageModel.encode(*m); err != nil {
			return nil, err
		}
	}
	return []any{monitoringTriageModel.Key, "", raw, at}, nil
}

// MonitoringTriageModelOf is the model a stored value names, nil for a NULL, no
// row, or a value Kubetail cannot use.
func MonitoringTriageModelOf(raw any) *MonitoringTriageModel {
	if raw == nil {
		return nil
	}
	m := monitoringTriageModel.valueOf(raw)
	if m == (MonitoringTriageModel{}) {
		return nil
	}
	return &m
}

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

// Package toolsettings keeps the user's settings of the model's tools, as
// rows of app.db's settings, a key per tool, spelled by services/settings. A
// tool is on unless a row turns it off, and a setting only takes a tool out
// of a turn's offer; it never adds one.
package toolsettings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/amorey/gochan/watch"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// A Group is what a tool does, as Settings groups the tools.
type Group string

const (
	GroupCommands Group = "commands"
	GroupFiles    Group = "files"
	GroupWeb      Group = "web"
	GroupCluster  Group = "cluster"
	GroupLogs     Group = "logs"
	GroupMemory   Group = "memory"
	GroupAgents   Group = "agents"
)

// A Listing is a tool a chat can be offered, as Settings lists it.
type Listing struct {
	Name        string // the tool's Name(), which its setting is stored under
	Label       string // what Settings calls it
	Group       Group
	Description string // what it does, in one line
	AlwaysOn    bool   // on whatever its row says; Set refuses to turn it off
}

// A Setting is a tool and whether turns are offered it.
type Setting struct {
	Listing
	Enabled bool
}

// ErrUnknownTool is a Set of a name the service lists no tool of.
var ErrUnknownTool = errors.New("toolsettings: no listed tool has that name")

// ErrAlwaysOn is a Set turning off a tool listed AlwaysOn.
var ErrAlwaysOn = errors.New("toolsettings: that tool cannot be turned off")

// Service owns the tools' rows of settings. Only the setting for every
// cluster is read and written.
type Service struct {
	store    *sqlstmt.Set[stmtID]
	listings []Listing
	now      func() time.Time
	mu       sync.Mutex // orders each write with the list it publishes
	list     []Setting  // the list last published; guarded by mu
	hub      *watch.Hub[[]Setting]
}

// New prepares the statements over the app's DB and reads the settings of
// listings, in the order Settings draws them. now stamps each write.
func New(db *appdb.DB, listings []Listing, now func() time.Time) (*Service, error) {
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare toolsettings statements: %w", err)
	}
	s := &Service{store: st, listings: listings, now: now}
	list, err := s.List(context.Background())
	if err != nil { // coverage:ignore the select prepared on the table a moment ago
		return nil, errors.Join(err, st.Close())
	}
	s.list, s.hub = list, watch.New(list)
	return s, nil
}

// Close releases the prepared statements.
func (s *Service) Close() error { return s.store.Close() }

// List is every tool with its setting, in the order Settings draws them.
func (s *Service) List(ctx context.Context) ([]Setting, error) {
	off, err := disabled(ctx, s.store.Stmts())
	if err != nil {
		return nil, err
	}
	out := make([]Setting, 0, len(s.listings))
	for _, l := range s.listings {
		out = append(out, Setting{Listing: l, Enabled: l.AlwaysOn || !slices.Contains(off, l.Name)})
	}
	return out, nil
}

// Set turns the tool named name on or off for every cluster. Turning a tool
// back on writes the row rather than deleting it, so updated_at orders every
// change.
func (s *Service) Set(ctx context.Context, name string, enabled bool) error {
	i := slices.IndexFunc(s.listings, func(l Listing) bool { return l.Name == name })
	if i < 0 {
		return ErrUnknownTool
	}
	if s.listings[i].AlwaysOn && !enabled {
		return ErrAlwaysOn
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := upsert(ctx, s.store.Stmts(), name, enabled, s.now().UnixMilli()); err != nil {
		return err
	}
	// The published list changes from the write alone, so nothing after the
	// commit can fail and leave subscribers on the old list. A copy, since
	// subscribers hold the one sent before.
	s.list = slices.Clone(s.list)
	for i := range s.list {
		if s.list[i].Name == name {
			s.list[i].Enabled = enabled
		}
	}
	s.hub.Sender().Send(s.list) //nolint:errcheck // Send never blocks, and the hub is never closed
	return nil
}

// WatchSettingsState streams every tool with its setting: current on subscribe, then
// the list after each change.
func (s *Service) WatchSettingsState() (<-chan []Setting, func()) {
	rx := s.hub.Receiver()
	return rx.Chan(), rx.Close
}

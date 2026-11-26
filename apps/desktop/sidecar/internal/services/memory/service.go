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

// Package memory keeps the notes the model and the user write for later chats:
// one fact each, for one cluster or for every cluster. It owns the memories table
// and the rules a note keeps, serves the dialog's watch and writes and the model's
// writes, and renders the section a chat's context block carries.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// MemoryID identifies one memory: a UUIDv7 minted by appdb.
type MemoryID string

// Author is who last wrote a memory.
type Author string

const (
	AuthorModel Author = "model"
	AuthorUser  Author = "user"
)

// Memory is one note, as the store and the wire hold it.
type Memory struct {
	ID        MemoryID
	ClusterID *apimeta.ClusterID // nil: every cluster
	ServerUID *string            // the cluster's UID at the last write; nil when unknown or global
	Name      string
	Body      string
	WrittenBy Author
	ChatID    *apimeta.ChatID
	CreatedAt time.Time
	UpdatedAt time.Time
}

var (
	// ErrBadInput is a note out of shape; it wraps the *FieldError naming the field.
	ErrBadInput = errors.New("memory: bad input")
	// ErrClusterGone is a write under a cluster that is missing or marked for
	// deletion: its row's delete would take the memory anyway.
	ErrClusterGone = errors.New("memory: cluster not found")
	// ErrNameTaken is the user's memory under a name its scope already holds.
	ErrNameTaken = errors.New("memory: name taken")
	// ErrUserNote is a model's save or forget of a name the user's note holds.
	ErrUserNote = errors.New("memory: the user's note")
	// ErrNotFound is a name the cluster does not see, or an id no memory has.
	ErrNotFound = errors.New("memory: not found")
	// ErrFull is a write that would take its scope past ScopeBudget.
	ErrFull = errors.New("memory: scope full")
	// ErrSecret is a note holding a credential.
	ErrSecret = errors.New("memory: holds a credential")
	// ErrStopping is a watch opened as the service stops.
	ErrStopping = errors.New("memory: stopping")
)

// ServerUIDReader reads a cluster's last-probed kube-system UID at each call, ""
// when it has none: a rebuilt cluster's UID changes while the app runs. app
// implements it over the cluster service.
type ServerUIDReader interface {
	ServerUID(ctx context.Context, clusterID apimeta.ClusterID) string
}

// Service is the memory store: the chats' notes, the user's dialog, and the section
// a chat's context block carries.
type Service interface {
	Start(ctx context.Context) (func(context.Context) error, error)
	Close() error

	// Visible is every memory cluster sees: its own, then the global ones, each by name.
	Visible(ctx context.Context, cluster apimeta.ClusterID) ([]Memory, error)
	WatchCollectionByCluster(ctx context.Context, clusterID apimeta.ClusterID) (*Stream[MemoryCollectionWatchFrame], error)

	// Create, Update and Delete are the user's writes, from the dialog.
	Create(ctx context.Context, in Input) (Memory, error)
	Update(ctx context.Context, id MemoryID, in Input) (Memory, error)
	Delete(ctx context.Context, id MemoryID) error

	// Save, Forget and their Everywhere pair are the model's writes, from chatID:
	// each reaches only its own scope and refuses a name the user's note holds.
	// SaveEverywhere and ForgetEverywhere change what every cluster's chats read.
	Save(ctx context.Context, cluster apimeta.ClusterID, name, body string, chatID apimeta.ChatID) error
	Forget(ctx context.Context, cluster apimeta.ClusterID, name string) error
	SaveEverywhere(ctx context.Context, name, body string, chatID apimeta.ChatID) error
	ForgetEverywhere(ctx context.Context, name string) error
	// CheckSaveEverywhere and CheckForgetEverywhere are what the two would answer
	// now, with nothing changed.
	CheckSaveEverywhere(ctx context.Context, name, body string) error
	CheckForgetEverywhere(ctx context.Context, name string) error

	// Section is every note cluster sees, whole, for its chats' context block.
	Section(ctx context.Context, cluster apimeta.ClusterID) json.RawMessage
}

var _ Service = (*service)(nil)

// New builds the service over the app's DB and each cluster's last-probed UID.
// Nothing runs until Start.
func New(db *appdb.DB, uids ServerUIDReader) (Service, error) { return newService(db, uids) }

type service struct {
	db    *appdb.DB
	store *sqlstmt.Set[stmtID]
	uids  ServerUIDReader

	// Shutdown: wg holds the watch pumps, which join through enter.
	wg       sync.WaitGroup
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	stopped  chan struct{}
	stopOnce sync.Once

	now func() time.Time
}

// newService prepares the statements over db. Nothing runs until Start.
func newService(db *appdb.DB, uids ServerUIDReader) (*service, error) {
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare memory statements: %w", err)
	}
	s := &service{db: db, store: st, uids: uids, stopped: make(chan struct{}), now: time.Now}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

// Start has no startup work; the func it returns ends the watch pumps and joins them.
func (s *service) Start(context.Context) (func(context.Context) error, error) {
	return s.stop, nil
}

func (s *service) stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		close(s.stopped)
		s.mu.Unlock()
	})
	return drain.WithContext(ctx, s.wg.Wait)
}

// enter joins the group stop waits on, ErrStopping once the service is stopping.
// The caller owes the matching wg.Done.
func (s *service) enter() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopped:
		return ErrStopping
	default:
		s.wg.Add(1)
		return nil
	}
}

// Close releases the prepared statements. It runs after stop has joined every reader.
func (s *service) Close() error { return s.store.Close() }

// checkCluster refuses a write under a cluster that is missing or marked. Inside
// the write transaction, so a write and a cluster's mark are serialized.
func checkCluster(ctx context.Context, st stmts, cluster apimeta.ClusterID) error {
	ok, err := exists(ctx, st, stmtSelectClusterAccepts, string(cluster))
	if err != nil {
		return err
	}
	if !ok {
		return ErrClusterGone
	}
	return nil
}

// checkName refuses a name another memory holds in m's scope. A cluster's note
// and a note for every cluster may share one.
func checkName(ctx context.Context, st stmts, m Memory) error {
	taken, err := exists(ctx, st, stmtSelectNameInScope, scopeKey(m.ClusterID), m.Name, string(m.ID))
	if err != nil {
		return err
	}
	if taken {
		return ErrNameTaken
	}
	return nil
}

// checkRoom refuses a write that would take m's scope past ScopeBudget: the
// scope's notes after it, measured as the section encodes them. A move is
// measured against the scope it enters.
func checkRoom(ctx context.Context, st stmts, m Memory) error {
	ms, err := scopeNotes(ctx, st, m.ClusterID)
	if err != nil {
		return err
	}
	after := []Memory{m}
	for _, other := range ms {
		if other.ID != m.ID {
			after = append(after, other)
		}
	}
	if sectionSize(entries(after)) > ScopeBudget {
		return ErrFull
	}
	return nil
}

// Visible is every memory cluster sees: its own, then the global ones, each by name.
func (s *service) Visible(ctx context.Context, cluster apimeta.ClusterID) ([]Memory, error) {
	return visible(ctx, s.store.Stmts(), cluster)
}

// Save is the model's write, from chatID: body under name in cluster.
func (s *service) Save(ctx context.Context, cluster apimeta.ClusterID, name, body string, chatID apimeta.ChatID) error {
	return s.save(ctx, &cluster, name, body, chatID, false)
}

// SaveEverywhere is the model's write for every cluster, from chatID.
func (s *service) SaveEverywhere(ctx context.Context, name, body string, chatID apimeta.ChatID) error {
	return s.save(ctx, nil, name, body, chatID, false)
}

// CheckSaveEverywhere is what SaveEverywhere would answer now, with nothing written.
func (s *service) CheckSaveEverywhere(ctx context.Context, name, body string) error {
	return s.save(ctx, nil, name, body, "", true)
}

// save writes the model's note under name in one scope, a cluster's or the
// global ones for nil, replacing the model's own note of that name there. It
// never reaches the other scope. A name the user's note holds in the scope is
// ErrUserNote: the user's notes are standing requests the model does not change.
// A dry save runs every check and writes nothing.
func (s *service) save(ctx context.Context, cluster *apimeta.ClusterID, name, body string, chatID apimeta.ChatID, dry bool) error {
	if err := checkNote(name, body); err != nil {
		return err
	}
	m := Memory{ClusterID: cluster, Name: name, Body: body, WrittenBy: AuthorModel}
	if chatID != "" {
		m.ChatID = &chatID
	}
	m.ServerUID = s.stamp(ctx, cluster)
	err := s.store.InTx(ctx, func(st stmts) error {
		if cluster != nil {
			if err := checkCluster(ctx, st, *cluster); err != nil {
				return err
			}
		}
		prev, err := scopeByName(ctx, st, cluster, name)
		if err != nil {
			return err
		}
		if prev != nil && prev.WrittenBy == AuthorUser {
			return ErrUserNote
		}
		if dry {
			return checkRoom(ctx, st, s.placed(m, prev))
		}
		_, err = s.put(ctx, st, m, prev)
		return err
	})
	if err != nil || dry {
		return err
	}
	s.db.Notify(appdb.KeyMemories)
	return nil
}

// Input is the user's memory, from the dialog, with the scope as a cluster: nil
// for every cluster.
type Input struct {
	ClusterID *apimeta.ClusterID
	Name      string
	Body      string
}

// Create writes a new memory of the user's.
func (s *service) Create(ctx context.Context, in Input) (Memory, error) {
	return s.write(ctx, nil, in)
}

// Update rewrites the user's memory by id. It can rename it and move it either way.
func (s *service) Update(ctx context.Context, id MemoryID, in Input) (Memory, error) {
	return s.write(ctx, &id, in)
}

// write is Create, with a nil id, and Update.
func (s *service) write(ctx context.Context, id *MemoryID, in Input) (Memory, error) {
	if err := checkNote(in.Name, in.Body); err != nil {
		return Memory{}, err
	}
	m := Memory{ClusterID: in.ClusterID, Name: in.Name, Body: in.Body, WrittenBy: AuthorUser}
	m.ServerUID = s.stamp(ctx, m.ClusterID)
	var out Memory
	err := s.store.InTx(ctx, func(st stmts) error {
		var (
			prev *Memory
			err  error
		)
		if in.ClusterID != nil {
			if err = checkCluster(ctx, st, *in.ClusterID); err != nil {
				return err
			}
		}
		if id != nil {
			if prev, err = memoryByID(ctx, st, *id); err != nil {
				return err
			}
			if prev == nil {
				return ErrNotFound
			}
			m.ID = prev.ID
		}
		if err = checkName(ctx, st, m); err != nil {
			return err
		}
		out, err = s.put(ctx, st, m, prev)
		return err
	})
	if err != nil {
		return Memory{}, err
	}
	s.db.Notify(appdb.KeyMemories)
	return out, nil
}

// Delete removes the user's memory by id.
func (s *service) Delete(ctx context.Context, id MemoryID) error {
	var found bool
	err := s.store.InTx(ctx, func(st stmts) (err error) {
		found, err = deleteMemory(ctx, st, id)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	s.db.Notify(appdb.KeyMemories)
	return nil
}

// stamp is the server UID a write to cluster records: which physical cluster the
// note was written against, kept because it cannot be recovered later. Nil for
// every cluster, or a cluster never identified. Read before the write
// transaction, since it asks another service.
func (s *service) stamp(ctx context.Context, cluster *apimeta.ClusterID) *string {
	if cluster == nil {
		return nil
	}
	if uid := s.uids.ServerUID(ctx, *cluster); uid != "" {
		return &uid
	}
	return nil
}

// put writes m over prev, or as a new memory when prev is nil, and returns the row
// as written. Inside the caller's transaction. The model's save needs no name
// check: its lookup already found whatever holds the name in its scope.
func (s *service) put(ctx context.Context, st stmts, m Memory, prev *Memory) (Memory, error) {
	m = s.placed(m, prev)
	if err := checkRoom(ctx, st, m); err != nil {
		return Memory{}, err
	}
	if prev == nil {
		return m, insertMemory(ctx, st, m)
	}
	return m, updateMemory(ctx, st, m)
}

// placed is m as it would be written over prev, or as a new memory when prev is
// nil: prev's id and creation time, else fresh ones, and updated now.
func (s *service) placed(m Memory, prev *Memory) Memory {
	at := fromMillis(millis(s.now()))
	if prev != nil {
		m.ID, m.CreatedAt = prev.ID, prev.CreatedAt
	} else {
		m.ID, m.CreatedAt = MemoryID(appdb.NewID()), at
	}
	m.UpdatedAt = at
	return m
}

// Forget is the model's delete of its own note under name in cluster.
func (s *service) Forget(ctx context.Context, cluster apimeta.ClusterID, name string) error {
	return s.forget(ctx, &cluster, name, false)
}

// ForgetEverywhere is the model's delete of its own note for every cluster.
func (s *service) ForgetEverywhere(ctx context.Context, name string) error {
	return s.forget(ctx, nil, name, false)
}

// CheckForgetEverywhere is what ForgetEverywhere would answer now, with nothing deleted.
func (s *service) CheckForgetEverywhere(ctx context.Context, name string) error {
	return s.forget(ctx, nil, name, true)
}

// forget deletes the model's own note under name in one scope, a cluster's or
// the global ones for nil. It never reaches the other scope. A name the user's
// note holds in the scope is ErrUserNote. A dry forget runs every check and
// deletes nothing.
func (s *service) forget(ctx context.Context, cluster *apimeta.ClusterID, name string, dry bool) error {
	if err := checkNoteName(name); err != nil {
		return err
	}
	err := s.store.InTx(ctx, func(st stmts) error {
		if cluster != nil {
			if err := checkCluster(ctx, st, *cluster); err != nil {
				return err
			}
		}
		m, err := scopeByName(ctx, st, cluster, name)
		if err != nil {
			return err
		}
		if m == nil {
			return ErrNotFound
		}
		if m.WrittenBy == AuthorUser {
			return ErrUserNote
		}
		if dry {
			return nil
		}
		_, err = deleteMemory(ctx, st, m.ID)
		return err
	})
	if err != nil || dry {
		return err
	}
	s.db.Notify(appdb.KeyMemories)
	return nil
}

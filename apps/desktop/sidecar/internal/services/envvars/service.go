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

package envvars

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
)

// Var is a variable the user gives their commands. ClusterID is its scope,
// one cluster's commands or, when "", every cluster's. Refusal is why no
// command gets it, "" when one does.
type Var struct {
	ClusterID   apimeta.ClusterID
	Name, Value string
	Target      Target
	Refusal     string
}

// maxVars is the most variables a scope keeps.
const maxVars = 100

var (
	// ErrFull is a new variable in a scope that holds maxVars.
	ErrFull = Refusal(fmt.Sprintf("The list holds at most %d variables.", maxVars))
	// ErrNotSet is a Remove of a name its scope does not hold.
	ErrNotSet = Refusal("That variable is not set.")
	// ErrClusterGone is a variable set for a cluster that is missing or marked
	// for deletion.
	ErrClusterGone = errors.New("envvars: cluster not found")
)

// Service owns the env_vars table.
type Service struct {
	store *sqlstmt.Set[stmtID]
	now   func() time.Time
}

// New prepares the statements over the app's DB. now stamps each write.
func New(db *appdb.DB, now func() time.Time) (*Service, error) {
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		return nil, fmt.Errorf("prepare envvars statements: %w", err)
	}
	return &Service{store: st, now: now}, nil
}

// Close releases the prepared statements.
func (s *Service) Close() error { return s.store.Close() }

// List is one scope's variables, in the order added.
func (s *Service) List(ctx context.Context, cluster apimeta.ClusterID) ([]Var, error) {
	return vars(ctx, s.store.Stmts(), stmtSelectScope, cluster)
}

// Set sets v's value and target, in its place when its scope lists the name,
// else at the scope's end. A variable Check refuses is its Refusal.
func (s *Service) Set(ctx context.Context, v Var) error {
	if err := Check(v.Name, v.Value, v.Target); err != nil {
		return err
	}
	at := s.now().UnixMilli()
	return s.store.InTx(ctx, func(st stmts) error {
		if v.ClusterID != "" {
			if err := checkCluster(ctx, st, v.ClusterID); err != nil {
				return err
			}
		}
		if done, err := update(ctx, st, v, at); err != nil || done {
			return err
		}
		if done, err := insert(ctx, st, v, at); err != nil || done {
			return err
		}
		return ErrFull
	})
}

// Remove removes a scope's variable, a refused one too.
func (s *Service) Remove(ctx context.Context, cluster apimeta.ClusterID, name string) error {
	done, err := remove(ctx, s.store.Stmts(), Var{ClusterID: cluster, Name: name})
	if err == nil && !done {
		return ErrNotSet
	}
	return err
}

// For is what a command of a chat on cluster is given, NAME=value: every
// cluster's variables in the order added, a cluster's own value replacing one
// of the same name in place, its other variables after. Only the variables
// whose target is the command's side of the sandbox, and none Check refuses
// for that side; a cluster's refused value still replaces every cluster's, so
// the command gets neither. cluster "" is a command with no cluster.
func (s *Service) For(ctx context.Context, cluster apimeta.ClusterID, sandboxed bool) ([]string, error) {
	target := Outside
	if sandboxed {
		target = Sandbox
	}
	all, err := vars(ctx, s.store.Stmts(), stmtSelectFor, cluster, target)
	if err != nil {
		return nil, err
	}
	env := []string{}
	place := map[string]int{}
	for _, v := range all {
		kv := v.Name + "=" + v.Value
		if Check(v.Name, v.Value, target) != nil {
			kv = "" // dropped below
		}
		if i, ok := place[v.Name]; ok {
			env[i] = kv
			continue
		}
		place[v.Name] = len(env)
		env = append(env, kv)
	}
	return slices.DeleteFunc(env, func(kv string) bool { return kv == "" }), nil
}

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

// Package session is one agent run's identity and policy. A leaf: a proxy
// imports it to read the session a run's token maps to, so it imports no proxy.
package session

import (
	"context"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// Kind is what kind of agent a session runs: the principal its runs' actions
// carry, so there is one list.
type Kind = permissions.Principal

const (
	Chat     = permissions.Chat
	Subagent = permissions.Subagent
	Monitor  = permissions.Monitor
	Console  = permissions.Console
)

// Network is why a sandboxed command reaches the internet. It is stored on the
// call's row as it is spelled here.
type Network string

const (
	NoNetwork       Network = ""
	NetworkChat     Network = "chat"     // the chat's switch
	NetworkTurn     Network = "turn"     // the turn's toggle
	NetworkApproved Network = "approved" // the user approved the call's own request
	NetworkConsole  Network = "console"  // the Sandbox console's Network checkbox
)

// Session is one agent run's policy: what its tools and the proxies its runs
// serve read to decide what it may do. The chat and cluster it runs in are the
// runtime's. chat builds a chat's at the start of each turn; a subagent's is
// Narrow of its parent's.
type Session struct {
	Kind    Kind
	Outside bool // the user switched the chat to run outside the sandbox
	// Policy is the mode and rules an action on target in account is decided
	// by: the account's mode, and every rule. It is read live on every
	// decision, so a mode or rule changed meanwhile applies to the next one.
	// What a principal may never do is the boundary's, which no policy lifts.
	Policy func(ctx context.Context, target permissions.Target, account string) permissions.Policy
	// Network is the network a sandboxed command starting now has: NetworkChat
	// while the chat's switch is on, read live, else NetworkTurn for the turn's
	// toggle, else NoNetwork. Nil is a session that never has network.
	Network func(context.Context) Network
	// Folders is the session's folder grants, the chat's joined with the always
	// ones, each checked when read. A read that fails is an error, never an
	// empty list. Nil reads none: only the chat's builder sets it, and every
	// reader goes through GrantedFolders.
	Folders func(context.Context) ([]Folder, error)
}

// Folder is one folder a session's commands may read, or read and write.
type Folder struct {
	Path  string
	Write bool
}

// PolicyFor is the policy an action on target in account is decided by: the
// session's, read now, joined by the rules its command's answers added. A
// session with no Policy is read-only with no rules.
func (s Session) PolicyFor(ctx context.Context, target permissions.Target, account string, commandRules []permissions.Rule) permissions.Policy {
	if s.Policy == nil {
		return permissions.Policy{Mode: permissions.ModeReadOnly}
	}
	p := s.Policy(ctx, target, account)
	p.Rules = append(slices.Clip(p.Rules), commandRules...)
	return p
}

// GrantedFolders is the session's folders, none when it has no Folders.
func (s Session) GrantedFolders(ctx context.Context) ([]Folder, error) {
	if s.Folders == nil {
		return nil, nil
	}
	return s.Folders(ctx)
}

// Narrow is a subagent's session under parent: the parent's, as Kind Subagent
// when the parent is a chat, the one kind that spawns one; any other keeps its
// kind, so a narrowed monitor stays a monitor. The switch is copied at spawn, so a subagent started outside the sandbox
// stays outside, and it decides by its parent's Policy and Network, and reads
// its parent's Folders: one spawned in a turn with the toggle keeps it for its
// whole life.
func Narrow(parent Session) Session {
	if parent.Kind == Chat {
		parent.Kind = Subagent
	}
	return parent
}

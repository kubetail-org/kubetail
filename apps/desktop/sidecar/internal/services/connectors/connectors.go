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

// Package connectors is the vocabulary every connector speaks. A connector is
// one package beside this one (chatgpt, openrouter, aws, github, google,
// kubetailcloud); what they share is under internal/.
package connectors

import "errors"

// Phase is where a connector is.
type Phase string

const (
	// Restoring is a launch that has not read the keyring yet: neither signed
	// out nor connected. Only New enters it, and only the restore leaves it.
	Restoring Phase = "Restoring"
	SignedOut Phase = "SignedOut"
	SigningIn Phase = "SigningIn"
	Connected Phase = "Connected"
	Failed    Phase = "Failed"
)

// Failure is why the last attempt, session or key ended.
type Failure string

const (
	Declined        Failure = "Declined"
	ScopeNotGranted Failure = "ScopeNotGranted"
	AccountMismatch Failure = "AccountMismatch"
	TimedOut        Failure = "TimedOut"
	Unavailable     Failure = "Unavailable"
	Refused         Failure = "Refused"
	NotSaved        Failure = "NotSaved"
	// NotDeleted is a Disconnect whose keyring delete failed: the key is out of
	// memory but still in the keyring, and returns at the next launch.
	NotDeleted   Failure = "NotDeleted"
	SessionEnded Failure = "SessionEnded"
	// KeyRejected is a key the provider confirmed it no longer honours.
	KeyRejected Failure = "KeyRejected"
)

// State is a connector as every window draws it. It holds no credential. A
// connector sets the fields it has.
type State struct {
	Phase Phase
	// Session is the generation this state belongs to.
	Session uint64
	// Failure is why, when Failed.
	Failure Failure
	// Subject, Email and Name are the account's, display only; set when
	// Connected.
	Subject string
	Email   string
	Name    string
	// UserCode is the code a device sign-in asks the user to enter on the
	// provider's page; set while SigningIn, once the provider issued it.
	UserCode string
	// Registered is a saved client id.
	Registered bool
	// RevocationUnconfirmed is a revocation the provider did not confirm, of a
	// sign-out or of a sign-in that could not be saved. Cleared by the next
	// Connect.
	RevocationUnconfirmed bool
}

// The refusals of Connect and Disconnect.
var (
	ErrSigningIn  = errors.New("connectors: a sign-in is under way")
	ErrConnected  = errors.New("connectors: already connected")
	ErrNotDeleted = errors.New("connectors: a key the keyring would not delete is still there")
	// ErrNoClient is a build that names no OAuth client to sign in under.
	ErrNoClient = errors.New("connectors: no OAuth client is configured")
	// ErrRestoring is a change asked for before the launch read the keyring.
	ErrRestoring = errors.New("connectors: the keyring has not been read yet")
)

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

package errors

import (
	"maps"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Custom GraphQL errors, each carrying a stable `code` extension.
var (
	ErrValidationError    = NewError("KUBETAIL_VALIDATION_ERROR", "Validation error")
	ErrRecordNotFound     = NewError("KUBETAIL_RECORD_NOT_FOUND", "Record not found")
	ErrUnauthenticated    = NewError("KUBETAIL_UNAUTHENTICATED", "Authentication required")
	ErrForbidden          = NewError("KUBETAIL_FORBIDDEN", "Forbidden")
	ErrConflict           = NewError("KUBETAIL_CONFLICT", "Conflict")
	ErrWatchError         = NewError("KUBETAIL_WATCH_ERROR", "Watch error")
	ErrServiceUnavailable = NewError("KUBETAIL_SERVICE_UNAVAILABLE", "Service unavailable")
	ErrChatContextFull    = NewError("KUBETAIL_CHAT_CONTEXT_FULL", "Chat is longer than the model can read")
	ErrChatSandboxChanged = NewError("KUBETAIL_CHAT_SANDBOX_CHANGED", "Chat's sandbox switch changed")
	ErrChatNetworkChanged = NewError("KUBETAIL_CHAT_NETWORK_CHANGED", "Chat's network switch changed")
	ErrMemoryNameTaken    = NewError("KUBETAIL_MEMORY_NAME_TAKEN", "Memory name taken")
	ErrMemoryFull         = NewError("KUBETAIL_MEMORY_FULL", "Not enough room for this memory")
	ErrMemorySecret       = NewError("KUBETAIL_MEMORY_SECRET", "Memory holds a credential")
	// ErrSavedUnread is a write that committed whose answer could not be read
	// after: the change took, so the client treats it as taken.
	ErrSavedUnread = NewError("KUBETAIL_SAVED_UNREAD", "Saved, but not read back")
	// ErrProviderKeyNotSaved is a key the keyring refused; nothing changed.
	ErrProviderKeyNotSaved = NewError("KUBETAIL_PROVIDER_KEY_NOT_SAVED", "The keyring refused the key")
	// ErrProviderKeyNotDeleted is a saved key the keyring would not delete. It
	// is out of use, and returns after a restart.
	ErrProviderKeyNotDeleted = NewError("KUBETAIL_PROVIDER_KEY_NOT_DELETED", "The keyring would not delete the key")
	ErrInternalServerError   = NewError("INTERNAL_SERVER_ERROR", "Internal server error")
)

// Clone is what a resolver returns; never one of the values above. gqlgen stamps
// Path and Locations onto the error it is handed, so a shared value would report the
// first request's field ever after — and two requests failing at once would write to
// it concurrently.
func Clone(e *gqlerror.Error) *gqlerror.Error {
	c := *e
	c.Extensions = maps.Clone(e.Extensions)
	return &c
}

// NewError builds a gqlerror carrying a `code` extension.
func NewError(code string, message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message: message,
		Extensions: map[string]any{
			"code": code,
		},
	}
}

// NewValidationError is a validation error naming the rule that failed.
func NewValidationError(rule string, message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message: message,
		Extensions: map[string]any{
			"code": ErrValidationError.Extensions["code"],
			"rule": rule,
		},
	}
}

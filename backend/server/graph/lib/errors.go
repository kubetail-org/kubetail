// Copyright 2024 Andres Morey
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

package lib

import "github.com/vektah/gqlparser/v2/gqlerror"

// custom errors
var (
	ErrValidationError     = NewError("KUBETAIL_VALIDATION_ERROR", "Validation error")
	ErrRecordNotFound      = NewError("KUBETAIL_RECORD_NOT_FOUND", "Record not found")
	ErrInternalServerError = NewError("INTERNAL_SERVER_ERROR", "Internal server error")
)

// Initialize custom GraphQL errors
func NewError(code string, message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message: message,
		Extensions: map[string]interface{}{
			"code": code,
		},
	}
}

// New validation error
func NewValidationError(rule string, message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message: message,
		Extensions: map[string]interface{}{
			"code": ErrValidationError.Extensions["code"],
			"rule": rule,
		},
	}
}

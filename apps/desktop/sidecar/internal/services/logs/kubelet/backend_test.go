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

package kubelet

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A read's failure is fatal for the user's credentials or a claim revoked, denied
// for a 403, ended for any other status under 500 but a 408 or a 429, and
// transient for anything else; a clean end is none.
func TestFailureKind(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]Failure{
		nil:                                FailNone,
		ErrRevoked:                         FailFatal,
		fmt.Errorf("read: %w", ErrRevoked): FailFatal,
		&StatusError{Code: http.StatusUnauthorized}:        FailFatal,
		&StatusError{Code: http.StatusForbidden}:           FailDenied,
		&StatusError{Code: http.StatusBadRequest}:          FailEnded,
		&StatusError{Code: http.StatusNotFound}:            FailEnded,
		&StatusError{Code: http.StatusRequestTimeout}:      FailTransient,
		&StatusError{Code: http.StatusTooManyRequests}:     FailTransient,
		&StatusError{Code: http.StatusInternalServerError}: FailTransient,
		errors.New("connection reset"):                     FailTransient,
		ErrRetired:                                         FailTransient,
	} {
		assert.Equal(t, want, FailureKind(err), "%v", err)
	}
}

// A fatal failure fails the page or the follow with the cluster's words for a
// 401, and as a revoked claim otherwise.
func TestAFatalFailureSaysWhy(t *testing.T) {
	t.Parallel()
	assert.EqualError(t, FatalError(&StatusError{Code: http.StatusUnauthorized, Message: "Unauthorized"}), "the cluster refused to show this log: Unauthorized")
	assert.ErrorIs(t, FatalError(ErrRevoked), ErrRevoked)
}

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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A failed reading's code is the one its ReadError carries, wrapped or not, and
// unavailable for an error that carries none, invalid for a code the core lacks.
func TestAReadErrorCarriesItsCode(t *testing.T) {
	cause := errors.New("refused")
	err := fmt.Errorf("read: %w", &ReadError{Code: CodePermissionDenied, Err: cause})

	assert.Equal(t, CodePermissionDenied, codeOf(err))
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "read: monitor: reading permission_denied: refused", err.Error())
	assert.Equal(t, "monitor: reading disabled", (&ReadError{Code: CodeDisabled}).Error())
	assert.Equal(t, CodeUnavailable, codeOf(cause))
	assert.Equal(t, CodeUnavailable, codeOf(&ReadError{}))
	assert.Equal(t, CodeInvalid, codeOf(&ReadError{Code: "throttled"}))
}

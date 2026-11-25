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

package memory

import (
	"fmt"
	"regexp"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// The limits on a note's shape, checked on every write whoever writes. How many
// fit in a scope is ScopeBudget's to say.
const (
	NameMax = 48
	BodyMax = 500
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// FieldError is a note one of whose fields is out of shape. ErrBadInput wraps it.
type FieldError struct {
	Field string
}

func (e *FieldError) Error() string { return "memory: bad " + e.Field }

// checkNote is the one check of a note's shape: nil, ErrBadInput wrapping the
// *FieldError that names the field, or ErrSecret.
func checkNote(name, body string) error {
	if err := checkNoteName(name); err != nil {
		return err
	}
	switch {
	case body == "" || len(body) > BodyMax:
		return badField("body")
	case safe.HasSecret(body):
		return ErrSecret
	}
	return nil
}

// checkNoteName is the check of a note's name alone, for a forget, which carries
// no body. A registered key can fit the pattern, and the name reaches every later
// chat as the body does, so it is checked for one too.
func checkNoteName(name string) error {
	switch {
	case len(name) > NameMax || !nameRE.MatchString(name):
		return badField("name")
	case safe.HasSecret(name):
		return ErrSecret
	}
	return nil
}

func badField(field string) error {
	return fmt.Errorf("%w: %w", ErrBadInput, &FieldError{Field: field})
}

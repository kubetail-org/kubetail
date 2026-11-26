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

package appdb

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrBadUUID is what ValidateUUID wraps every refusal in.
var ErrBadUUID = errors.New("appdb: not a canonical uuid")

// NewID mints a row id: a canonical lowercase UUIDv7. Ids minted by one process
// increase in the order minted, since the library serializes its clock and counts
// within a millisecond. The only failure is the entropy source, which nothing
// above can handle, so it panics.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil { // coverage:ignore only the OS entropy source failing reaches it
		panic(fmt.Sprintf("appdb: mint id: %v", err))
	}
	return id.String()
}

// ValidateUUID accepts a canonical lowercase hyphenated UUID of version 4 or 7 with the
// RFC variant: the shape of a row id and of a request key a client mints. It says
// nothing about who minted it. uuid.Parse is laxer (braces, urn:uuid:, uppercase,
// bare hex), so the round trip through String pins the spelling; the nil UUID is
// version 0 and fails the version check.
func ValidateUUID(s string) error {
	id, err := uuid.Parse(s)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadUUID, err)
	}
	if id.String() != s {
		return fmt.Errorf("%w: not lowercase hyphenated", ErrBadUUID)
	}
	if v := id.Version(); v != 4 && v != 7 {
		return fmt.Errorf("%w: version %d", ErrBadUUID, v)
	}
	if id.Variant() != uuid.RFC4122 {
		return fmt.Errorf("%w: variant %v", ErrBadUUID, id.Variant())
	}
	return nil
}

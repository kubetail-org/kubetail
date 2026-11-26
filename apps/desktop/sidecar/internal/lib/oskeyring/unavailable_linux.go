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

package oskeyring

import (
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

const dbusError = "org.freedesktop.DBus.Error."

// unavailable wraps ErrUnavailable around a D-Bus reply saying the Secret
// Service is not on the bus. go-keyring calls with auto-start on, so a service
// not running yet answers ServiceUnknown or a Spawn error.
func unavailable(err error) error {
	switch name := replyName(err); {
	case name == dbusError+"ServiceUnknown",
		name == dbusError+"NameHasNoOwner",
		strings.HasPrefix(name, dbusError+"Spawn."):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

// replyName is the name of the D-Bus error reply err wraps, empty with none.
// godbus puts the value on a failed call and builds the pointer in NewError.
func replyName(err error) string {
	var value dbus.Error
	var ptr *dbus.Error
	switch {
	case errors.As(err, &value):
		return value.Name
	case errors.As(err, &ptr):
		return ptr.Name
	}
	return ""
}

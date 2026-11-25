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
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/go-keyring"
)

// A Secret Service that is not on the bus is ErrUnavailable, as godbus's value
// and as its pointer; another D-Bus error and a plain one are not.
func TestUnavailable(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner",
		"org.freedesktop.DBus.Error.Spawn.ExecFailed",
	} {
		for _, err := range []error{dbus.Error{Name: name}, dbus.NewError(name, nil)} {
			got := unavailable(err)
			assert.ErrorIs(t, got, ErrUnavailable, "%#v", err)
			assert.ErrorContains(t, got, name, "the cause is kept")
		}
	}
	for _, err := range []error{
		dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"},
		dbus.NewError("org.freedesktop.Secret.Error.IsLocked", nil),
		errors.New("plain"),
	} {
		assert.Equal(t, err, unavailable(err), "%#v", err)
	}
	assert.NoError(t, unavailable(nil))
}

// Every call of the OS keyring answers ErrUnavailable for a missing Secret
// Service. Serial: go-keyring's mock is process state.
func TestTheOSKeyringIsUnavailableWithoutTheService(t *testing.T) {
	keyring.MockInitWithError(dbus.NewError("org.freedesktop.DBus.Error.ServiceUnknown", nil))
	t.Cleanup(keyring.MockInit)
	k := osKeyring{service: "Kubetail-test"}

	_, err := k.Get("a")
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.ErrorIs(t, k.Set("a", "v"), ErrUnavailable)
	assert.ErrorIs(t, k.Delete("a"), ErrUnavailable)
}

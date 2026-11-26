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

package oauthconn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
)

// openStore is a loaded store over db, closed when the test ends.
func openStore(t *testing.T, db *appdb.DB) *registrationStore {
	t.Helper()
	store, err := openRegistration(db, providerID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.close()) })
	require.NoError(t, store.load())
	return store
}

// No row is an installation that never signed in.
func TestARegistrationWithNoRowIsEmpty(t *testing.T) {
	store := openStore(t, appdbtest.Open(t))

	assert.Equal(t, registration{}, store.get())
}

// What save writes, the next open reads: the row is the provider's own, its
// empty values NULL and the host id in provider_data.
func TestARegistrationRoundTrips(t *testing.T) {
	db := appdbtest.Open(t)
	store := openStore(t, db)
	want := registration{Data: hostData("urn:uuid:x"), ClientID: "c", Subject: "s", Email: "e", Name: "n", SessionID: "s1"}

	require.NoError(t, store.save(want))

	assert.Equal(t, want, openStore(t, db).get())
	var data string
	require.NoError(t, db.Read.QueryRowContext(t.Context(),
		`SELECT provider_data FROM oauth_connections WHERE provider = 'acme'`).Scan(&data))
	assert.JSONEq(t, `{"hostID":"urn:uuid:x"}`, data)

	require.NoError(t, store.save(want.forgotten()))

	assert.Equal(t, registration{Data: hostData("urn:uuid:x")}, openStore(t, db).get())
	var signedOut bool
	require.NoError(t, db.Read.QueryRowContext(t.Context(), `SELECT client_id IS NULL AND subject IS NULL
		AND email IS NULL AND name IS NULL AND session_id IS NULL FROM oauth_connections`).Scan(&signedOut))
	assert.True(t, signedOut, "an empty value is stored NULL")
}

// The row is one provider's: another provider's row is neither read nor written.
func TestARegistrationLeavesOtherProvidersAlone(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.ExecContext(t.Context(), `INSERT INTO oauth_connections
		(provider, client_id, session_id, created_at, updated_at) VALUES ('other', 'theirs', 'their-session', 0, 0)`)
	require.NoError(t, err)
	store := openStore(t, db)
	assert.Equal(t, registration{}, store.get())

	require.NoError(t, store.save(registration{Data: hostData("h"), ClientID: "c"}))

	var clientID, sessionID string
	require.NoError(t, db.Read.QueryRowContext(t.Context(),
		`SELECT client_id, session_id FROM oauth_connections WHERE provider = 'other'`).Scan(&clientID, &sessionID))
	assert.Equal(t, "theirs", clientID)
	assert.Equal(t, "their-session", sessionID)
}

// A save keeps the row's created_at.
func TestASaveKeepsWhenTheRowWasCreated(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.ExecContext(t.Context(),
		`INSERT INTO oauth_connections (provider, created_at, updated_at) VALUES ('acme', 7, 7)`)
	require.NoError(t, err)

	require.NoError(t, openStore(t, db).save(registration{Data: hostData("h")}))

	var created, updated int64
	require.NoError(t, db.Read.QueryRowContext(t.Context(),
		`SELECT created_at, updated_at FROM oauth_connections`).Scan(&created, &updated))
	assert.EqualValues(t, 7, created)
	assert.Greater(t, updated, int64(7))
}

// Every save reaches the writer, one equal to what memory holds included: a
// write that failed leaves memory as it was, and the retry must not be skipped.
func TestEverySaveReachesTheWriter(t *testing.T) {
	store := openStore(t, appdbtest.Open(t))
	var writes int
	write := store.write
	store.write = func(reg registration) error {
		writes++
		return write(reg)
	}

	require.NoError(t, store.save(registration{Data: hostData("h")}))
	require.NoError(t, store.save(registration{Data: hostData("h")}))

	assert.Equal(t, 2, writes)
}

// A failed write leaves memory as it was.
func TestAFailedSaveLeavesMemoryAlone(t *testing.T) {
	db := appdbtest.Open(t)
	store := openStore(t, db)
	require.NoError(t, store.save(registration{Data: hostData("h"), SessionID: "s1"}))
	require.NoError(t, store.close())

	assert.Error(t, store.save(registration{Data: hostData("h")}))
	assert.Equal(t, "s1", store.get().SessionID)
}

// A row whose provider_data is no JSON object is refused, and nothing of it is
// kept.
func TestAnUnreadableRegistrationIsRefused(t *testing.T) {
	db := appdbtest.Open(t)
	_, err := db.Write.ExecContext(t.Context(), `INSERT INTO oauth_connections
		(provider, client_id, provider_data, created_at, updated_at) VALUES ('acme', 'c', '{', 0, 0)`)
	require.NoError(t, err)
	store, err := openRegistration(db, providerID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.close()) })

	assert.Error(t, store.load())
	assert.Equal(t, registration{}, store.get())
}

// A read that fails is the load's error, and memory stays as it was.
func TestALoadThatCannotReadFails(t *testing.T) {
	store := openStore(t, appdbtest.Open(t))
	require.NoError(t, store.save(registration{Data: hostData("h")}))
	require.NoError(t, store.close())

	assert.Error(t, store.load())
	assert.Equal(t, registration{Data: hostData("h")}, store.get())
}

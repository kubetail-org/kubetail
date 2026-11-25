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

package kubeproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// kubectlTable is the Accept kubectl's get sends with no -o: two Table types,
// then plain JSON for a server that serves no Table.
const kubectlTable = "application/json;as=Table;v=v1;g=meta.k8s.io,application/json;as=Table;v=v1beta1;g=meta.k8s.io,application/json"

// A read prefers metadata alone when the first JSON type it accepts is a Table
// or PartialObjectMetadata, and it does not ask for the whole object.
func TestMetadataOnly(t *testing.T) {
	for _, c := range []struct {
		accept, query string
		want          bool
	}{
		{kubectlTable, "", true},
		{kubectlTable, "?includeObject=Metadata", true},
		{kubectlTable, "?includeObject=Object", false},
		{"application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io,application/json", "", true},
		{"application/json;as=PartialObjectMetadata;v=v1;g=meta.k8s.io", "", true},
		{"application/vnd.kubernetes.protobuf;as=Table;v=v1;g=meta.k8s.io,application/json", "", false},
		{"application/json,application/json;as=Table;v=v1;g=meta.k8s.io", "", false},
		{"application/json;as=Table;v=v1;g=example.com", "", false},
		{"application/json", "", false},
		{"", "", false},
	} {
		r := httptest.NewRequest("GET", "http://"+Host+"/api/v1/namespaces/web/secrets"+c.query, nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		assert.Equal(t, c.want, metadataOnly(r), "%q %q", c.accept, c.query)
	}
}

// allowSecrets is a rule allowing Secret reads in dev / web, as a chat answer
// writes one.
var allowSecrets = permissions.GrantRule(permissions.Action{Target: permissions.Kubernetes, AccessLevel: permissions.DataRead, Scope: "c1/web"})

// getSecret is a get of the Secret db in web, as kubectl get -o yaml sends it.
func (s *served) getSecret(t *testing.T) *http.Request {
	t.Helper()
	req := s.request(t, "GET", "/api/v1/namespaces/web/secrets/db", s.g.Token())
	req.Header.Set("Accept", "application/json")
	return req
}

// A grant with no one to ask, a background command's, reads Secret data
// redacted whatever its rules allow, and records nothing.
func TestASecretReadWithNoAskerIsRedacted(t *testing.T) {
	api := answering(t, secretJSON)
	for _, sess := range []session.Session{sessionIn(permissions.ModeAsk, allowSecrets), sessionIn(permissions.ModeAuto)} {
		s := serveGrant(t, NewGrant(api.upstream(), sess, "c1", "dev", nil, noAsker, 1000, 1000, 32))
		resp, body := s.do(t, s.getSecret(t))
		require.Equal(t, http.StatusOK, resp.StatusCode, body)
		assert.JSONEq(t, redactedSecretJSON, body)
	}
}

// A Secret read a rule allows answers the values, recorded allowed with the
// rule, the method and the path; a write on secrets under the same rule still
// answers redacted.
func TestAnAllowedSecretReadPassesUnredacted(t *testing.T) {
	api := answering(t, secretJSON)
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), sessionIn(permissions.ModeAsk, allowSecrets), asker)

	resp, body := s.do(t, s.getSecret(t))

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, secretJSON, body)
	r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
	assert.Equal(t, permissions.Allowed, r.d)
	assert.Equal(t, "a rule allows it: Allow data reads in dev / web", r.why)
	assert.Equal(t, Write{Method: "GET", Path: "/api/v1/namespaces/web/secrets/db"}, r.w)
	assert.Equal(t, "Show Secret db in web on dev", r.r.Action.Summary)
	assert.Empty(t, asker.fakeAsker, "nobody was asked")

	done := s.sendAsync(t, s.writeRequest(t, "PATCH", "/api/v1/namespaces/web/secrets/db", "application/merge-patch+json", `{"metadata":{"labels":{"a":"b"}}}`))
	asker.next(t).answer <- once
	reply := testutil.Recv(t, done, "the write")
	require.NotNil(t, reply.resp)
	assert.JSONEq(t, redactedSecretJSON, reply.body)
}

// A Secret read the policy refuses answers its values redacted with a 200, and
// is recorded refused with the reason: under a Deny rule, and in a session
// that never asks.
func TestADeniedSecretReadIsRedactedAndTwoHundred(t *testing.T) {
	deny := rule(permissions.Deny, permissions.DataRead, "c1/**")
	silent := sessionIn(permissions.ModeAsk)
	silent.Kind = session.Console
	for _, c := range []struct {
		sess session.Session
		why  string
	}{
		{sessionIn(permissions.ModeAsk, deny), "a rule denies it: Deny data reads anywhere in dev"},
		{silent, "it is never allowed"},
	} {
		api := answering(t, secretJSON)
		asker := newRecordingAsker()
		s := serveIn(t, api.upstream(), c.sess, asker)

		resp, body := s.do(t, s.getSecret(t))

		require.Equal(t, http.StatusOK, resp.StatusCode, body)
		assert.JSONEq(t, redactedSecretJSON, body)
		r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
		assert.Equal(t, permissions.Denied, r.d)
		assert.Equal(t, c.why, r.why)
	}

	// A refusal the asker cannot record still answers redacted.
	s := serveIn(t, answering(t, secretJSON).upstream(), sessionIn(permissions.ModeAsk, deny), errAsker{errors.New("store refused")})
	resp, body := s.do(t, s.getSecret(t))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, redactedSecretJSON, body)
}

// Under Ask a Secret read reaches the cluster only once the user decides:
// approved, its values pass; denied, or an asker that fails, they pass
// redacted with a 200.
func TestAPromptedSecretReadWaitsForTheDecision(t *testing.T) {
	api := answering(t, secretJSON)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	for answer, want := range map[permissions.Answer]string{once: secretJSON, denied: redactedSecretJSON} {
		done := s.sendAsync(t, s.getSecret(t))
		a := asker.next(t)
		assert.Empty(t, api.requests(), "nothing is read while the user decides")
		assert.Equal(t, "Show Secret db in web on dev", a.r.Action.Summary)
		assert.Equal(t, Write{Method: "GET", Path: "/api/v1/namespaces/web/secrets/db"}, a.w)
		assert.True(t, a.r.Grantable)
		assert.Equal(t, "Allow data reads in dev / web", a.r.ChatRule)
		a.answer <- answer
		r := testutil.Recv(t, done, "the read")
		require.NotNil(t, r.resp)
		require.Equal(t, http.StatusOK, r.resp.StatusCode)
		assert.JSONEq(t, want, r.body)
		api = answering(t, secretJSON)
		s = serveAsking(t, api.upstream(), asker)
	}

	s = serveAsking(t, api.upstream(), errAsker{errors.New("store refused")})
	resp, body := s.do(t, s.getSecret(t))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, redactedSecretJSON, body)
}

// A Command answer lets the same read in the same command pass unasked; a
// read in another namespace still asks.
func TestACommandAnswerCoversTheReadsThatFollow(t *testing.T) {
	api := answering(t, secretJSON)
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), askSession, asker)

	done := s.sendAsync(t, s.getSecret(t))
	asker.next(t).answer <- permissions.Answer{Approved: true, Duration: permissions.DurationCommand}
	testutil.Recv(t, done, "the first read")

	resp, body := s.do(t, s.getSecret(t))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, secretJSON, body)
	r := testutil.Recv(t, (<-chan recorded)(asker.records), "the record")
	assert.Equal(t, "a rule allows it: Allow get of core secrets in dev / web for this command", r.why)

	done = s.sendAsync(t, s.request(t, "GET", "/api/v1/namespaces/api/secrets/db", s.g.Token()))
	asker.next(t).answer <- denied
	testutil.Recv(t, done, "the read in another namespace")
}

// A read across the cluster asks for the context: the rule an answer writes
// names no namespace.
func TestAClusterWideListAsksForTheContext(t *testing.T) {
	api := answering(t, `{"kind":"SecretList","apiVersion":"v1","items":[`+secretJSON+`]}`)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.request(t, "GET", "/api/v1/secrets", s.g.Token()))
	a := asker.next(t)
	assert.Equal(t, "Show Secret data on dev", a.r.Action.Summary)
	assert.Equal(t, "Allow data reads of core secrets anywhere in dev", a.r.ChatRule)
	assert.Equal(t, "c1/**", permissions.GrantRule(a.r.Action).Scope, "every namespace, as the list reaches")
	a.answer <- denied
	testutil.Recv(t, done, "the list")
}

// A list across every namespace reaches each one's Secrets, so a deny for any
// namespace keeps the whole list redacted, though the mode is auto.
func TestADenyForOneNamespaceRedactsAClusterWideList(t *testing.T) {
	api := answering(t, `{"kind":"SecretList","apiVersion":"v1","items":[`+secretJSON+`]}`)
	deny := allowSecrets
	deny.Effect = permissions.Deny
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto, deny), asker)

	req := s.request(t, "GET", "/api/v1/secrets", s.g.Token())
	req.Header.Set("Accept", "application/json")
	resp, body := s.do(t, req)

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, `{"kind":"SecretList","apiVersion":"v1","items":[`+redactedSecretJSON+`]}`, body)
	assert.Equal(t, permissions.Denied, testutil.Recv(t, (<-chan recorded)(asker.records), "the record").d)
}

// A watch and a table that asks for the whole object are reads of Secret
// data: each asks, and each passes unredacted once allowed.
func TestAWatchAndAWholeTableAreDataReads(t *testing.T) {
	watching, next, _ := secretWatch(t)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, watching.upstream(), asker)
	opened := make(chan *http.Response, 1)
	go func() {
		resp, err := s.client.Do(s.request(t, "GET", "/api/v1/namespaces/web/secrets?watch=true", s.g.Token()))
		if err == nil {
			opened <- resp
		}
	}()
	a := asker.next(t)
	assert.Equal(t, "Watch Secret data in web on dev", a.r.Action.Summary)
	a.answer <- once
	resp := testutil.Recv(t, opened, "the watch to open")
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	next <- event("ADDED", secretJSON)
	require.True(t, lines.Scan(), lines.Err())
	assert.JSONEq(t, event("ADDED", secretJSON), lines.Text())

	table := `{"kind":"Table","apiVersion":"meta.k8s.io/v1","rows":[{"cells":["db"],"object":` + secretJSON + `}]}`
	s = serveAsking(t, answering(t, table).upstream(), asker)
	req := s.request(t, "GET", "/api/v1/namespaces/web/secrets?includeObject=Object", s.g.Token())
	req.Header.Set("Accept", kubectlTable)
	done := s.sendAsync(t, req)
	asker.next(t).answer <- once
	r := testutil.Recv(t, done, "the table")
	require.NotNil(t, r.resp)
	assert.JSONEq(t, table, r.body)
}

// A read that prefers metadata alone asks no one and records nothing: it
// passes redacted, as it has no data to show.
func TestAMetadataOnlyReadDoesNotAsk(t *testing.T) {
	table := `{"kind":"Table","apiVersion":"meta.k8s.io/v1","rows":[{"cells":["db"],"object":{"kind":"PartialObjectMetadata","metadata":{"name":"db"}}}]}`
	api := answering(t, table)
	asker := newRecordingAsker()
	s := serveIn(t, api.upstream(), askSession, asker)
	for _, accept := range []string{kubectlTable, "application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io,application/json"} {
		req := s.request(t, "GET", "/api/v1/namespaces/web/secrets", s.g.Token())
		req.Header.Set("Accept", accept)
		resp, body := s.do(t, req)
		require.Equal(t, http.StatusOK, resp.StatusCode, body)
		assert.JSONEq(t, table, body)
	}
	assert.Empty(t, asker.records, "nothing is recorded")
	assert.Empty(t, asker.fakeAsker, "nobody is asked")

	for _, c := range []struct{ accept, query string }{{"application/json", ""}, {kubectlTable, "?includeObject=Object"}} {
		req := s.request(t, "GET", "/api/v1/namespaces/web/secrets"+c.query, s.g.Token())
		req.Header.Set("Accept", c.accept)
		done := s.sendAsync(t, req)
		asker.next(t).answer <- denied
		testutil.Recv(t, done, "the read")
	}
}

// A Secret read waits for the write lock as a write does: behind a write the
// user is deciding, one more past the queue's bound answering 429; and an
// allowed watch lets the lock go before it streams.
func TestASecretReadWaitsItsTurnBehindAWrite(t *testing.T) {
	watching, next, _ := secretWatch(t)
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/secrets") {
			_, _ = w.Write([]byte(secretJSON))
		}
	})
	asker := make(fakeAsker, 1)
	s, waiting := serveCountingWaiters(t, api.upstream(), asker)

	write := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/a", "", ""))
	held := asker.next(t)
	read := s.sendAsync(t, s.getSecret(t))
	require.Eventually(t, func() bool { return waiting.Load() == 1 }, testutil.Timeout, time.Millisecond, "the read to queue")
	testutil.NoRecv(t, (<-chan asked)(asker), quietWindow, "a read asked while a write waits")
	held.answer <- once
	testutil.Recv(t, write, "the write")
	asker.next(t).answer <- once
	r := testutil.Recv(t, read, "the read")
	require.NotNil(t, r.resp)
	assert.JSONEq(t, secretJSON, r.body)

	s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/b", "", ""))
	asker.next(t)
	for range maxQueuedWrites {
		s.sendAsync(t, s.getSecret(t))
	}
	require.Eventually(t, func() bool { return waiting.Load() == maxQueuedWrites }, testutil.Timeout, time.Millisecond, "the queue to fill")
	resp, body := s.do(t, s.getSecret(t))
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "kubetail: too many requests are waiting on the user. Send one at a time.", statusOf(t, body).Message)

	s = serveIn(t, watching.upstream(), sessionIn(permissions.ModeAsk, allowSecrets), asker)
	resp, err := s.client.Do(s.request(t, "GET", "/api/v1/namespaces/web/secrets?watch=true", s.g.Token()))
	require.NoError(t, err)
	defer resp.Body.Close()
	done := s.sendAsync(t, s.writeRequest(t, "DELETE", "/api/v1/namespaces/web/pods/c", "", ""))
	asker.next(t).answer <- denied
	testutil.Recv(t, done, "a write sent while the watch streams")
	lines := bufio.NewScanner(resp.Body)
	next <- event("ADDED", secretJSON)
	require.True(t, lines.Scan(), lines.Err())
	assert.JSONEq(t, event("ADDED", secretJSON), lines.Text())
}

// approvingAsker approves whatever it is asked, failing the test: a session
// that must never ask would show Secret data if it did.
type approvingAsker struct{ t *testing.T }

func (a approvingAsker) Ask(context.Context, Request) (permissions.Answer, error) {
	a.t.Error("a session that never asks asked")
	return once, nil
}

func (approvingAsker) Record(context.Context, Request, permissions.Decision, string) error {
	return nil
}

// Without a grant every read of Secret data answers it redacted: under
// ReadOnly and Ask in a session that never asks, and under every mode in a
// session that never reads Secret data, whatever its rules allow. Auto
// without NoSecretData allows the read, as the note's table has it.
func TestSecretDataIsRedactedWithoutTheGrant(t *testing.T) {
	release := releaseValue(t, `{"name":"web","config":{"password":"hunter2"}}`, true)
	releaseJSON := `{"kind":"Secret","metadata":{"name":"r"},"data":{"release":` + marshaled(release) + `},"type":"helm.sh/release.v1"}`
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("watch") == "true":
			_, _ = w.Write([]byte(event("ADDED", secretJSON) + "\n"))
		case strings.HasSuffix(r.URL.Path, "/secrets/r"):
			_, _ = w.Write([]byte(releaseJSON))
		case r.URL.Query().Get("includeObject") == "Object":
			_, _ = w.Write([]byte(`{"kind":"Table","apiVersion":"meta.k8s.io/v1","rows":[{"cells":["db"],"object":` + secretJSON + `}]}`))
		case strings.HasSuffix(r.URL.Path, "/secrets"):
			_, _ = w.Write([]byte(`{"kind":"SecretList","apiVersion":"v1","items":[` + secretJSON + `]}`))
		default:
			_, _ = w.Write([]byte(secretJSON))
		}
	})
	var sessions []session.Session
	for _, mode := range []permissions.Mode{permissions.ModeReadOnly, permissions.ModeAsk} {
		sess := sessionIn(mode)
		sess.Kind = session.Console
		sessions = append(sessions, sess)
	}
	everything := rule(permissions.Allow, permissions.DataRead, "c1/**")
	for _, mode := range permissions.Modes {
		sess := sessionIn(mode, allowSecrets, everything)
		sess.Kind = session.Monitor
		sessions = append(sessions, sess)
	}
	for i, sess := range sessions {
		s := serveIn(t, api.upstream(), sess, approvingAsker{t})
		for _, target := range []string{
			"/api/v1/namespaces/web/secrets/db",
			"/api/v1/namespaces/web/secrets",
			"/api/v1/namespaces/web/secrets?includeObject=Object",
			"/api/v1/namespaces/web/secrets?watch=true",
		} {
			resp, body := s.send(t, "GET", target, s.g.Token())
			require.Equal(t, http.StatusOK, resp.StatusCode, body)
			assert.NotContains(t, body, "aHVudGVyMg==", "session %d, %s", i, target)
			assert.Contains(t, body, "W3JlZGFjdGVkXQ==", "session %d, %s", i, target)
		}
		resp, body := s.send(t, "GET", "/api/v1/namespaces/web/secrets/r", s.g.Token())
		require.Equal(t, http.StatusOK, resp.StatusCode, body)
		var secret struct {
			Data map[string]string `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &secret))
		assert.Equal(t, map[string]any{"password": "[redacted]"}, helmReleaseOf(t, secret.Data["release"]).Config, "session %d", i)
	}

	s := serveIn(t, api.upstream(), sessionIn(permissions.ModeAuto), approvingAsker{t})
	_, body := s.do(t, s.getSecret(t))
	assert.JSONEq(t, secretJSON, body, "auto mode shows Secret data unasked")
}

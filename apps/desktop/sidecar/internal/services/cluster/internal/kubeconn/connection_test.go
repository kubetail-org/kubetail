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

package kubeconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// The trap: a hardcoded scheme turns a plain-HTTP port-forward into https:// and every request
// fails at the handshake. The scheme follows the credentials the config actually carries.
func TestNewConnectionResolvesTheBaseURLWithoutAssumingTLS(t *testing.T) {
	t.Parallel()

	plain, err := NewConnection(&rest.Config{Host: "localhost:8080"})
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", plain.BaseURL.String())

	secured, err := NewConnection(&rest.Config{
		Host:            "example.com",
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", secured.BaseURL.String())
}

// The tuning is this package's, and the caller's config is not ours to edit — a caller that
// reads its own config back must find what it wrote.
func TestNewConnectionStampsItsTuningOnACopy(t *testing.T) {
	t.Parallel()

	caller := &rest.Config{Host: "https://one.example"}

	conn, err := NewConnection(caller)
	require.NoError(t, err)

	assert.Equal(t, defaultQPS, conn.Config.QPS)
	assert.Equal(t, version.UserAgent(), conn.Config.UserAgent)
	assert.Zero(t, caller.QPS, "the caller's config is untouched")
	assert.Empty(t, caller.UserAgent)
}

// A host that will not parse fails the build, where a nil connection handed back as usable
// would panic at the first request instead.
func TestNewConnectionRefusesAHostItCannotParse(t *testing.T) {
	t.Parallel()

	_, err := NewConnection(&rest.Config{Host: "://nonsense"})

	assert.Error(t, err)
}

// Retiring is what tells every holder that what it derived over this connection no longer
// holds. Idempotent because publish and Release can both reach one on the way out.
func TestRetireClosesDoneOnce(t *testing.T) {
	t.Parallel()

	conn, err := NewConnection(&rest.Config{Host: "https://one.example"})
	require.NoError(t, err)

	conn.Retire()
	conn.Retire()

	<-conn.Done()
}

// A connection nobody built has a nil channel, which blocks forever — reading as never retired,
// where a closed one would tell every holder its connection had gone.
func TestDoneOnAConnectionNobodyBuiltNeverFires(t *testing.T) {
	t.Parallel()

	var c Connection

	select {
	case <-c.Done():
		t.Fatal("an unbuilt connection reported itself retired")
	default:
	}
}

func TestGetJSONDecodesTheAnswer(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveAPI(t))

	var body struct {
		Versions []string `json:"versions"`
	}
	require.NoError(t, conn.getJSON(t.Context(), "/api", &body))

	assert.Equal(t, []string{"v1"}, body.Versions)
}

// The status code is the whole evidence a raw endpoint leaves, so it travels as data — a
// classifier reading it back out of a formatted string is one that breaks on a reworded message.
func TestGetJSONReportsTheStatusCode(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	})))

	err := conn.getJSON(t.Context(), "/api", &struct{}{})

	var he *httpErr
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusForbidden, he.code)
	assert.Contains(t, err.Error(), "/api")
}

// A 200 that will not decode is a proxy or captive portal answering for the API server, which
// is a different fault from either a transport failure or a status code.
func TestGetJSONReportsABodyThatWillNotDecode(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>sign in to continue</html>"))
	})))

	err := conn.getJSON(t.Context(), "/api", &struct{}{})

	assert.ErrorIs(t, err, errMalformed)
}

// Nothing listening is the transport failing, and it must not read as a status.
func TestGetJSONReportsATransportFailure(t *testing.T) {
	t.Parallel()

	conn, err := NewConnection(restConfigForHost(deadHost))
	require.NoError(t, err)

	err = conn.getJSON(t.Context(), "/api", &struct{}{})

	require.Error(t, err)
	var he *httpErr
	assert.False(t, errors.As(err, &he), "nothing answered, so there is no status")
}

// wrapped is how every one of these actually arrives: the http client returns a *url.Error
// around whatever the transport hit, so classifying the bare error would classify nothing.
func wrapped(err error) error {
	return &url.Error{Op: "Get", URL: "https://one.example/api", Err: err}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want Reason
	}{
		{"connection refused", wrapped(&net.OpError{Err: syscall.ECONNREFUSED}), ReasonUnreachable},
		{"no such host", wrapped(&net.DNSError{IsNotFound: true}), ReasonUnreachable},
		{"untrusted CA", wrapped(x509.UnknownAuthorityError{}), ReasonTLSInvalid},
		{"wrong hostname", wrapped(x509.HostnameError{}), ReasonTLSInvalid},
		{"expired certificate", wrapped(&tls.CertificateVerificationError{}), ReasonTLSInvalid},
		{"our deadline", wrapped(context.DeadlineExceeded), ReasonTimeout},
		{"unauthorized", &httpErr{code: http.StatusUnauthorized}, ReasonUnauthorized},
		{"forbidden", &httpErr{code: http.StatusForbidden}, ReasonForbidden},
		{"throttled", &httpErr{code: http.StatusTooManyRequests}, ReasonThrottled},
		{"server fault", &httpErr{code: http.StatusInternalServerError}, ReasonInternalError},
		{"restarting", &httpErr{code: http.StatusServiceUnavailable}, ReasonServiceUnavailable},
		{"not a kube server", &httpErr{code: http.StatusNotFound}, ReasonMalformed},
		{"a proxy answered", &httpErr{code: http.StatusBadGateway}, ReasonMalformed},
		{"undecodable body", fmt.Errorf("%w: %w", errMalformed, errors.New("invalid character '<'")), ReasonMalformed},
		{"a bug here", fmt.Errorf("%w: %w", errBadRequest, errors.New("bad method")), ReasonInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classify(tt.err))
		})
	}
}

func TestClassifyRequest(t *testing.T) {
	t.Parallel()

	pods := schema.GroupResource{Resource: "pods"}
	tests := []struct {
		name string
		err  error
		want Reason
	}{
		{"forbidden", apierrors.NewForbidden(pods, "", errors.New("cannot list")), ReasonForbidden},
		{"unauthorized", apierrors.NewUnauthorized("expired"), ReasonUnauthorized},
		{"gone from the server", apierrors.NewNotFound(pods, ""), ReasonNotFound},
		{"throttled", apierrors.NewTooManyRequests("slow down", 1), ReasonThrottled},
		{"server fault", apierrors.NewInternalError(errors.New("boom")), ReasonInternalError},
		{"restarting", apierrors.NewServiceUnavailable("upgrading"), ReasonServiceUnavailable},
		{"wrapped", fmt.Errorf("list pods: %w", apierrors.NewForbidden(pods, "", errors.New("no"))), ReasonForbidden},
		{"a status with no code of note", apierrors.FromObject(&metav1.Status{Message: "too old resource version"}), ReasonUnknown},
		{"connection refused", wrapped(&net.OpError{Err: syscall.ECONNREFUSED}), ReasonUnreachable},
		{"untrusted CA", wrapped(x509.UnknownAuthorityError{}), ReasonTLSInvalid},
		{"our deadline", fmt.Errorf("list: %w", context.DeadlineExceeded), ReasonTimeout},
		{"not a request's failure", errors.New("watch closed having proved nothing"), ReasonUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyRequest(tt.err))
		})
	}
}

// The forms above are hand-built, so one real rejection is what pins them to what the transport
// actually emits — including that crypto/tls wraps the x509 error in its own.
func TestClassifyReadsARealHostnameMismatchAsTLSInvalid(t *testing.T) {
	t.Parallel()

	srv := serveAPI(t)
	// httptest's certificate covers 127.0.0.1 and example.com, so "localhost" reaches the same
	// server with a name the certificate does not carry.
	conn, err := NewConnection(&rest.Config{
		Host:            strings.Replace(srv.URL, "127.0.0.1", "localhost", 1),
		TLSClientConfig: rest.TLSClientConfig{CAData: caPEM(srv)},
	})
	require.NoError(t, err)

	err = conn.getJSON(t.Context(), "/api", &struct{}{})

	require.Error(t, err)
	assert.Equal(t, ReasonTLSInvalid, classify(err))
}

// A TLS failure arrives wrapped in the same *url.Error a refused connection does, so an
// unreachable-first classifier would report every rejected certificate as a server that never
// answered — the opposite of the truth, and a different fix for the user.
func TestClassifyTellsARejectedCertificateFromAServerThatNeverAnswered(t *testing.T) {
	t.Parallel()

	refused := wrapped(&net.OpError{Err: syscall.ECONNREFUSED})
	rejected := wrapped(&net.OpError{Op: "remote error", Err: x509.UnknownAuthorityError{}})

	assert.Equal(t, ReasonUnreachable, classify(refused))
	assert.Equal(t, ReasonTLSInvalid, classify(rejected))
}

// fakeEnv is an environment of the test's own, so a keepalive test leaves the process's
// alone.
type fakeEnv map[string]string

func (e fakeEnv) lookup(key string) (string, bool) {
	v, ok := e[key]
	return v, ok
}

func (e fakeEnv) set(key, val string) error {
	e[key] = val
	return nil
}

func TestKeepaliveTightensTheHealthCheck(t *testing.T) {
	t.Parallel()
	env := fakeEnv{}

	setKeepaliveDefaults(env.lookup, env.set)

	assert.Equal(t, fakeEnv{readIdleTimeoutEnv: "10", pingTimeoutEnv: "5"}, env)
}

// The values stay tunable: 0 disables the health check, which is how an operator turns
// it off.
func TestKeepalivePreservesAnOverride(t *testing.T) {
	t.Parallel()
	env := fakeEnv{readIdleTimeoutEnv: "0", pingTimeoutEnv: "42"}

	setKeepaliveDefaults(env.lookup, env.set)

	assert.Equal(t, fakeEnv{readIdleTimeoutEnv: "0", pingTimeoutEnv: "42"}, env)
}

// Building the pool is what applies it: the vars are read when a transport is built, and this
// package is what builds them — a call the composition root has to remember is one that goes
// missing. It reads the process's environment, so it stays serial.
func TestNewConfiguresTheKeepalive(t *testing.T) {
	t.Setenv(readIdleTimeoutEnv, "")
	os.Unsetenv(readIdleTimeoutEnv)

	svc := New(resolving("prod", "key-1"))
	defer func() { assert.NoError(t, svc.Close()) }()

	assert.Equal(t, "10", os.Getenv(readIdleTimeoutEnv))
}

// TLS material that will not load fails the build, where a connection handed back as usable
// would fail at the first handshake instead.
func TestNewConnectionRefusesCredentialsItCannotLoad(t *testing.T) {
	t.Parallel()

	_, err := NewConnection(&rest.Config{
		Host:            "https://one.example",
		TLSClientConfig: rest.TLSClientConfig{CAFile: "/nonexistent/ca.crt"},
	})

	assert.ErrorContains(t, err, "build http client")
}

func TestNewConnectionReportsAClientItCannotBuild(t *testing.T) {
	t.Parallel()

	failing := errors.New("no dynamic client")
	build := func(*rest.Config, *http.Client) (dynamic.Interface, error) { return nil, failing }

	_, err := newConnection(&rest.Config{Host: "https://one.example"}, options{}, build)

	assert.ErrorIs(t, err, failing)
}

// A request that cannot be built is a bug here, not news about the cluster — so it classifies as
// Internal rather than as a server that would not answer.
func TestRequestReportsOneItCannotBuild(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveAPI(t))
	var noCtx context.Context

	err := conn.getJSON(noCtx, "/api", &struct{}{})

	assert.ErrorIs(t, err, errBadRequest)
	assert.Equal(t, ReasonInternal, classify(err))
}

// A body that stops arriving mid-read is the same fault as one that will not parse: something
// answered, and what it said cannot be used.
func TestRequestReportsABodyThatStopsMidRead(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = io.WriteString(w, "{")
	})))

	_, err := conn.getText(t.Context(), "/api")

	assert.ErrorIs(t, err, errMalformed)
	assert.Equal(t, ReasonMalformed, classify(err))
}

// The status line already arrived, and it is the stronger evidence: a 403 whose error page stops
// mid-stream is still a grant to fix, not the proxy a malformed answer would point at.
func TestRequestKeepsTheStatusWhenTheBodyStopsMidRead(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "forbidden: ")
	})))

	_, err := conn.getText(t.Context(), "/api")

	var status *httpErr
	require.ErrorAs(t, err, &status)
	assert.Equal(t, http.StatusForbidden, status.code)
	assert.Equal(t, ReasonForbidden, classify(err))
	assert.Equal(t, "forbidden: ", status.body, "whatever arrived before it stopped")
}

// --- identity ---

// A connection nobody has identified must never read as a match: the caller scoped to one
// cluster has to refuse it, and ("", false) is the only answer that lets it.
func TestServerUIDIsUnsetUntilAProbeReadsOne(t *testing.T) {
	t.Parallel()

	conn := &Connection{}

	uid, ok := conn.ServerUID()

	assert.False(t, ok)
	assert.Empty(t, uid)
}

// Set once, and re-confirming the same server changes nothing.
func TestSetServerUIDIsSetOnce(t *testing.T) {
	t.Parallel()

	conn := &Connection{}

	conn.setServerUID("uid-1")
	conn.setServerUID("uid-1")

	uid, ok := conn.ServerUID()
	require.True(t, ok)
	assert.Equal(t, "uid-1", uid)
}

// A second, different uid is the server replaced behind credentials that never moved. Keeping
// the old stamp would go on vouching for the old cluster while the new one answers — so the
// connection stops vouching for either, and every caller scoped to an identity is refused.
func TestSetServerUIDStopsVouchingWhenTheServerChanges(t *testing.T) {
	t.Parallel()

	conn := &Connection{}
	conn.setServerUID("uid-1")

	conn.setServerUID("uid-2")

	uid, ok := conn.ServerUID()
	assert.False(t, ok, "not the old cluster, and not the new one either")
	assert.Empty(t, uid)
}

// The conflict stands: a later read agreeing with the original stamp does not clear it, since
// what answers over this connection has already been seen to change.
func TestSetServerUIDKeepsTheConflict(t *testing.T) {
	t.Parallel()

	conn := &Connection{}
	conn.setServerUID("uid-1")
	conn.setServerUID("uid-2")

	conn.setServerUID("uid-1")

	_, ok := conn.ServerUID()
	assert.False(t, ok)
}

// The three refusals are told apart in one place, so a caller cannot re-derive them from the
// parts and drift — and so the message names what actually moved.
func TestIdentityForTellsTheThreeRefusalsApart(t *testing.T) {
	t.Parallel()

	unidentified := &Connection{}

	identified := &Connection{}
	identified.setServerUID("uid-1")

	conflicted := &Connection{}
	conflicted.setServerUID("uid-1")
	conflicted.setServerUID("uid-2")

	assert.NoError(t, identified.IdentityFor("uid-1"))
	assert.ErrorContains(t, identified.IdentityFor("uid-2"), "reached uid-1, not uid-2")
	assert.ErrorContains(t, unidentified.IdentityFor("uid-1"), "has not said which server")
	assert.ErrorContains(t, conflicted.IdentityFor("uid-1"), "was replaced")

	for _, err := range []error{
		identified.IdentityFor("uid-2"), unidentified.IdentityFor("uid-1"), conflicted.IdentityFor("uid-1"),
	} {
		assert.ErrorIs(t, err, ErrIdentityMismatch)
	}
}

// conflicted is what the rebuild reads, and it has to tell the two ways a connection vouches for
// nobody apart: one nothing has identified yet is on its way to being usable, while one whose
// server changed underneath it is finished. ServerUID answers ("", false) for both.
func TestConflictedIsOnlyTheServerChangingUnderneath(t *testing.T) {
	t.Parallel()

	unidentified := &Connection{}

	identified := &Connection{}
	identified.setServerUID("uid-1")

	replaced := &Connection{}
	replaced.setServerUID("uid-1")
	replaced.setServerUID("uid-2")

	assert.False(t, unidentified.conflicted(), "nothing has read a uid over it yet")
	assert.False(t, identified.conflicted())
	assert.True(t, replaced.conflicted())
}

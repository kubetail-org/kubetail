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
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// connect runs the connection probe's body once, on the test's goroutine, and applies what it
// recorded the way the supervisor would.
func connect(t *testing.T, cfg *fakeKubeconfig, v connInfo) (supervisor.Result, connInfo) {
	t.Helper()
	pass := supervisor.NewJobPass("prod", &v, supervisor.Snapshot{})
	res := (&connectionProbe{kubecfgSvc: cfg}).Run(t.Context(), pass)
	if next, ok := pass.Updated(); ok {
		v = next
	}
	return res, v
}

// The supervisor reads a committed value as one that moved and wakes whoever reads it, so a probe
// that found nothing new must hand back nothing — or the four behind it re-run every cycle and
// their intervals stop meaning anything.
func TestTheConnectionProbeCommitsOnlyOnAChange(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	_, first := connect(t, cfg, connInfo{departed: true})
	require.False(t, first.departed, "the context resolves, so it is back")

	pass := supervisor.NewJobPass("prod", &first, supervisor.Snapshot{})
	res := (&connectionProbe{kubecfgSvc: cfg}).Run(t.Context(), pass)

	assert.Equal(t, ReasonUnreachable, res.Reason())
	_, recorded := pass.Updated()
	assert.False(t, recorded, "nothing moved, so nothing is committed")
}

// Each of the four reads the connection's value to reach the server, so a connection that moves
// re-runs them — even parked, as they are here: nothing answers, so they are suspended behind a
// connection that never succeeds, and only the data edge can reach them.
func TestAMovedConnectionReRunsTheProbesBehindIt(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	before := awaitState(t, watched, func(st State) bool {
		return st.ServerUID.LastAttempt.Done()
	}).ServerUID.LastAttempt

	// A departure moves the connection's value, where a re-read that found the same thing
	// would not.
	cfg.rotate("prod", "")
	cfg.changed()

	after := awaitState(t, watched, func(st State) bool {
		return st.ServerUID.LastAttempt.FinishedAt.After(before.FinishedAt)
	}).ServerUID
	assert.Equal(t, ReasonDependencyFailed, after.LastAttempt.Reason)
	assert.True(t, after.LastAttempt.StartedAt.IsZero(), "re-run, but still never dialed")
}

// --- the connection probe's classifications ---

// A context that left the file has nothing to reach, so its probe suspends — the file is the
// whole truth about presence, and the watch reports it moving, so polling asks nothing new.
func TestConnectionSuspendsADepartedContext(t *testing.T) {
	t.Parallel()

	cfg := resolving("staging", "key-1") // "prod" is not named

	res, v := connect(t, cfg, connInfo{})

	assert.Equal(t, supervisor.VerdictSuspended, res.Verdict())
	assert.Equal(t, ReasonContextNotFound, res.Reason())
	assert.True(t, v.departed)
}

// The file still names it, so the remedy is to fix the file — a failure on the retry ladder,
// since nothing here can tell when the user has.
func TestConnectionFailsWhenTheFileWillNotResolve(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	cfg.err = errors.New("open ca.crt: no such file")

	res, v := connect(t, cfg, connInfo{departed: true})

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonResolveFailed, res.Reason())
	assert.ErrorIs(t, res.Err(), cfg.err)
	assert.False(t, v.departed, "the file names it, so it has not departed")
}

// An unread kubeconfig names nothing, and reporting that as anything would tell every holder
// its context is gone for as long as the first read takes.
func TestConnectionSkipsAnUnreadKubeconfig(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	cfg.err = kubeconfig.ErrNotRead

	res, v := connect(t, cfg, connInfo{})

	assert.True(t, res.IsSkip())
	assert.False(t, v.departed)
}

// The whole answer: reached, over TLS we trust, with credentials the server accepts.
func TestConnectionSucceedsWhenTheServerAnswers(t *testing.T) {
	t.Parallel()

	srv := serveAPI(t)
	cfg := serving(srv, "prod", "key-1")

	res, v := connect(t, cfg, connInfo{departed: true})

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.False(t, v.departed, "a context that resolves again is back")
	assert.NotNil(t, v.conn)
	assert.Equal(t, srv.URL, v.endpoint)
	assert.Equal(t, "key-1", v.fingerprint)
}

// A server that will not have these credentials is not an outage: the failure names what the
// user has to fix, and the four probes behind this one could not run either.
func TestConnectionFailsWithWhatTheServerSaid(t *testing.T) {
	t.Parallel()

	srv := serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))

	res, v := connect(t, serving(srv, "prod", "key-1"), connInfo{})

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonUnauthorized, res.Reason())
	assert.NotNil(t, v.conn, "the credentials resolved, so the connection stands")
}

func TestConnectionFailsWhenNothingAnswers(t *testing.T) {
	t.Parallel()

	res, _ := connect(t, resolving("prod", "key-1"), connInfo{})

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonUnreachable, res.Reason())
}

// A 200 from something that is not an API server is the case a status code cannot catch — a
// captive portal answers everything, so what proves the far end is a Kubernetes API server is
// its body.
func TestConnectionFailsWhenTheAnswerIsNotKubernetes(t *testing.T) {
	t.Parallel()

	srv := serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))

	res, _ := connect(t, serving(srv, "prod", "key-1"), connInfo{})

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonMalformed, res.Reason())
}

// Rebuilding a healthy connection would drop every socket under the holders using it, so the
// same credentials keep the same connection — and commit nothing, since the four probes behind
// it wake on a committed value.
func TestConnectionKeepsItsConnectionWhileTheCredentialsHold(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	pass := supervisor.NewJobPass("prod", &first, supervisor.Snapshot{})
	res := (&connectionProbe{kubecfgSvc: cfg}).Run(t.Context(), pass)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	_, recorded := pass.Updated()
	assert.False(t, recorded, "nothing moved, so nothing is committed")
}

// Credentials that moved are a different server as far as anything derived from them goes, so
// the connection is rebuilt — and the new pointer is what wakes the probes reading it.
func TestConnectionRebuildsWhenTheCredentialsMove(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	cfg.rotate("prod", "key-2")
	res, second := connect(t, cfg, first)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.NotSame(t, first.conn, second.conn)
	assert.Equal(t, "key-2", second.fingerprint)
}

// The server behind unchanged credentials was replaced, so nothing about the file moved and the
// connection is the only thing that knows. Without this arm the stall is permanent: a conflicted
// connection vouches for nobody, and every identity-scoped caller is refused for as long as it
// stands.
func TestConnectionRebuildsWhenItsServerWasReplaced(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	first.conn.setServerUID("uid-1")
	first.conn.setServerUID("uid-2")
	res, second := connect(t, cfg, first)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.NotSame(t, first.conn, second.conn, "the conflicted connection is replaced")
	assert.False(t, second.conn.conflicted(), "and the replacement vouches for nobody yet")
	assert.Equal(t, "key-1", second.fingerprint, "over credentials that never moved")
}

// A first identification is not a replaced server, so it rebuilds nothing — otherwise every
// connection would be rebuilt once, as soon as the probe behind it answered.
func TestConnectionKeepsItsConnectionThroughTheFirstStamp(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	first.conn.setServerUID("uid-1")
	_, second := connect(t, cfg, first)

	assert.Same(t, first.conn, second.conn)
}

// A file that will not resolve keeps its connection, where a departure drops one: the read
// failed, which says nothing about whether the credentials behind the connection still work, and
// an editor saving non-atomically is a read that fails for a moment.
func TestConnectionKeepsItsConnectionThroughAResolveFailure(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	cfg.setErr(errors.New("open ca.crt: no such file"))
	res, second := connect(t, cfg, first)

	require.Equal(t, ReasonResolveFailed, res.Reason())
	assert.Same(t, first.conn, second.conn)
	select {
	case <-second.conn.Done():
		t.Fatal("the connection was retired over a read that failed")
	default:
	}
}

// A context that left the file has no connection to hand out. Retiring the one it had is the
// pool's, not the probe's — this only stops naming it.
func TestConnectionDropsItsConnectionWhenTheContextDeparts(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	_, first := connect(t, cfg, connInfo{})
	require.NotNil(t, first.conn)

	cfg.rotate("prod", "")
	_, second := connect(t, cfg, first)

	assert.True(t, second.departed)
	assert.Nil(t, second.conn)
	assert.Empty(t, second.endpoint)
}

// The trap: a build that fails commits the new fingerprint, so a run keyed on the fingerprint
// alone would find it unchanged and never build again — the probe would climb its ladder
// retrying nothing.
func TestConnectionBuildsAgainAfterABuildThatFailed(t *testing.T) {
	t.Parallel()

	srv := serveAPI(t)
	cfg := serving(srv, "prod", "key-1")
	cfg.host = "://nonsense"

	res, failed := connect(t, cfg, connInfo{})
	require.Equal(t, ReasonResolveFailed, res.Reason())
	require.Nil(t, failed.conn)
	require.Equal(t, "key-1", failed.fingerprint, "the fingerprint it tried")

	cfg.host = srv.URL
	res, built := connect(t, cfg, failed)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.NotNil(t, built.conn, "the fingerprint did not move, but there was no connection")
}

// --- through the supervisor ---

// The four probes behind the connection are recorded rather than dialed while nothing has
// succeeded at reaching the server: one timeout per cycle, not one per probe.
func TestDependentsRecordDependencyFailedWhileTheServerIsUnreachable(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	// Not any recorded frame: the connection committing wakes the probes that read
	// it, so one can show a fresh run in flight over the last record. The settled
	// one is what the suspension is asserted on.
	st := awaitState(t, watched, func(st State) bool {
		return st.ServerUID.LastAttempt.Done() && !st.ServerUID.InFlight()
	})

	assert.Equal(t, ReasonDependencyFailed, st.ServerUID.LastAttempt.Reason)
	assert.False(t, st.ServerUID.Scheduled(), "suspended for the rest of the outage")
	assert.True(t, st.ServerUID.LastAttempt.StartedAt.IsZero(), "recorded, never dispatched")
}

// A context that comes back is re-read and dialed again — with no failure streak, since a
// departure was the user's edit, not a fault.
func TestAReturningContextIsDialedAgain(t *testing.T) {
	t.Parallel()

	cfg := serving(serveAPI(t), "prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()

	cfg.rotate("prod", "")
	cfg.changed()
	require.Eventually(t, lease.Departed, testutil.Timeout, time.Millisecond)

	cfg.rotate("prod", "key-1")
	cfg.changed()
	require.Eventually(t, func() bool { return !lease.Departed() }, testutil.Timeout, time.Millisecond)

	require.Eventually(t, func() bool {
		return lease.State().Phase() == PhaseProbed
	}, testutil.Timeout, time.Millisecond)
	assert.Zero(t, lease.State().Connection.Failures)
}

// A resolve failure keeps its cadence, and consecutive failures are one streak: FailingSince is
// when it began, which the widening ladder means a count alone cannot give.
func TestAResolveFailureKeepsAsking(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	cfg.err = errors.New("open ca.crt: no such file")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	first := awaitState(t, watched, func(st State) bool {
		return st.Connection.Failures >= 1
	}).Connection
	assert.True(t, first.Scheduled(), "a failure earns a retry")

	// The retry sits out on the ladder; the wake stands in for it, as a worked answer the
	// schedule would eventually produce.
	s.supervisor.Wake("prod", nameConnection)

	second := awaitState(t, watched, func(st State) bool {
		return st.Connection.Failures >= 2
	}).Connection
	assert.Equal(t, first.FailingSince, second.FailingSince, "one run of failures, not two")
}

// The supervisor hands back what a run committed when it drops the value — a commit refused because
// the context was released mid-run, a Skip, a panic. Nothing else can reach that connection: it
// reached neither a snapshot nor an entry.
func TestDiscardRetiresWhatTheRunBuilt(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveAPI(t))

	(&connectionProbe{}).Discard(connInfo{conn: conn})

	<-conn.Done()
}

// A context that resolves to nothing commits a value with no connection, which is the ordinary
// shape of a dropped one.
func TestDiscardOfAValueWithNoConnectionIsANoOp(t *testing.T) {
	t.Parallel()

	(&connectionProbe{}).Discard(connInfo{departed: true})
}

// --- readiness ---

// A healthy server's answer is the zero ComponentStatus, so the first one has to be committed
// even though it equals what Prev reads: the value is what dates the observation, and a cluster
// that has never had a failing component would otherwise read as never observed.
func TestReadinessCommitsItsFirstHealthyAnswer(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveCluster(t).Server)

	res, v, committed := runProbe(t, readinessProbe{}, conn, nil)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.True(t, committed, "the first answer lands whatever it says")
	assert.Empty(t, v.Failing)
}

// Every healthy answer after it moves nothing, and the supervisor wakes whoever watches a committed
// value — so a cluster that stays ready must stop committing.
func TestReadinessCommitsNothingWhileTheServerStaysReady(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveCluster(t).Server)

	res, _, committed := runProbe(t, readinessProbe{}, conn, &ComponentStatus{})

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.False(t, committed)
}

// The endpoint's failure is its answer: a 500 names the checks that are not ok, which is the
// whole reason to ask a server whether it is ready rather than whether it answers.
func TestReadinessNamesTheComponentsThatFailed(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusInternalServerError,
		"[+]ping ok\n[-]etcd failed: reason withheld\n[+]log ok\n[-]informer-sync failed: reason withheld\nreadyz check failed\n")

	res, v, committed := runProbe(t, readinessProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonComponentsFailing, res.Reason())
	assert.Equal(t, []string{"etcd", "informer-sync"}, v.Failing)
	assert.True(t, committed)
	assert.Contains(t, res.Err().Error(), "etcd")
}

// A 500 from something that is not the readyz handler answered, but not with the one thing this
// endpoint's failure is supposed to carry.
func TestReadinessReportsA500ThatNamesNothing(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusInternalServerError, "internal server error\n")

	res, _, committed := runProbe(t, readinessProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonInternalError, res.Reason())
	assert.False(t, committed)
}

// Some managed distributions do not serve it at all, and will not start: terminal for this
// connection rather than a failure to retry.
func TestReadinessSuspendsWhenTheEndpointIsAbsent(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusNotFound, "404 page not found")

	res, _, _ := runProbe(t, readinessProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, supervisor.VerdictSuspended, res.Verdict())
	assert.Equal(t, ReasonUnsupported, res.Reason())
}

func TestReadinessClassifiesAnythingElse(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusForbidden, "no")

	res, _, _ := runProbe(t, readinessProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonForbidden, res.Reason())
}

// A cluster that recovers has to clear what it reported, or the last failure stands as the
// answer for as long as the connection lives.
func TestReadinessClearsWhatItReportedWhenTheServerRecovers(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveCluster(t).Server)

	res, v, committed := runProbe(t, readinessProbe{}, conn, &ComponentStatus{Failing: []string{"etcd"}})

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.True(t, committed)
	assert.Empty(t, v.Failing)
}

// The supervisor wakes every probe watching a committed value, so a set that did not move must not
// be committed again.
func TestReadinessCommitsOnlyWhenTheSetMoves(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusInternalServerError, "[-]etcd failed: reason withheld\nreadyz check failed\n")

	_, _, committed := runProbe(t, readinessProbe{}, connTo(t, cs.Server), &ComponentStatus{Failing: []string{"etcd"}})

	assert.False(t, committed)
}

func TestReadinessParksWithoutAConnection(t *testing.T) {
	t.Parallel()

	res, _, _ := runProbe(t, readinessProbe{}, nil, nil)

	assert.True(t, res.IsSkip())
}

// --- serverUID ---

func TestServerUIDReadsKubeSystem(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)

	res, v, committed := runProbe(t, serverUIDProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.Equal(t, "uid-1", v)
	assert.True(t, committed)
	assert.Equal(t, kubeSystemPath, cs.requests.Await(t, "the namespace read"))
}

func TestServerUIDCommitsOnlyWhenTheUIDMoves(t *testing.T) {
	t.Parallel()

	uid := "uid-1"

	res, _, committed := runProbe(t, serverUIDProbe{}, connTo(t, serveCluster(t).Server), &uid)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.False(t, committed)
}

// The stamp is not the commit. A rebuilt connection to a cluster whose UID never moved
// commits nothing — and must still be stamped, or it stays unidentified forever and every
// caller scoped to that cluster refuses it.
func TestServerUIDStampsTheConnectionEvenWhenItCommitsNothing(t *testing.T) {
	t.Parallel()

	uid := "uid-1"
	conn := connTo(t, serveCluster(t).Server)

	_, _, committed := runProbe(t, serverUIDProbe{}, conn, &uid)

	require.False(t, committed)
	got, ok := conn.ServerUID()
	require.True(t, ok, "the connection is identified by the read, not by the commit")
	assert.Equal(t, "uid-1", got)
}

// The identity is recorded on the connection the request went over, which is what makes it
// safe to compare later: no other party knows which connection answered.
func TestServerUIDStampsTheConnectionItRead(t *testing.T) {
	t.Parallel()

	conn := connTo(t, serveCluster(t).Server)

	runProbe(t, serverUIDProbe{}, conn, nil)

	got, ok := conn.ServerUID()
	require.True(t, ok)
	assert.Equal(t, "uid-1", got)
}

// A read that failed identifies nothing: stamping on the way past would pin an identity no
// answer supports, and the stamp is set-once.
func TestServerUIDStampsNothingWhenTheReadFails(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(kubeSystemPath, http.StatusForbidden, "")
	conn := connTo(t, cs.Server)

	runProbe(t, serverUIDProbe{}, conn, nil)

	_, ok := conn.ServerUID()
	assert.False(t, ok)
}

// The trap the reason vocabulary exists for: this 404 is the namespace, not the endpoint, so the
// probe keeps asking — kube-system can be created.
func TestServerUIDReportsAMissingNamespaceAsNotFound(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(kubeSystemPath, http.StatusNotFound, `{"kind":"Status","reason":"NotFound"}`)

	res, _, _ := runProbe(t, serverUIDProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, supervisor.VerdictFailed, res.Verdict())
	assert.Equal(t, ReasonNotFound, res.Reason())
}

// A namespace-scoped user reads this as a grant to fix, which is what tells a healthy cluster
// with no cache from an unreachable one.
func TestServerUIDReportsAForbiddenRead(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(kubeSystemPath, http.StatusForbidden, "no")

	res, _, _ := runProbe(t, serverUIDProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonForbidden, res.Reason())
}

func TestServerUIDReportsANamespaceWithNoUID(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.answer(kubeSystemPath, `{"metadata":{"name":"kube-system"}}`)

	res, _, committed := runProbe(t, serverUIDProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonMalformed, res.Reason())
	assert.False(t, committed)
}

func TestServerUIDParksWithoutAConnection(t *testing.T) {
	t.Parallel()

	res, _, _ := runProbe(t, serverUIDProbe{}, nil, nil)

	assert.True(t, res.IsSkip())
}

// --- serverVersion ---

func TestServerVersionReadsTheReportedVersion(t *testing.T) {
	t.Parallel()

	res, v, committed := runProbe(t, serverVersionProbe{}, connTo(t, serveCluster(t).Server), nil)

	assert.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.Equal(t, VersionInfo{GitVersion: "v1.31.4", Major: "1", Minor: "31"}, v)
	assert.True(t, committed)
}

func TestServerVersionCommitsOnlyWhenTheVersionMoves(t *testing.T) {
	t.Parallel()

	prev := VersionInfo{GitVersion: "v1.31.4", Major: "1", Minor: "31"}

	_, _, committed := runProbe(t, serverVersionProbe{}, connTo(t, serveCluster(t).Server), &prev)

	assert.False(t, committed)
}

func TestServerVersionReportsAnAnswerWithNoVersion(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.answer(versionPath, `{"major":"1"}`)

	res, _, _ := runProbe(t, serverVersionProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonMalformed, res.Reason())
}

func TestServerVersionClassifiesAFailure(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(versionPath, http.StatusServiceUnavailable, "restarting")

	res, _, _ := runProbe(t, serverVersionProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonServiceUnavailable, res.Reason())
}

func TestServerVersionParksWithoutAConnection(t *testing.T) {
	t.Parallel()

	res, _, _ := runProbe(t, serverVersionProbe{}, nil, nil)

	assert.True(t, res.IsSkip())
}

// --- principal ---

// The server names the subject; a token does not name it to us. The review is a create, so it
// carries a body — an empty POST is a 400 and the username silently comes back missing.
func TestPrincipalAsksTheServerWhoWeAre(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	var got struct {
		method, contentType, body string
	}
	cs.route(selfSubjectReviewPath, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method, got.contentType, got.body = r.Method, r.Header.Get("Content-Type"), string(body)
		_, _ = io.WriteString(w, reviewJSON)
	})

	res, v, committed := runProbe(t, principalProbe{}, connTo(t, cs.Server), nil)

	require.Equal(t, supervisor.VerdictSucceeded, res.Verdict())
	assert.Equal(t, "admin@example", v.Username)
	assert.Equal(t, []string{"system:authenticated", "system:masters"}, v.Groups, "sorted")
	assert.True(t, committed)
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "application/json", got.contentType)
	assert.Equal(t, string(selfSubjectReviewBody), got.body)
}

// Sorting is what makes the guard hold: the same groups in another order are not news any
// watcher of this value has to be woken for.
func TestPrincipalCommitsOnlyWhenTheSubjectMoves(t *testing.T) {
	t.Parallel()

	prev := Principal{Username: "admin@example", Groups: []string{"system:authenticated", "system:masters"}}

	_, _, committed := runProbe(t, principalProbe{}, connTo(t, serveCluster(t).Server), &prev)

	assert.False(t, committed)
}

func TestPrincipalCommitsAChangedUsername(t *testing.T) {
	t.Parallel()

	prev := Principal{Username: "reader@example", Groups: []string{"system:authenticated", "system:masters"}}

	_, v, committed := runProbe(t, principalProbe{}, connTo(t, serveCluster(t).Server), &prev)

	assert.True(t, committed)
	assert.Equal(t, "admin@example", v.Username)
}

// Before 1.27 the endpoint does not exist, and a server too old to serve it will not grow one.
func TestPrincipalSuspendsOnAServerTooOldToAnswer(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(selfSubjectReviewPath, http.StatusNotFound, "404 page not found")

	res, _, _ := runProbe(t, principalProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, supervisor.VerdictSuspended, res.Verdict())
	assert.Equal(t, ReasonUnsupported, res.Reason())
}

func TestPrincipalReportsAReviewWithNoUsername(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.answer(selfSubjectReviewPath, `{"status":{}}`)

	res, _, _ := runProbe(t, principalProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonMalformed, res.Reason())
}

func TestPrincipalClassifiesAFailure(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(selfSubjectReviewPath, http.StatusUnauthorized, "no")

	res, _, _ := runProbe(t, principalProbe{}, connTo(t, cs.Server), nil)

	assert.Equal(t, ReasonUnauthorized, res.Reason())
}

func TestPrincipalParksWithoutAConnection(t *testing.T) {
	t.Parallel()

	res, _, _ := runProbe(t, principalProbe{}, nil, nil)

	assert.True(t, res.IsSkip())
}

// --- the caller going away ---

// Cancellation says nothing about the cluster: the deadline was not ours and the answer was not
// refused, so a run the supervisor's shutdown cut short must record nothing rather than opening a
// failure streak against a healthy server.
func TestACanceledRunRecordsNothing(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	conn := connTo(t, cs.Server)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, tt := range []struct {
		name string
		run  func() supervisor.Result
	}{
		{nameConnection, func() supervisor.Result {
			cfg := serving(cs.Server, "prod", "key-1")
			prev := connInfo{conn: conn, fingerprint: "key-1"}
			return (&connectionProbe{kubecfgSvc: cfg}).Run(ctx, supervisor.NewJobPass("prod", &prev, supervisor.Snapshot{}))
		}},
		{nameReadiness, func() supervisor.Result { return runCanceled(t, ctx, readinessProbe{}, conn) }},
		{nameServerUID, func() supervisor.Result { return runCanceled(t, ctx, serverUIDProbe{}, conn) }},
		{nameServerVersion, func() supervisor.Result { return runCanceled(t, ctx, serverVersionProbe{}, conn) }},
		{namePrincipal, func() supervisor.Result { return runCanceled(t, ctx, principalProbe{}, conn) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.run().IsSkip(), "a canceled run records nothing")
		})
	}
}

// runCanceled runs one probe body on a context that is already done.
func runCanceled[T any](t *testing.T, ctx context.Context, p supervisor.Job[T], conn *Connection) supervisor.Result {
	t.Helper()
	snap := supervisor.NewSnapshot(map[string]any{nameConnection: connInfo{conn: conn}})
	return p.Run(ctx, supervisor.NewJobPass[T]("prod", nil, snap))
}

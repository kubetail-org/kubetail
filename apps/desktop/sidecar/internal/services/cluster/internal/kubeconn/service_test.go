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
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// fakeKubeconfig resolves each context to a key the test controls, standing in for the user's
// file. err, when set, is what every context fails with.
//
// Every context resolves to the same host, since what a test varies is the key: the key is the
// fingerprint, which is what a rebuild is decided on.
type fakeKubeconfig struct {
	mu     sync.Mutex
	keys   map[string]string
	host   string
	tls    rest.TLSClientConfig
	err    error
	hub    *watch.Hub[*api.Config]
	onRead func()
	// reads names each context read, for a test counting them rather than their answers.
	reads *testutil.Probe[string]
	// startQuiet hands Start a subscription that has read the current config already, so only
	// a change wakes a probe: a test that counts runs has none of its own from startup.
	startQuiet bool
}

// resolving is a kubeconfig where each context resolves to the key beside it, aimed at an
// address nothing answers on.
func resolving(contextKeys ...string) *fakeKubeconfig {
	f := &fakeKubeconfig{keys: map[string]string{}, host: deadHost}
	for i := 0; i < len(contextKeys); i += 2 {
		f.keys[contextKeys[i]] = contextKeys[i+1]
	}
	return f
}

// serving is resolving against a server that answers: every context aims at srv and trusts its
// certificate.
func serving(srv *httptest.Server, contextKeys ...string) *fakeKubeconfig {
	f := resolving(contextKeys...)
	f.host = srv.URL
	f.tls = rest.TLSClientConfig{CAData: caPEM(srv)}
	return f
}

// counting is resolving with the reads probe armed.
func counting(contextKeys ...string) *fakeKubeconfig {
	f := resolving(contextKeys...)
	f.reads = testutil.NewProbe[string](8)
	return f
}

// rotate is the user editing their file: contextName now names different credentials. An
// empty key removes the context.
func (f *fakeKubeconfig) rotate(contextName, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if key == "" {
		delete(f.keys, contextName)
		return
	}
	f.keys[contextName] = key
}

func (f *fakeKubeconfig) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// duringRead runs fn inside the next read, so a test can interleave an event with a read in
// flight. Latency in the code under test on purpose — the test's own assertions stay immediate.
func (f *fakeKubeconfig) duringRead(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.onRead = fn
}

func (f *fakeKubeconfig) RESTConfig(contextName string) (*rest.Config, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.reads != nil {
		f.reads.Fire(contextName)
	}
	if fn := f.onRead; fn != nil {
		f.onRead = nil
		fn()
	}
	if f.err != nil {
		return nil, "", f.err
	}
	key, ok := f.keys[contextName]
	if !ok {
		return nil, "", kubeconfig.ErrContextNotFound
	}
	return &rest.Config{Host: f.host, TLSClientConfig: f.tls}, key, nil
}

func (f *fakeKubeconfig) WatchConfigState() kubeconfig.ConfigStateWatch {
	if f.hub == nil {
		f.hub = watch.New[*api.Config](nil)
	}
	rx := f.hub.Receiver()
	if f.startQuiet {
		_, _ = rx.TryRecv()
	}
	return rx
}

// changed is the user saving their file: the watch reports it moving.
func (f *fakeKubeconfig) changed() { f.hub.Sender().Send(&api.Config{}) }

// within bounds a wait for something that must arrive, so a regression fails the test rather
// than hanging it until the suite's own deadline.
func within(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Timeout)
	t.Cleanup(cancel)
	return ctx
}

// startService runs the pool's supervisor and watch for a test that needs them, and joins them on
// cleanup.
func startService(t *testing.T, s *Service) {
	t.Helper()
	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
}

// settled waits until the pool has published a first verdict for contextName, so a test
// subscribing after it hears the news it goes on to cause rather than the first probe's. It reads
// the pool's own record, not the claim's State: the supervisor commits before it dispatches the
// publish, so State can show a verdict whose signal is still a dispatch away.
func settled(t *testing.T, s *Service, contextName string) {
	t.Helper()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.published[contextName].phase != PhasePending
	}, testutil.Timeout, time.Millisecond, "a first verdict")
}

// awaitState receives one claim's states until cond holds. The hub keeps only the latest value,
// so frames in between may be skipped — cond must be a level, never an edge.
func awaitState(t *testing.T, sub *StateWatch, cond func(State) bool) State {
	t.Helper()
	ctx := within(t)
	for {
		st, err := sub.RecvContext(ctx)
		require.NoError(t, err, "state frame")
		if cond(st) {
			return st
		}
	}
}

// --- claims ---

// A claim is taken whatever the file says. Whether the context resolves is the pool's to find
// out on its own schedule, and a claim is how the holder hears about it — refusing would make a
// transient fact permanent for the caller.
func TestAcquireClaimsAContextTheKubeconfigDoesNotName(t *testing.T) {
	t.Parallel()

	s := New(&fakeKubeconfig{})

	lease := s.Acquire("prod")
	defer lease.Release()

	assert.Len(t, s.claimed, 1)
	assert.Equal(t, State{}, lease.State())
}

// Contexts that resolve alike are deliberately not merged: each gets its own entry, so what is
// read about one belongs to one context.
func TestAcquireBuildsAnEntryPerContext(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1", "prod-admin", "key-1"))

	defer s.Acquire("prod").Release()
	defer s.Acquire("prod-admin").Release()

	assert.Len(t, s.claimed, 2)
	assert.NotSame(t, s.claimed["prod"], s.claimed["prod-admin"])
}

// An entry nobody holds is one nothing tracks, so the last release has to drop it.
func TestReleaseDropsTheEntryTheLastHolderHeld(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))

	s.Acquire("prod").Release()

	assert.Empty(t, s.claimed)
}

// Two holders of one context — the cluster pass and a log tail — are two claims. The first to
// finish must not stop the pool tracking it for the other.
func TestReleaseKeepsTheEntryWhileAnotherHolderHoldsIt(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	pass := s.Acquire("prod")
	tail := s.Acquire("prod")

	pass.Release()
	assert.Len(t, s.claimed, 1)

	tail.Release()
	assert.Empty(t, s.claimed)
}

// Release is idempotent by contract, so it is safe to defer beside a path that also releases.
func TestReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	other := s.Acquire("prod")

	lease.Release()
	lease.Release()

	assert.Len(t, s.claimed, 1, "the second release must not drop the other holder's entry")
	other.Release()
}

// A claim released twice — or one whose entry the pool no longer holds — is a no-op rather than
// a decrement into the negative.
func TestReleaseIsANoOpOnceTheEntryIsGone(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	lease.Release()
	require.Empty(t, s.claimed)

	lease.Release()

	assert.Empty(t, s.claimed)
}

// Close drops entries out from under the leases still holding them, so a lease released
// afterwards must not decrement whatever has claimed its name since.
func TestAPreCloseReleaseDoesNotDropALaterClaim(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	stale := s.Acquire("prod")
	require.NoError(t, s.Close())

	fresh := s.Acquire("prod")
	defer fresh.Release()
	stale.Release()

	assert.Len(t, s.claimed, 1, "the later claim still holds its entry")
	assert.False(t, fresh.Departed())
}

// A claim reads as departed once nothing holds its context: there is no entry left to answer
// from, and reporting present would be a claim about a context the pool stopped tracking.
func TestAReleasedClaimReadsAsDeparted(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	require.False(t, lease.Departed())

	lease.Release()

	assert.True(t, lease.Departed())
	assert.Equal(t, State{}, lease.State())
}

// Close drops what the pool holds, so a claim outliving it reads as nothing known rather than
// as a context the pool is still tracking.
func TestCloseDropsWhatThePoolHolds(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	require.False(t, lease.Departed())

	require.NoError(t, s.Close())

	assert.Empty(t, s.claimed)
	assert.True(t, lease.Departed())
}

// --- retiring connections ---

// The probe builds and the pool retires, so a rebuild hands back what it replaced: a run cannot
// retire the connection it is replacing, since its commit lands after it returns and holders
// would reconnect against a Conn still handing out the dead one.
func TestRecordHandsBackTheConnectionItReplaced(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	defer s.Acquire("prod").Release()
	first, second := connTo(t, serveAPI(t)), connTo(t, serveAPI(t))

	stale, held, _ := s.record("prod", first, news{})
	require.True(t, held)
	assert.Nil(t, stale, "the first connection replaces nothing")

	stale, held, _ = s.record("prod", second, news{})

	assert.True(t, held)
	assert.Same(t, first, stale)
}

// The leak this rule exists for: a release landing between the commit and the pass retires the
// entry's connection, which is the *previous* one — the connection the run just built is held by
// an entry that no longer exists, so nothing else can reach it.
func TestRecordHandsBackAConnectionNothingHoldsAnyMore(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	s.Acquire("prod").Release()
	built := connTo(t, serveAPI(t))

	stale, held, _ := s.record("prod", built, news{})

	assert.False(t, held)
	assert.Same(t, built, stale, "nothing else can reach it")
}

// The entry check and the published write are one critical section, so a pass that raced the
// last release files nothing: a baseline left behind for a context nobody holds is one the first
// pass of the next claim compares equal to, telling the fleet nothing about a context that just
// came back.
func TestRecordFilesNothingForAContextNobodyHolds(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	probed := news{phase: PhaseProbed}
	_, _, _ = s.record("prod", nil, probed)
	lease.Release()

	_, held, _ := s.record("prod", nil, probed)

	assert.False(t, held)
	assert.Empty(t, s.published, "no baseline for the claim that comes next")
}

// An entry that goes takes its connection with it, or every released context leaves its sockets
// behind.
func TestReleaseRetiresTheConnectionTheEntryHeld(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	conn := connTo(t, serveAPI(t))
	_, _, _ = s.record("prod", conn, news{})

	lease.Release()

	<-conn.Done()
}

func TestCloseRetiresEveryConnection(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	defer s.Acquire("prod").Release()
	conn := connTo(t, serveAPI(t))
	_, _, _ = s.record("prod", conn, news{})

	require.NoError(t, s.Close())

	<-conn.Done()
}

// --- handing a connection out ---

// What the probe built is what a holder gets, without dialing anything of its own.
func TestConnHandsOutWhatTheProbeBuilt(t *testing.T) {
	t.Parallel()

	srv := serveAPI(t)
	s := New(serving(srv, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitState(t, watched, func(st State) bool { return st.Phase() == PhaseProbed })

	conn, err := lease.Conn(t.Context())

	require.NoError(t, err)
	assert.Equal(t, srv.URL, conn.BaseURL.String())
}

// A context that resolves to nothing has no connection to hand out, and saying so is what stops
// a holder interpreting a nil.
func TestConnReportsThatThereIsNoConnection(t *testing.T) {
	t.Parallel()

	s := New(&fakeKubeconfig{})
	lease := s.Acquire("prod")
	defer lease.Release()

	conn, err := lease.Conn(t.Context())

	assert.Nil(t, conn)
	assert.ErrorIs(t, err, ErrNoConnection)
}

// A commit that lands after the pass worker has stopped leaves the connection in the supervisor and
// nowhere else, so Close reads it back rather than retiring only what the entries hold.
func TestCloseRetiresTheConnectionTheEngineHolds(t *testing.T) {
	t.Parallel()

	s := New(serving(serveCluster(t).Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitState(t, watched, func(st State) bool { return st.Phase() == PhaseProbed })
	conn, err := lease.Conn(t.Context())
	require.NoError(t, err)

	require.NoError(t, s.Close())

	<-conn.Done()
}

// A claim the pool no longer holds has no connection to answer with, and saying so is what stops
// a holder reading one belonging to whatever claims the name next.
func TestConnReportsNothingOnceTheClaimIsReleased(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	lease.Release()

	conn, err := lease.Conn(t.Context())

	assert.Nil(t, conn)
	assert.ErrorIs(t, err, ErrNoConnection)
}

// --- the news: a context leaving the kubeconfig ---

// The one thing a holder is told without asking. It learns what changed by asking the claim.
func TestDepartureReachesTheHolder(t *testing.T) {
	t.Parallel()

	cfg := counting("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	require.Equal(t, "prod", cfg.reads.Await(t, "the claim's own read"))
	settled(t, s, "prod")
	news := s.ListenForConnectionChanges()
	defer news.Close()
	// `settled` returns once the first verdict is recorded, a step before its signal is sent.
	// That signal, read below, would pass for the departure — and read that early, the state
	// it asserts on is one the departure has not reached yet.
	settleNotifications(t, news)

	cfg.rotate("prod", "")
	cfg.changed()

	ev, err := news.RecvContext(within(t))
	require.NoError(t, err)
	assert.Equal(t, "prod", ev.Key)
	assert.True(t, lease.Departed(), "the state is committed before the signal is sent")
}

// The claim outlives what it is a claim on: the file may name the context again, and this claim
// is how the holder hears about that.
func TestDepartureKeepsTheClaim(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()

	cfg.rotate("prod", "")
	cfg.changed()
	require.Eventually(t, lease.Departed, testutil.Timeout, time.Millisecond)

	cfg.rotate("prod", "key-1")
	cfg.changed()

	require.Eventually(t, func() bool { return !lease.Departed() },
		testutil.Timeout, time.Millisecond, "named again, and the same claim reports it")
	assert.Len(t, s.claimed, 1)
}

// A departed context is announced once. Announcing it on every kubeconfig write would wake its
// holder forever for news it already has.
func TestDepartureIsAnnouncedOnce(t *testing.T) {
	t.Parallel()

	cfg := counting("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	cfg.reads.Await(t, "the claim's own read")
	settled(t, s, "prod")
	news := s.ListenForConnectionChanges()
	defer news.Close()
	// The baseline: `settled` returns once the first verdict is recorded, a step before its
	// signal is sent. That signal, read below, would pass for the departure, and the
	// departure's own signal for a second announcement.
	settleNotifications(t, news)

	cfg.rotate("prod", "")
	cfg.changed()
	_, err := news.RecvContext(within(t))
	require.NoError(t, err)

	cfg.changed()
	cfg.reads.Await(t, "the re-read the change woke")

	// A negative assertion needs a bounded window: this fails the instant a second one lands.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = news.RecvContext(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// An unread kubeconfig names nothing. Reporting that as a departure would tell every holder its
// context is gone for as long as the first read takes.
func TestUnreadKubeconfigIsNotADeparture(t *testing.T) {
	t.Parallel()

	cfg := counting("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	cfg.reads.Await(t, "the claim's own read")

	cfg.setErr(kubeconfig.ErrNotRead)
	cfg.changed()
	cfg.reads.Await(t, "the re-read the change woke")

	assert.False(t, lease.Departed())
}

// A context the file still names but cannot resolve — a cluster entry it points at that is
// gone, credentials that will not load — has not departed. It is the file that is broken, and
// saying otherwise reports a removal the user did not make.
func TestAResolveFailureIsNotADeparture(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	cfg.err = errors.New("no such certificate authority file")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	awaitState(t, watched, func(st State) bool {
		return st.Connection.LastAttempt.Reason == ReasonResolveFailed
	})

	assert.False(t, lease.Departed())
}

// A holder watching its own claim is told too, not just the fleet feed, and the value carries
// why: a probe that suspends has to record its reason or nothing explains the suspension.
func TestDepartureReachesAClaimWatcher(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	cfg.rotate("prod", "")
	cfg.changed()

	// Not any departed frame: one can show a woken re-read still in flight. The settled one is
	// what the suspension is asserted on.
	st := awaitState(t, watched, func(st State) bool {
		return st.Connection.LastAttempt.Reason == ReasonContextNotFound && !st.Connection.InFlight()
	})
	assert.False(t, st.Connection.Scheduled(), "the watch reports the file moving; polling asks nothing new")
}

// --- the asks behind the news ---

// A new context asks for its connection to be probed; the claim does not wait for it. Reading
// the kubeconfig is not work to do on the caller's thread.
func TestAcquireAsksForANewContextsPresence(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	require.Eventually(t, func() bool { return !lease.Departed() },
		testutil.Timeout, time.Millisecond, "presence was never read")

	cfg.rotate("prod", "")
	cfg.changed()

	require.Eventually(t, lease.Departed,
		testutil.Timeout, time.Millisecond, "a kubeconfig change never reached the claim")
}

// A later holder joins what the first one's probe found rather than asking for another read.
// Only the supervisor runs: the kubeconfig watch's subscription seed wakes a re-read this test must
// not count, and coalescing makes that wake land as zero or one extra reads — so it stays off.
func TestAcquireAsksOnlyForANewContext(t *testing.T) {
	t.Parallel()

	cfg := counting("prod", "key-1")
	s := New(cfg)
	stop := s.supervisor.Start(t.Context())
	t.Cleanup(func() { assert.NoError(t, stop(context.Background())) })
	first := s.Acquire("prod")
	defer first.Release()
	require.Equal(t, "prod", cfg.reads.Await(t, "the first claim's read"))

	second := s.Acquire("prod")
	defer second.Release()

	// A negative assertion needs a bounded window: this fails the instant a read lands.
	testutil.NoRecv(t, cfg.reads.Chan(), 50*time.Millisecond, "a second holder asked for a read")
}

// A kubeconfig change landing while a read is in flight earns a read of its own: the one out
// had already passed the file when it moved.
func TestAChangeArrivingDuringAReadIsReadAgain(t *testing.T) {
	t.Parallel()

	cfg := counting("prod", "key-1")
	s := New(cfg)
	startService(t, s)
	cfg.duringRead(cfg.changed)

	defer s.Acquire("prod").Release()

	assert.Equal(t, "prod", cfg.reads.Await(t, "the claim's read"))
	assert.Equal(t, "prod", cfg.reads.Await(t, "the read the mid-flight change earned"))
}

// The ask a claim makes before Start waits in the supervisor's queues for its workers, so a context
// that had already gone does not sit reported as present.
func TestAClaimTakenBeforeStartIsChecked(t *testing.T) {
	t.Parallel()

	s := New(resolving("staging", "key-1")) // "prod" is not named
	lease := s.Acquire("prod")
	defer lease.Release()
	require.False(t, lease.Departed(), "nothing has read the file yet")

	startService(t, s)

	require.Eventually(t, lease.Departed, testutil.Timeout, time.Millisecond,
		"a claim taken before Start was never checked")
}

// --- the state hub ---

// A receiver is bound to its key for life, and a claim's key is its context — which never moves,
// however the credentials behind it do.
func TestWatchStateIsKeyedByContext(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	defer lease.Release()
	sub := lease.WatchState()
	defer sub.Close()
	_, err := sub.RecvContext(within(t))
	require.NoError(t, err, "the state on attach")

	s.stateHub.Sender().Send("prod", State{ServerUID: Observation[string]{Value: "uid-1", LastSeenAt: runAt}})

	st, err := sub.RecvContext(within(t))
	require.NoError(t, err)
	assert.Equal(t, "uid-1", st.ServerUID.Value)
}

// A state receiver is the caller's, not the claim's: releasing does not close it, which is why
// the contract says to. One kept past its claim holds a hub slot and reports whatever claims
// that context next.
func TestReleaseDoesNotCloseAStateWatcher(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	watched := lease.WatchState()
	defer watched.Close()
	_, err := watched.RecvContext(within(t))
	require.NoError(t, err, "the state on attach")

	lease.Release()
	s.stateHub.Sender().Send("prod", State{ServerUID: Observation[string]{Value: "uid-1", LastSeenAt: runAt}})

	st, err := watched.RecvContext(within(t))
	require.NoError(t, err, "the watch is still live, and still the caller's to close")
	assert.Equal(t, "uid-1", st.ServerUID.Value)
}

// The subscriptions are taken before Start returns, so a config sent straight after it is read
// rather than dropped, and stop joins the loops instead of leaving them running.
func TestStartWatchesTheKubeconfigUntilStopped(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)

	stop, err := s.Start(t.Context())
	require.NoError(t, err)
	require.NotNil(t, cfg.hub, "Start subscribes before it returns")
	cfg.changed()

	require.NoError(t, stop(t.Context()))
}

// A second Start is refused, and the first one's stop still drains what it started.
func TestStartRejectsASecondStart(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	stop, err := s.Start(t.Context())
	require.NoError(t, err)

	_, err = s.Start(t.Context())
	assert.ErrorContains(t, err, "already started")

	require.NoError(t, stop(t.Context()))
}

// A pass can land after the last holder let go — the supervisor's Remove and this publish are not
// one step. Announcing then would tell watchers on this name about a claim nobody holds, and
// leave news behind for whatever claims it next to be compared against.
func TestAPublishForAReleasedClaimIsDropped(t *testing.T) {
	t.Parallel()

	s := New(resolving("prod", "key-1"))
	lease := s.Acquire("prod")
	v, ok := s.supervisor.Read("prod")
	require.True(t, ok)
	s.publish("prod", v)
	require.Contains(t, s.published, "prod", "a held claim publishes")

	lease.Release()
	s.publish("prod", v)

	assert.NotContains(t, s.published, "prod")
}

// The feed ending is one of the two ways the watch is meant to finish; the other is its context
// being cancelled. Neither leaves the goroutine parked.
func TestTheKubeconfigWatchEndsWhenItsFeedCloses(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	s := New(cfg)
	cfgs := cfg.WatchConfigState()
	cfgs.Close()

	testutil.WaitReturn(t, func() { s.watchKubeconfig(context.Background(), cfgs) }, "the watch to end")
}

// A cluster that has never been ready still has an answer worth reading: the failing components
// are what a caller came for, and an observation nothing dates reads as never observed.
func TestAFailingReadinessAnswerIsDated(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cs.fail(readyzPath, http.StatusInternalServerError, "[-]etcd failed: reason withheld\nreadyz check failed\n")
	s := New(serving(cs.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	st := awaitState(t, watched, func(st State) bool { return st.Readiness.LastAttempt.Done() })

	assert.Equal(t, ReasonComponentsFailing, st.Readiness.LastAttempt.Reason)
	assert.Equal(t, []string{"etcd"}, st.Readiness.Value.Failing)
	assert.True(t, st.Readiness.Known(), "the component list was read")
	assert.False(t, st.Readiness.OK(), "read, and not ready")
}

// --- retry ---

// probed waits until every probe has answered — settled is the connection alone, which
// the four behind it can still be running past. Answered is not yet quiet: a startup
// re-run may still be in flight, which the caller settles off the request log itself.
func probed(t *testing.T, lease Lease) {
	t.Helper()
	require.Eventually(t, func() bool {
		st := lease.State()
		return st.Connection.LastAttempt.Done() && st.Readiness.LastAttempt.Done() &&
			st.ServerUID.LastAttempt.Done() && st.ServerVersion.LastAttempt.Done() &&
			st.Principal.LastAttempt.Done()
	}, testutil.Timeout, time.Millisecond, "every probe to answer")
}

// All five, not the connection alone: a connection that is already up commits nothing, so
// waking it would leave the probes behind it sitting on the answer the user just fixed.
func TestRetryRerunsEveryProbe(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	probed(t, lease)
	// probed means answered, not quiet: startup re-runs a probe that was woken or
	// re-added while its first run was still in flight, so a straggler request can land
	// after every probe has an answer. No event announces the last one, so — like a
	// negative assertion — going quiet takes a bounded window, sized far above a
	// scheduling hiccup plus a loopback request; every probe is then scheduled minutes
	// out, so nothing reaches the server unbidden.
	for quiet := false; !quiet; {
		select {
		case <-c.requests.Chan():
		case <-time.After(100 * time.Millisecond):
			quiet = true
		}
	}

	require.NoError(t, s.RetryAndWait(within(t), "prod"))

	want := map[string]bool{
		apiDiscoveryPath: true, readyzPath: true, kubeSystemPath: true,
		versionPath: true, selfSubjectReviewPath: true,
	}
	// Until want empties rather than the next five exactly: a wake landing mid-run is
	// redelivered, so a probe may be asked twice, and a duplicate does not falsify
	// "every probe asked again". One never asked leaves Recv on its failsafe.
	for len(want) > 0 {
		delete(want, testutil.Recv(t, c.requests.Chan(), "a re-probe request"))
	}
}

// A wake on a subject nothing tracks is a no-op, so the call takes its own claim — otherwise it
// would wait out its ceiling for a run that is never dispatched. The claim is the call's, and
// goes back when it returns.
func TestRetryAndWaitClaimsTheContextItProbes(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)

	require.NoError(t, s.RetryAndWait(within(t), "prod"))

	s.mu.Lock()
	held := len(s.claimed)
	s.mu.Unlock()
	assert.Zero(t, held, "the claim is released with the call")
}

// parkDials makes every dial wait for the test to let it through, so the runs below are ordered
// by the test rather than by how fast the server answers. Installed before anything claims the
// context, so the first dial is the first probe.
func parkDials(t *testing.T, c *cluster) (dialing *testutil.Probe[struct{}], gate chan struct{}) {
	t.Helper()
	dialing, gate = testutil.NewProbe[struct{}](4), make(chan struct{}, 4)
	c.route(apiDiscoveryPath, func(w http.ResponseWriter, _ *http.Request) {
		dialing.Fire(struct{}{})
		<-gate
		_, _ = io.WriteString(w, apiVersions)
	})
	return dialing, gate
}

// The case every client-side attempt at this got wrong. A wake landing while a run is in flight
// finds the key held and is redelivered, so the run the caller asked for is the NEXT one to
// begin — and the one already out must not answer for it.
func TestRetryAndWaitIsNotSatisfiedByARunAlreadyInFlight(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	dialing, gate := parkDials(t, c)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)
	// After the service's own cleanup, so a parked dial is released before stop joins it.
	t.Cleanup(func() { close(gate) })
	lease := s.Acquire("prod")
	defer lease.Release()
	testutil.Recv(t, dialing.Chan(), "the first dial")
	gate <- struct{}{}
	settled(t, s, "prod")

	// The run that must not answer, put in flight from the test's own goroutine: its dial is
	// observed here, so it began before the ask below however the two goroutines interleave.
	s.supervisor.Wake("prod", nameConnection)
	testutil.Recv(t, dialing.Chan(), "the dial already out")

	done := make(chan error, 1)
	go func() { done <- s.RetryAndWait(within(t), "prod") }()
	// The ask's own claim, which it takes after reading its moment and before its wake: the
	// next run begins after the ask whichever side of the wake it lands on.
	awaitHolders(t, s, "prod", 2)
	gate <- struct{}{}
	// The next dial having landed proves the run already out committed and published.
	testutil.Recv(t, dialing.Chan(), "the dial the ask bought")

	// A negative assertion has no event to wait for, so it needs a bounded window: this
	// fails the instant the call returns rather than at the end of the wait.
	testutil.NoRecv(t, done, 50*time.Millisecond, "a return on a run it did not ask for")

	gate <- struct{}{}
	require.NoError(t, testutil.Recv(t, done, "the call to return"))
}

// awaitHolders waits until contextName is claimed by n holders.
func awaitHolders(t *testing.T, s *Service, contextName string, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.claimed[contextName]
		return e != nil && e.holders == n
	}, testutil.Timeout, time.Millisecond, "the claim the ask takes")
}

// The wait is the caller's; the run is the supervisor's. A caller that goes away leaves the probe
// running — a Wake is not a Restart, and cancelling it would tear down work another watcher wants.
func TestRetryAndWaitLeavesTheRunAloneWhenItsCallerGoesAway(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	dialing, gate := parkDials(t, c)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)
	t.Cleanup(func() { close(gate) })
	lease := s.Acquire("prod")
	defer lease.Release()
	testutil.Recv(t, dialing.Chan(), "the first dial")
	gate <- struct{}{}
	settled(t, s, "prod")
	first := lease.State().Connection.LastAttempt

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.RetryAndWait(ctx, "prod") }()
	testutil.Recv(t, dialing.Chan(), "the dial the ask bought")
	cancel()

	require.ErrorIs(t, testutil.Recv(t, done, "the call to return"), context.Canceled)
	gate <- struct{}{}
	require.Eventually(t, func() bool {
		return lease.State().Connection.LastAttempt.StartedAt.After(first.StartedAt)
	}, testutil.Timeout, time.Millisecond, "the abandoned run still finishes")
}

// A run that has begun and does not end is what the ceiling is for. Reporting success there would
// tell the caller a probe finished when none had.
func TestRetryAndWaitGivesUpOnARunThatNeverFinishes(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	dialing, gate := parkDials(t, c)
	// Start's own wake would otherwise put a second run in flight before the ask: its dial
	// parks, and the run the ask buys waits behind it for the probe's own timeout.
	cfg := serving(c.Server, "prod", "key-1")
	cfg.startQuiet = true
	s := New(cfg)
	startService(t, s)
	t.Cleanup(func() { close(gate) })
	lease := s.Acquire("prod")
	defer lease.Release()
	testutil.Recv(t, dialing.Chan(), "the first dial")
	gate <- struct{}{}
	settled(t, s, "prod")

	// The dial the ask buys parks, so its run begins and never ends. The ceiling is a parameter
	// for exactly this: the test never outwaits the production one.
	err := s.retryAndWait(t.Context(), "prod", 50*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// A Skip is a finished run that records nothing — the connection probe declines on an unread
// kubeconfig rather than reporting every context gone. The probe the caller asked for has run, so
// waiting it out would fail a retry that was already answered.
func TestRetryAndWaitReturnsOnARunThatRecordedNothing(t *testing.T) {
	t.Parallel()

	cfg := resolving("prod", "key-1")
	cfg.setErr(kubeconfig.ErrNotRead)
	s := New(cfg)
	startService(t, s)

	// A short ceiling, so a wait that cannot see the Skip fails rather than drags.
	require.NoError(t, s.retryAndWait(within(t), "prod", 100*time.Millisecond))
}

// **The ceiling starts when the run does, never at the ask.** Dispatch waits for one of the
// supervisor's fleet-wide start slots, so a fleet with enough queued probes to fill them delays
// the requested run by however long those take — bounded by their own timeouts, but by no number
// this call can name. A ceiling run from the ask reports a failure for a probe nobody has tried.
func TestRetryAndWaitDoesNotGiveUpOnARunStillWaitingToStart(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	dialing, gate := parkDials(t, c)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)
	t.Cleanup(func() { close(gate) })
	lease := s.Acquire("prod")
	defer lease.Release()
	// Parked, so it holds the key: the run the ask buys cannot begin behind it.
	testutil.Recv(t, dialing.Chan(), "the first dial")

	done := make(chan error, 1)
	go func() { done <- s.retryAndWait(t.Context(), "prod", 20*time.Millisecond) }()

	// A negative assertion has no event to wait for, so it needs a bounded window — sized well
	// past the ceiling, which is what would end the wait if it ran from the ask.
	testutil.NoRecv(t, done, 100*time.Millisecond, "a give-up while the requested run was still owed a slot")
}

// The whole point of waiting: the call returns on a run of its own, not on whatever the last
// one left behind.
func TestRetryAndWaitReturnsOnARunItAskedFor(t *testing.T) {
	t.Parallel()

	c := serveCluster(t)
	s := New(serving(c.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	probed(t, lease)
	before := lease.State().Connection.LastAttempt

	require.NoError(t, s.RetryAndWait(within(t), "prod"))

	assert.True(t, lease.State().Connection.LastAttempt.StartedAt.After(before.StartedAt),
		"the attempt it returned on began after the ask")
}

// --- ConnFor ---

// awaitIdentified drains the claim's states until the UID probe has answered, which is what
// stamps the connection.
func awaitIdentified(t *testing.T, watched *StateWatch) {
	t.Helper()
	awaitState(t, watched, func(st State) bool { return st.ServerUID.Known() })
}

func TestConnForHandsOutTheRequestedCluster(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	s := New(serving(cs.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitIdentified(t, watched)

	conn, err := lease.ConnFor(t.Context(), "uid-1")

	require.NoError(t, err)
	assert.Equal(t, cs.Server.URL, conn.BaseURL.String())
}

// The whole point: a caller scoped to one cluster must not be handed the connection to
// another, however the context came to point at it.
func TestConnForRefusesAnotherCluster(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	s := New(serving(cs.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitIdentified(t, watched)

	conn, err := lease.ConnFor(t.Context(), "uid-2")

	assert.Nil(t, conn)
	assert.ErrorIs(t, err, ErrIdentityMismatch)
}

// A connection that is up but has not been identified is refused, not assumed. This is the
// window the identity lives on the connection to close: a rebuilt connection sits beside the
// previous one's UID observation until the probe re-runs, and only the connection itself can
// say it has not been read yet.
func TestConnForRefusesAConnectionNoProbeHasIdentified(t *testing.T) {
	t.Parallel()

	// serveAPI answers the dial and 404s kube-system, so the connection comes up and the
	// UID probe never succeeds over it.
	srv := serveAPI(t)
	s := New(serving(srv, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitState(t, watched, func(st State) bool { return st.Phase() == PhaseProbed })

	conn, err := lease.ConnFor(t.Context(), "uid-1")

	assert.Nil(t, conn)
	assert.ErrorIs(t, err, ErrIdentityMismatch)
	require.NoError(t, func() error { _, err := lease.Conn(t.Context()); return err }(),
		"the connection itself is fine; only its identity is unknown")
}

// A cluster nothing reached reports the outage rather than the identity it could not have
// read either way — the two are different remedies, and an outage passes on its own.
func TestConnForReportsTheOutageAheadOfTheIdentity(t *testing.T) {
	t.Parallel()

	s := New(&fakeKubeconfig{})
	lease := s.Acquire("prod")
	defer lease.Release()

	conn, err := lease.ConnFor(t.Context(), "uid-1")

	assert.Nil(t, conn)
	assert.ErrorIs(t, err, ErrNoConnection)
	assert.NotErrorIs(t, err, ErrIdentityMismatch)
}

// The case the stamp alone does not cover: an API server replaced behind an endpoint and
// credentials that never moved. Nothing the kubeconfig says changed, so the UID probe
// reads a second, different uid over the connection that is already stamped. It must
// stop being handed out for the old cluster, or a subject bound to it sweeps the replacement.
//
// It is refused for the new cluster too: a connection that has already answered as something
// else cannot vouch for what answers now. That refusal is asked of the connection itself, since
// the conflict is also what the connection probe rebuilds on — once it has, uid-2 is served over
// a fresh connection, so the lease is only stably refusing uid-1.
func TestConnForRefusesAConnectionWhoseServerWasReplaced(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	s := New(serving(cs.Server, "prod", "key-1"))
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitIdentified(t, watched)
	stamped, err := lease.ConnFor(t.Context(), "uid-1")
	require.NoError(t, err)

	// Same endpoint, same credentials, a different cluster behind them.
	cs.answer(kubeSystemPath, `{"metadata":{"name":"kube-system","uid":"uid-2"}}`)
	require.NoError(t, s.RetryAndWait(within(t), "prod"))
	// The uid-2 observation is committed by the run that stamped it over this connection, so
	// the conflict below is already recorded by the time the state carries it.
	awaitState(t, watched, func(st State) bool { return st.ServerUID.Value == "uid-2" })

	_, oldErr := lease.ConnFor(t.Context(), "uid-1")

	assert.ErrorIs(t, oldErr, ErrIdentityMismatch, "the cache named for uid-1 must not read uid-2")
	assert.ErrorIs(t, stamped.IdentityFor("uid-2"), ErrIdentityMismatch,
		"and this connection cannot vouch for uid-2 either")
}

// quiesce drains the fleet feed until it stops producing, so a test asserting one signal is
// not handed a leftover from the fleet coming up. A bounded window because "nothing more" has
// no event to wait for; the feed coalesces, so what is pending is at most one per landed pass.
// settleNotifications drains the signals a startup still has in flight, **without touching the
// receiver's channel**: Chan starts a feeder goroutine that owns every later event, so a
// receiver drained that way stops answering RecvContext. quiesce below is the same wait
// for a test that reads through Chan throughout.
//
// A bounded quiet window, not a wait for an event: what it waits for is the absence of
// further signals, which has nothing to fire.
func settleNotifications(t *testing.T, news ConnectionChangeNotifications) {
	t.Helper()
	for {
		// Drain what is pending, then give the window one whole wait rather than polling
		// it away: what this waits for is the absence of further signals, which has
		// nothing to fire.
		for _, err := news.TryRecv(); err == nil; _, err = news.TryRecv() {
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-t.Context().Done():
			return
		}
		if _, err := news.TryRecv(); err != nil {
			return
		}
	}
}

func quiesce(t *testing.T, moved ConnectionChangeNotifications) {
	t.Helper()
	for {
		select {
		case <-moved.Chan():
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// The stall this signal exists to prevent: credentials rotate for a cluster that did not
// change, so the connection is rebuilt unstamped while every other part of the news —
// departed, phase, identity, the per-probe verdicts — stays exactly as it was. An
// identity-scoped holder is refused meanwhile, and the re-stamp commits nothing, since the uid
// it reads equals the one already recorded. Without the connection's own vouching in the news,
// no signal fires and nothing ever tells that holder to try again.
func TestRotationSignalsTheFleet(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cfg := serving(cs.Server, "prod", "key-1")
	s := New(cfg)
	startService(t, s)
	moved := s.ListenForConnectionChanges()
	defer moved.Close()
	lease := s.Acquire("prod")
	defer lease.Release()

	watched := lease.WatchState()
	defer watched.Close()
	awaitIdentified(t, watched)
	require.NoError(t, func() error { _, err := lease.ConnFor(t.Context(), "uid-1"); return err }())
	quiesce(t, moved)

	// Same cluster, new credentials: the connection is replaced and nothing else moves.
	cfg.rotate("prod", "key-2")
	require.NoError(t, s.RetryAndWait(within(t), "prod"))

	testutil.Recv(t, moved.Chan(), "the rotation's signal")
}

// And the connection is usable again once the re-stamp lands, which is what the signal is for:
// the holder woken by it finds an answer rather than the same refusal.
func TestRotationRestoresTheStamp(t *testing.T) {
	t.Parallel()

	cs := serveCluster(t)
	cfg := serving(cs.Server, "prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()
	awaitIdentified(t, watched)

	cfg.rotate("prod", "key-2")
	require.NoError(t, s.RetryAndWait(within(t), "prod"))

	require.Eventually(t, func() bool {
		_, err := lease.ConnFor(t.Context(), "uid-1")
		return err == nil
	}, testutil.Timeout, time.Millisecond, "the rebuilt connection is stamped and handed out again")
}

// --- identity-driven retirement ---

// kubeSystemAs is the kube-system read the serverUID probe makes, answering as one cluster.
func kubeSystemAs(uid string) string {
	return `{"metadata":{"name":"kube-system","uid":"` + uid + `"}}`
}

// The recovery the conflict exists to make possible: a server replaced behind credentials that
// never moved leaves the connection vouching for nobody, and only a rebuild clears it. The
// rebuild arm alone would wait out the connection probe's interval, so the pass that records the
// conflict wakes it.
func TestAReplacedServerRebuildsTheConnection(t *testing.T) {
	t.Parallel()

	cl := serveCluster(t)
	cfg := serving(cl.Server, "prod", "key-1")
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	awaitState(t, watched, func(st State) bool { return st.Identity().ServerUID == "uid-1" })
	stale, err := lease.Conn(within(t))
	require.NoError(t, err)

	// The endpoint now answers as another cluster, over the same credentials.
	cl.answer(kubeSystemPath, kubeSystemAs("uid-2"))
	s.supervisor.Wake("prod", nameServerUID)

	testutil.Wait(t, stale.Done(), "the conflicted connection to be retired")

	// Asked of the connection, never of State.Identity() — the observable reaches uid-2 as soon
	// as the probe reads it over the OLD connection, which says nothing about the replacement
	// having been stamped. That pairing is the trap ConnFor exists to close, and the refusal
	// through the stamp window is what a holder waits out.
	var fresh *Connection
	require.Eventually(t, func() bool {
		conn, err := lease.ConnFor(within(t), "uid-2")
		fresh = conn
		return err == nil
	}, testutil.Timeout, time.Millisecond, "the replacement to be stamped as uid-2")
	assert.NotSame(t, stale, fresh)
}

// settledReads counts kubeconfig reads until they stop. A spin has no quiet gap at all, so the
// count guard trips long before the window elapses; the window sits well inside the probe's
// backoff base, so a retry the ladder paced is never mistaken for one.
func settledReads(t *testing.T, reads *testutil.Probe[string]) {
	t.Helper()
	const window = 150 * time.Millisecond
	for seen := 0; ; seen++ {
		select {
		case <-reads.Chan():
			require.Less(t, seen, 25, "the connection probe is spinning on its own wake")
		case <-time.After(window):
			return
		}
	}
}

// The wake is gated on the news moving, and this is why. A Wake is a queue add rather than a
// schedule, so a condition re-read every pass is paced by nothing — and a conflict outlives the
// run meant to clear it whenever that run returns before the rebuild arm. A kubeconfig that stops
// resolving does exactly that, and on the level the pair would hot-loop: publish, wake, fail,
// publish, with the backoff ladder bypassed and every state watcher flooded.
func TestAConflictWhoseFileStoppedResolvingDoesNotSpin(t *testing.T) {
	t.Parallel()

	cl := serveCluster(t)
	cfg := serving(cl.Server, "prod", "key-1")
	cfg.reads = testutil.NewProbe[string](64)
	s := New(cfg)
	startService(t, s)
	lease := s.Acquire("prod")
	defer lease.Release()
	watched := lease.WatchState()
	defer watched.Close()

	awaitState(t, watched, func(st State) bool { return st.Identity().ServerUID == "uid-1" })

	// The server is replaced and the file stops resolving, so every run the conflict wakes
	// returns before the rebuild arm and leaves the conflict standing.
	cl.answer(kubeSystemPath, kubeSystemAs("uid-2"))
	cfg.setErr(errors.New("open ca.crt: no such file"))
	cfg.reads.Drain()
	s.supervisor.Wake("prod", nameServerUID)

	settledReads(t, cfg.reads)
}

// The rotation signal cannot depend on the re-stamp being slower than the pass that
// publishes. The stamp is a mutable word on the connection, read at publish time, so a
// re-stamp of the same uid landing first leaves every uid-derived field exactly as it
// was — and the fleet would hear nothing about a connection that was replaced under it,
// stranding whoever was refused inside the window.
//
// Driven through record, which is where a pass's news is compared, so the ordering is
// the test's rather than the scheduler's.
func TestARebuiltConnectionIsNewsEvenWhenTheStampLandsFirst(t *testing.T) {
	t.Parallel()

	s := New(serving(serveCluster(t).Server, "prod", "key-1"))
	lease := s.Acquire("prod")
	defer lease.Release()

	first := &Connection{seq: connSeq.Add(1)}
	first.setServerUID("uid-1")
	_, _, changed := s.record("prod", first, s.newsFor(first))
	require.True(t, changed, "the first connection is news")

	// The replacement, already stamped as the same cluster by the time this pass
	// publishes: every uid-derived field matches what was published for its predecessor.
	second := &Connection{seq: connSeq.Add(1)}
	second.setServerUID("uid-1")

	_, _, changed = s.record("prod", second, s.newsFor(second))

	assert.True(t, changed, "a replaced connection went unannounced")
}

// newsFor is the news a pass carrying conn would publish, with everything the probes
// report held equal — the rotation case, where only the connection moved.
func (s *Service) newsFor(conn *Connection) news {
	n := news{phase: PhaseProbed}
	if conn != nil {
		n.conn = conn.Seq()
		n.vouchedFor, _ = conn.ServerUID()
	}
	return n
}

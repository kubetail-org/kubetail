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

package kubelet

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustertest"
)

// endless writes a line each time lines sends, until the request ends.
func endless(lines <-chan string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case line := <-lines:
				_, _ = w.Write([]byte(line + "\n"))
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

// A claim reaches the API server the mirror's pods came from and no other.
func TestAClaimIsRefusedAnotherServer(t *testing.T) {
	t.Parallel()
	svc := serving(t, endless(nil))
	c, err := Acquire(t.Context(), svc, "7", "uid-2")
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)

	assert.ErrorIs(t, err, cluster.ErrIdentityMismatch)
}

// A cluster Kubetail cannot connect to is refused before anything is read, and any
// other failure to claim it is the failure.
func TestAClaimOnACLusterKubetailDoesNotConnectTo(t *testing.T) {
	t.Parallel()
	_, err := Acquire(t.Context(), &fakeService{acquireErr: cluster.ErrNotConnectable}, "7", "uid-1")
	assert.ErrorIs(t, err, ErrRevoked)

	_, err = Acquire(t.Context(), &fakeService{acquireErr: errors.New("pool closed")}, "7", "uid-1")
	assert.EqualError(t, err, "pool closed")
}

// A request in flight when the claim is revoked answers the revocation, not the
// cut connection, and a revoked claim refuses the next.
func TestARevokedClaimCutsAndRefusesItsRequests(t *testing.T) {
	t.Parallel()
	arrived := make(chan struct{})
	svc := serving(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
	}))
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)
	defer c.Close()
	revoked := make(chan struct{})
	c.Revoked = revoked
	got := make(chan error, 1)
	go func() {
		_, err := c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)
		got <- err
	}()
	testutil.Wait(t, arrived, "the request to reach the server")

	close(revoked)

	assert.ErrorIs(t, testutil.Recv(t, got, "the request to end"), ErrRevoked)
	_, err = c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)
	assert.ErrorIs(t, err, ErrRevoked)
}

// A claim whose record's watch cannot open is refused, and releases its lease.
func TestAClaimWhoseWatchCannotOpenIsRefused(t *testing.T) {
	t.Parallel()
	svc := serving(t, endless(nil))
	svc.watchErr = errors.New("no watch")
	_, err := Open(t.Context(), svc, "7", "uid-1", time.Millisecond)
	assert.EqualError(t, err, "no watch")
	assert.Equal(t, int32(1), svc.lease.released.Load())
}

// A record's watch that ends before the claim does revokes it: a claim nothing
// watches could outlive the cluster.
func TestAWatchThatEndsRevokesTheClaim(t *testing.T) {
	t.Parallel()
	svc := serving(t, endless(nil))
	svc.frames = make(chan cluster.ClusterCollectionWatchFrame)
	c, err := Open(t.Context(), svc, "7", "uid-1", time.Millisecond)
	require.NoError(t, err)
	defer c.Close()

	close(svc.frames)

	testutil.WaitClosed(t, c.Revoked, "the claim to be revoked")
}

// deliver sends frame on frames, failing the test if nothing takes it.
func deliver(t *testing.T, frames chan<- cluster.ClusterCollectionWatchFrame, frame cluster.ClusterCollectionWatchFrame, what string) {
	t.Helper()
	select {
	case frames <- frame:
	case <-time.After(testutil.Timeout):
		t.Fatalf("timed out sending %s", what)
	}
}

// synced sends the record's snapshot on frames: the cluster, then the bookmark.
func synced(t *testing.T, frames chan<- cluster.ClusterCollectionWatchFrame) {
	t.Helper()
	deliver(t, frames, cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: enabledCluster()}, "the record")
	deliver(t, frames, cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}, "the bookmark")
}

// A record's watch that ends after its snapshot is opened again, and the claim
// stands while the new snapshot holds the record Kubetail connects to.
func TestAWatchThatEndsIsOpenedAgain(t *testing.T) {
	t.Parallel()
	svc := serving(t, endless(nil))
	first, second := make(chan cluster.ClusterCollectionWatchFrame), make(chan cluster.ClusterCollectionWatchFrame)
	svc.frames = first
	c, err := Open(t.Context(), svc, "7", "uid-1", time.Millisecond)
	require.NoError(t, err)
	defer c.Close()
	synced(t, first)

	svc.set(func() { svc.frames = second })
	close(first)
	synced(t, second)

	assert.False(t, c.IsRevoked())
	disabled := enabledCluster()
	disabled.Spec.Enabled = false
	deliver(t, second, cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameModified, Cluster: disabled}, "the record disabled")
	testutil.WaitClosed(t, c.Revoked, "the claim to be revoked")
}

// A watch opened again whose snapshot lacks the record, or that cannot be opened,
// revokes the claim.
func TestAWatchOpenedAgainOnAGoneRecordRevokes(t *testing.T) {
	t.Parallel()
	for name, gone := range map[string]func(svc *fakeService){
		"no record": func(svc *fakeService) {
			again := make(chan cluster.ClusterCollectionWatchFrame, 1)
			again <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}
			svc.frames = again
		},
		"no watch": func(svc *fakeService) { svc.watchErr = errors.New("no watch") },
	} {
		t.Run(name, func(t *testing.T) {
			svc := serving(t, endless(nil))
			first := make(chan cluster.ClusterCollectionWatchFrame)
			svc.frames = first
			c, err := Open(t.Context(), svc, "7", "uid-1", time.Millisecond)
			require.NoError(t, err)
			defer c.Close()
			synced(t, first)

			svc.set(func() { gone(svc) })
			close(first)

			testutil.WaitClosed(t, c.Revoked, "the claim to be revoked")
		})
	}
}

// A cluster disabled under a follow ends the body being read, and refuses the
// next request.
func TestADisabledClusterEndsTheClaim(t *testing.T) {
	t.Parallel()
	lines := make(chan string)
	svc := serving(t, endless(lines))
	svc.frames = make(chan cluster.ClusterCollectionWatchFrame)
	c, err := Open(t.Context(), svc, "7", "uid-1", time.Millisecond)
	require.NoError(t, err)
	defer c.Close()
	body, err := c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", url.Values{"follow": {"true"}})
	require.NoError(t, err)
	defer body.Close()
	svc.frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: enabledCluster()}

	disabled := enabledCluster()
	disabled.Spec.Enabled = false
	svc.frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameModified, Cluster: disabled}

	_, err = bufio.NewReader(body).ReadString('\n')
	assert.Error(t, err)
	testutil.WaitClosed(t, c.Revoked, "the claim to be revoked")
	_, err = c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)
	assert.ErrorIs(t, err, ErrRevoked)
}

// A body being read when its connection is retired ends with ErrRetired, not the
// cancel that cut it.
func TestARetiredConnectionEndsTheBody(t *testing.T) {
	t.Parallel()
	lines := make(chan string)
	svc := serving(t, endless(lines))
	conn := clustertest.Connection(t, svc.lease.conn.BaseURL.String())
	svc.lease.conn = conn
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)
	defer c.Close()
	body, err := c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", url.Values{"follow": {"true"}})
	require.NoError(t, err)
	defer body.Close()

	conn.Retire()

	_, err = bufio.NewReader(body).ReadString('\n')
	assert.ErrorIs(t, err, ErrRetired)
}

// The watch's retry waits until its context ends, and a claim whose context ends
// there is not revoked.
func TestAWatchWaitingToReopenEndsWithItsContext(t *testing.T) {
	t.Parallel()
	frames := make(chan cluster.ClusterCollectionWatchFrame, 2)
	frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: enabledCluster()}
	frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}
	close(frames)
	stream := &cluster.Stream[cluster.ClusterCollectionWatchFrame]{Frames: frames}

	// Done is closed while Err is still nil, as a cancel landing after the watch's end
	// was checked leaves it: the wait for the retry is what sees it.
	assert.False(t, watchRevoked(endingCtx{t.Context()}, nil, "7", stream, time.Hour))
}

// endingCtx is a context whose Done is closed but whose Err is nil.
type endingCtx struct{ context.Context }

func (endingCtx) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (endingCtx) Err() error { return nil }

// The body goes on reading while the server goes on writing, and ends when the
// context does.
func TestTheBodyStreamsUntilTheContextEnds(t *testing.T) {
	t.Parallel()
	lines := make(chan string)
	svc := serving(t, endless(lines))
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)
	defer c.Close()
	ctx, cancel := context.WithCancel(t.Context())
	body, err := c.Get(ctx, "/api/v1/namespaces/prod/pods/web/log", url.Values{"follow": {"true"}})
	require.NoError(t, err)
	defer body.Close()
	r := bufio.NewReader(body)

	for _, want := range []string{"one\n", "two\n"} {
		lines <- want[:len(want)-1]
		got, err := r.ReadString('\n')
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	cancel()

	_, err = r.ReadString('\n')
	assert.Error(t, err)
}

// A status the API server refuses with is its code and its message, cleaned.
func TestARefusalIsItsStatus(t *testing.T) {
	t.Parallel()
	svc := serving(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"kind":"Status","message":"container \"app\" in pod \"web\" is waiting to start: \u001b[31mContainerCreating"}`))
	}))
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)

	var se *StatusError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, http.StatusBadRequest, se.Code)
	assert.Equal(t, `container "app" in pod "web" is waiting to start: ContainerCreating`, se.Message)
}

// A refusal whose body is no Status is named by its code alone.
func TestARefusalWithNoStatusIsItsCode(t *testing.T) {
	t.Parallel()
	svc := serving(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream gone", http.StatusBadGateway)
	}))
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Get(t.Context(), "/api/v1/namespaces/prod/pods/web/log", nil)

	assert.EqualError(t, err, "the API server answered 502")
}

// Close releases the lease.
func TestCloseReleasesTheLease(t *testing.T) {
	t.Parallel()
	svc := serving(t, endless(nil))
	c, err := Acquire(t.Context(), svc, "7", "uid-1")
	require.NoError(t, err)

	c.Close()

	assert.Equal(t, int32(1), svc.lease.released.Load())
}

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

package bash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/kubeproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
)

// fakeLease is a claim on a cluster whose connection is conn, reached by
// serverUID alone; any other UID answers a mismatch. Every other method
// panics on the nil embedded lease.
type fakeLease struct {
	cluster.Lease
	serverUID string
	conn      *cluster.Connection
	err       error // what ConnFor answers, when set
	released  atomic.Int32
}

func (l *fakeLease) ConnFor(_ context.Context, serverUID string) (*cluster.Connection, error) {
	switch {
	case l.err != nil:
		return nil, l.err
	case serverUID != l.serverUID:
		return nil, fmt.Errorf("%w: another cluster", cluster.ErrIdentityMismatch)
	}
	return l.conn, nil
}

func (l *fakeLease) Release() { l.released.Add(1) }

// A claim is the lease the cluster service hands out for the target's UID,
// and Close releases it.
func TestAClaimIsTheLeaseForTheTargetsUID(t *testing.T) {
	t.Parallel()

	lease := &fakeLease{serverUID: "uid-1"}
	tl := &Tool{clusterSvc: fakeService{lease: lease}}

	up, err := tl.claim(t.Context(), "7", "uid-1")

	require.NoError(t, err)
	u := up.(upstream)
	assert.Same(t, lease, u.lease)
	assert.Equal(t, "uid-1", u.serverUID)
	up.Close()
	assert.Equal(t, int32(1), lease.released.Load())
}

// A claim whose record cannot be watched is not made, and its lease is
// released.
func TestAClaimWhoseRecordCannotBeWatched(t *testing.T) {
	t.Parallel()

	lease := &fakeLease{serverUID: "uid-1"}
	tl := &Tool{clusterSvc: fakeService{lease: lease, watchErr: errDisk}}

	_, err := tl.claim(t.Context(), "7", "uid-1")

	assert.ErrorIs(t, err, errDisk)
	assert.Equal(t, int32(1), lease.released.Load())
}

// A claim is revoked by a record Kubetail no longer connects to, and by a watch
// that fails, since then nothing would tell it.
func TestAClaimIsRevoked(t *testing.T) {
	t.Parallel()

	enabled := kubeCluster("ctx", "uid-1")
	enabled.Spec.Enabled = true
	disabled := kubeCluster("ctx", "uid-1")
	marked := kubeCluster("ctx", "uid-1")
	marked.Spec.Enabled = true
	marked.DeletionRequestedAt = &time.Time{}
	for name, frame := range map[string]*cluster.ClusterCollectionWatchFrame{
		"disabled":         {Type: cluster.CollectionWatchFrameModified, Cluster: disabled},
		"being deleted":    {Type: cluster.CollectionWatchFrameModified, Cluster: marked},
		"deleted":          {Type: cluster.CollectionWatchFrameDeleted, Cluster: enabled},
		"the watch failed": nil,
	} {
		t.Run(name, func(t *testing.T) {
			frames := make(chan cluster.ClusterCollectionWatchFrame)
			tl := &Tool{clusterSvc: fakeService{lease: &fakeLease{serverUID: "uid-1"}, frames: frames}}
			up, err := tl.claim(t.Context(), "7", "uid-1")
			require.NoError(t, err)
			defer up.Close()

			frames <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: enabled}
			if frame == nil {
				close(frames)
			} else {
				frames <- *frame
			}

			testutil.WaitClosed(t, up.(upstream).revoked, "the claim to be revoked")
			_, err = up.Endpoint(t.Context())
			assert.ErrorIs(t, err, kubeproxy.ErrNotConnectable)
		})
	}
}

// A record Kubetail connects to leaves the claim alone.
func TestAConnectableRecordKeepsTheClaim(t *testing.T) {
	t.Parallel()

	enabled := kubeCluster("ctx", "uid-1")
	enabled.Spec.Enabled = true
	ctx, cancel := context.WithCancel(t.Context())
	stream := cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCollectionWatchFrame) error {
		out <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameAdded, Cluster: enabled}
		out <- cluster.ClusterCollectionWatchFrame{Type: cluster.CollectionWatchFrameBookmark}
		<-ctx.Done()
		return nil
	})
	revoked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchRevoked(ctx, stream, revoked)
	}()

	cancel()
	testutil.WaitClosed(t, done, "the watch to end")

	select {
	case <-revoked:
		t.Fatal("revoked by a connectable record")
	default:
	}
}

// A claim that cannot be made for a reason of the cluster's own still runs the
// command, its every request answering why: a cluster never identified, and
// one Kubetail does not connect to. A cluster gone since the target read it is
// gone, and any other failure is the run's.
func TestAClaimThatCannotBeMade(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		uid        string
		acquireErr error
		answers    error // what every request answers, for a claim made
		fails      error // why the run fails, for one that is not
	}{
		"no uid":          {answers: kubeproxy.ErrNotIdentified},
		"not connectable": {uid: "u", acquireErr: fmt.Errorf("%w: disabled", cluster.ErrNotConnectable), answers: kubeproxy.ErrNotConnectable},
		"gone":            {uid: "u", acquireErr: fmt.Errorf("%w: x", cluster.ErrNotFound), fails: errClusterGone},
		"another":         {uid: "u", acquireErr: errDisk, fails: errDisk},
	} {
		t.Run(name, func(t *testing.T) {
			tl := &Tool{clusterSvc: fakeService{acquireErr: c.acquireErr}}

			up, err := tl.claim(t.Context(), "7", c.uid)

			if c.fails != nil {
				assert.ErrorIs(t, err, c.fails)
				return
			}
			require.NoError(t, err)
			_, err = up.Endpoint(t.Context())
			assert.ErrorIs(t, err, c.answers)
			up.Close()
		})
	}
}

// errDisk is a failure of the cluster service's own.
var errDisk = errors.New("disk")

// A claim vouches for the UID the target read: the connection's base URL and
// client for it, a mismatch for a lease re-pointed at another server, and any
// other error as it is.
func TestUpstreamIsTheTargetsCluster(t *testing.T) {
	t.Parallel()

	base, _ := url.Parse("https://api.example")
	conn := &cluster.Connection{BaseURL: base, HTTPClient: &http.Client{}}
	lease := &fakeLease{serverUID: "uid-1", conn: conn}

	got, err := upstream{lease: lease, serverUID: "uid-1"}.Endpoint(t.Context())
	require.NoError(t, err)
	assert.Same(t, base, got.Base)
	assert.Same(t, conn.HTTPClient, got.Client)

	_, err = upstream{lease: lease, serverUID: "uid-2"}.Endpoint(t.Context())
	assert.ErrorIs(t, err, kubeproxy.ErrIdentityMismatch)

	lease.err = context.Canceled
	_, err = upstream{lease: lease, serverUID: "uid-1"}.Endpoint(t.Context())
	assert.ErrorIs(t, err, context.Canceled, "any other error as it is")
}

// A connection handed out is done once the claim is revoked, so a watch open
// through it ends.
func TestARevokedClaimEndsWhatIsOpen(t *testing.T) {
	t.Parallel()

	conn := &cluster.Connection{HTTPClient: &http.Client{}}
	revoked := make(chan struct{})
	u := upstream{lease: &fakeLease{serverUID: "uid-1", conn: conn}, serverUID: "uid-1", revoked: revoked}

	got, err := u.Endpoint(t.Context())
	require.NoError(t, err)
	close(revoked)

	testutil.WaitClosed(t, got.Done, "the connection to be done")
}

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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstest"
)

// newLogServer is a fake log server whose refusals are the status errors a claim
// answers.
func newLogServer() *logstest.Server {
	s := logstest.New()
	s.Refused = func(code int, message string) error { return &StatusError{Code: code, Message: message} }
	return s
}

// at is a moment on 2026-10-08 at 10:00, offset by d.
func at(d time.Duration) time.Time {
	return time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC).Add(d)
}

func after(d time.Duration) logline.Bound {
	return logline.Bound{At: logline.MomentAt(at(d)), Inclusive: false}
}

// from, after and to are bounds at the moment d past 10:00: since it, after it,
// and until it.
func from(d time.Duration) logline.Bound {
	return logline.Bound{At: logline.MomentAt(at(d)), Inclusive: true}
}

func to(d time.Duration) logline.Bound {
	return logline.Bound{At: logline.MomentAt(at(d)), Inclusive: true}
}

// containerID is the runtime id of the instance-th instance of the container
// name in the pod uid, as containerStatus writes it.
func containerID(uid, name string, instance int) string {
	return fmt.Sprintf("containerd://%s-%s-%d", uid, name, instance)
}

// lineAt is the line cursor of key's line at d past 10:00, from the first instance
// of a fixture pod, the ordinal-th of its moment.
func lineAt(key logline.StreamKey, d time.Duration, ordinal int) logline.Cursor {
	return logline.Cursor{ContainerName: logline.ContainerName{Namespace: key.Namespace, Pod: key.Pod, Container: key.Container}, RunPlace: logline.RunPlace{ContainerID: containerID("uid-"+key.Pod, key.Container, 0), Timestamp: at(d), Ordinal: ordinal}}
}

// drain is every record r sends until it closes.
func drain(r *Read) []logline.Record {
	var out []logline.Record
	for rec := range r.Lines.Chan() {
		if !rec.WindowEnd && !rec.WindowCut {
			out = append(out, rec.Record)
		}
	}
	return out
}

// messages is each record's message.
func messages(recs []logline.Record) []string {
	out := []string{}
	for _, r := range recs {
		out = append(out, r.Message)
	}
	return out
}

// newestFirst is recs, one stream's, newest first: the order of a backward read's
// lines across its windows, which each arrive oldest first.
func newestFirst(recs []logline.Record) []logline.Record {
	out := slices.Clone(recs)
	slices.SortStableFunc(out, func(a, b logline.Record) int { return logline.CompareLines(b, a) })
	return out
}

// claimOn is a claim on a cluster whose API server serves s.
func claimOn(t *testing.T, s *logstest.Server) *Claim {
	t.Helper()
	c, err := Acquire(t.Context(), serving(t, s), "7", "uid-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// The limits scaled down so a log of a few hundred lines reaches them: smallCaps
// 1,024 times, and midCaps 64 times, under which a window's GET reads little past
// its end next to what the window holds.
var (
	smallCaps = Quota{Second: 1 << 10, Stream: 16 << 10, Gets: 16, Query: 64 << 10, Window: 4 << 10, MinWindow: 64, Part: 16}
	midCaps   = Quota{Second: 16 << 10, Stream: 256 << 10, Gets: 16, Query: 1 << 20, Window: 64 << 10, MinWindow: 1 << 10, Part: 256}
)

// limitsOf is the limits of a query's one stream under c.
func limitsOf(c Quota) *Limits { return QueryLimits(c, 1)[0] }

// newLogServer is a fake log server whose refusals are the status errors a claim

// fakeService is the cluster service as a claim reads it: the lease
// AcquireConnection hands out and the cluster watch's frames. Every method it
// does not set panics on the nil embedded service. A test that changes a field
// while a claim runs does so through set.
type fakeService struct {
	cluster.Service
	mu         sync.Mutex
	lease      *fakeLease
	frames     chan cluster.ClusterCollectionWatchFrame
	watchErr   error
	acquireErr error
}

func (f *fakeService) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeService) Clusters() cluster.Clusters { return fakeClusters{f: f} }

func (f *fakeService) AcquireConnection(context.Context, cluster.ClusterID) (cluster.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.lease, nil
}

type fakeClusters struct {
	cluster.Clusters
	f *fakeService
}

func (fc fakeClusters) WatchCollectionByID(ctx context.Context, _ cluster.ClusterID) (*cluster.Stream[cluster.ClusterCollectionWatchFrame], error) {
	fc.f.mu.Lock()
	frames, err := fc.f.frames, fc.f.watchErr
	fc.f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return cluster.NewStream(ctx, func(ctx context.Context, out chan<- cluster.ClusterCollectionWatchFrame) error {
		if frames == nil {
			<-ctx.Done()
			return nil
		}
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					return fmt.Errorf("watch failed")
				}
				out <- f
			case <-ctx.Done():
				return nil
			}
		}
	}), nil
}

// fakeLease is a claim on a cluster whose connection is conn, reached by serverUID
// alone; any other UID answers a mismatch.
type fakeLease struct {
	cluster.Lease
	serverUID string
	conn      *cluster.Connection
	released  atomic.Int32
}

func (l *fakeLease) ConnFor(_ context.Context, serverUID string) (*cluster.Connection, error) {
	if serverUID != l.serverUID {
		return nil, fmt.Errorf("%w: another cluster", cluster.ErrIdentityMismatch)
	}
	return l.conn, nil
}

func (l *fakeLease) Release() { l.released.Add(1) }

// serving is a service whose cluster uid-1 is reached at h.
func serving(t *testing.T, h http.Handler) *fakeService {
	t.Helper()
	// HTTP/2, as an API server speaks it: a read that closes its body early drops
	// one stream rather than its connection, so a test paging many logs dials a
	// handful of connections, not one per GET.
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeService{lease: &fakeLease{serverUID: "uid-1", conn: &cluster.Connection{BaseURL: base, HTTPClient: srv.Client()}}}
}

func enabledCluster() *cluster.Cluster {
	return &cluster.Cluster{Spec: cluster.ClusterSpec{
		Enabled: true,
		Source:  cluster.ClusterSpecSource{Kubeconfig: &cluster.ClusterSpecSourceKubeconfig{Context: "dev"}},
	}}
}

// matching keeps the lines whose message matches pattern, as a page's grep does.
func matching(pattern string) func(logline.Record) bool {
	re := regexp.MustCompile(pattern)
	return func(rec logline.Record) bool { return logline.Matches(re, rec.Message) }
}

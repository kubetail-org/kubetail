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

package logs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
)

var fetcherTestSource = LogSource{
	Metadata:      LogSourceMetadata{Node: "node-1"},
	Namespace:     "ns",
	PodName:       "pod-1",
	ContainerName: "app",
	ContainerID:   "containerd://abc",
}

var fetcherBaseTS = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

// fetcherTestRecords returns n records one second apart, messages "line <i>".
func fetcherTestRecords(n int) []LogRecord {
	records := make([]LogRecord, n)
	for i := range n {
		records[i] = LogRecord{
			Timestamp: fetcherBaseTS.Add(time.Duration(i) * time.Second),
			Message:   fmt.Sprintf("line %d", i),
		}
	}
	return records
}

// fakePodLog serves a pod's log through a fake clientset and records the
// PodLogOptions of each request. Like the Kubernetes API, it truncates
// SinceTime to the second.
type fakePodLog struct {
	mu      sync.Mutex
	records []LogRecord
	reqs    []corev1.PodLogOptions

	// err, if set, is returned for every request that matches errIf.
	err   error
	errIf func(opts *corev1.PodLogOptions) bool

	// grow is appended to the log after the first TailLines request is served.
	grow []LogRecord
}

func (f *fakePodLog) clientset() *fake.Clientset {
	cs := fake.NewClientset()
	cs.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(k8stesting.GenericAction)
		if !ok || action.GetSubresource() != "log" {
			return false, nil, nil
		}
		opts := ga.GetValue().(*corev1.PodLogOptions)
		body, err := f.serve(opts)
		if err != nil {
			return true, nil, err
		}
		return true, &runtime.Unknown{Raw: body}, nil
	})
	return cs
}

func (f *fakePodLog) serve(opts *corev1.PodLogOptions) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// The fetcher reuses its TailLines pointer across batches.
	req := *opts
	if opts.TailLines != nil {
		req.TailLines = new(*opts.TailLines)
	}
	f.reqs = append(f.reqs, req)

	if f.err != nil && (f.errIf == nil || f.errIf(opts)) {
		return nil, f.err
	}

	records := f.records
	if opts.SinceTime != nil {
		since := opts.SinceTime.Truncate(time.Second)
		records = nil
		for _, r := range f.records {
			if !r.Timestamp.Before(since) {
				records = append(records, r)
			}
		}
	}
	if opts.TailLines != nil {
		records = records[max(0, len(records)-int(*opts.TailLines)):]
	}

	var b strings.Builder
	for _, r := range records {
		fmt.Fprintf(&b, "%s %s\n", r.Timestamp.Format(time.RFC3339Nano), r.Message)
	}
	body := []byte(b.String())
	if opts.LimitBytes != nil && int64(len(body)) > *opts.LimitBytes {
		body = body[:*opts.LimitBytes]
	}

	if opts.TailLines != nil && f.grow != nil {
		f.records = append(f.records, f.grow...)
		f.grow = nil
	}

	return body, nil
}

func (f *fakePodLog) requests() []corev1.PodLogOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

// drainRecords reads ch until it closes, failing the test after a deadline.
func drainRecords(t *testing.T, ch <-chan LogRecord) []LogRecord {
	t.Helper()
	var out []LogRecord
	deadline := time.After(10 * time.Second)
	for {
		select {
		case r, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, r)
		case <-deadline:
			t.Fatal("channel did not close")
			return nil
		}
	}
}

func recordMessages(records []LogRecord) []string {
	msgs := make([]string, len(records))
	for i, r := range records {
		msgs[i] = r.Message
	}
	return msgs
}

// reversedMessages returns the messages of fetcherTestRecords(n), last first.
func reversedMessages(n int) []string {
	msgs := recordMessages(fetcherTestRecords(n))
	slices.Reverse(msgs)
	return msgs
}

func lineMessages(idx ...int) []string {
	msgs := make([]string, len(idx))
	for i, n := range idx {
		msgs[i] = fmt.Sprintf("line %d", n)
	}
	return msgs
}

func TestKubeLogFetcher_StreamForward(t *testing.T) {
	at := func(sec float64) time.Time {
		return fetcherBaseTS.Add(time.Duration(sec * float64(time.Second)))
	}

	tests := []struct {
		name     string
		opts     FetcherOptions
		wantMsgs []string
		wantReq  corev1.PodLogOptions
	}{
		{
			name:     "all records",
			opts:     FetcherOptions{},
			wantMsgs: lineMessages(0, 1, 2, 3, 4),
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app"},
		},
		{
			name:     "start time drops records the second-granular API returns early",
			opts:     FetcherOptions{StartTime: at(2.5)},
			wantMsgs: lineMessages(3, 4),
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app", SinceTime: &metav1.Time{Time: at(2.5)}},
		},
		{
			name:     "stop time is inclusive",
			opts:     FetcherOptions{StopTime: at(2)},
			wantMsgs: lineMessages(0, 1, 2),
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app"},
		},
		{
			name:     "grep",
			opts:     FetcherOptions{GrepRegex: regexp.MustCompile(`line [13]`)},
			wantMsgs: lineMessages(1, 3),
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app"},
		},
		{
			name:     "follow from default starts at start time",
			opts:     FetcherOptions{FollowFrom: FollowFromDefault, StartTime: at(3)},
			wantMsgs: lineMessages(3, 4),
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app", Follow: true, SinceTime: &metav1.Time{Time: at(3)}},
		},
		{
			name:     "follow from end asks for no past lines",
			opts:     FetcherOptions{FollowFrom: FollowFromEnd, StartTime: at(3)},
			wantMsgs: []string{},
			wantReq:  corev1.PodLogOptions{Timestamps: true, Container: "app", Follow: true, TailLines: new(int64(0))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fl := &fakePodLog{records: fetcherTestRecords(5)}
			f := NewKubeLogFetcher(fl.clientset())

			ch, err := f.StreamForward(t.Context(), fetcherTestSource, tt.opts)
			require.NoError(t, err)

			records := drainRecords(t, ch)
			assert.Equal(t, tt.wantMsgs, recordMessages(records))
			for _, r := range records {
				assert.NoError(t, r.err)
				assert.Equal(t, fetcherTestSource, r.Source)
			}

			reqs := fl.requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, tt.wantReq, reqs[0])
		})
	}
}

func TestKubeLogFetcher_StreamForward_PodNotFoundReturnsClosedChannel(t *testing.T) {
	fl := &fakePodLog{err: apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "pod-1")}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamForward(t.Context(), fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)
	assert.Empty(t, drainRecords(t, ch))
}

func TestKubeLogFetcher_StreamForward_ReturnsRequestError(t *testing.T) {
	fl := &fakePodLog{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pod-1", errors.New("denied"))}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamForward(t.Context(), fetcherTestSource, FetcherOptions{})
	assert.ErrorContains(t, err, "forbidden")
	assert.Nil(t, ch)
}

func TestKubeLogFetcher_StreamForward_StopsWhenContextIsCancelled(t *testing.T) {
	fl := &fakePodLog{records: fetcherTestRecords(5)}
	f := NewKubeLogFetcher(fl.clientset())

	ctx, cancel := context.WithCancel(t.Context())
	ch, err := f.StreamForward(ctx, fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)

	first := <-ch
	assert.Equal(t, "line 0", first.Message)
	cancel()

	// Records already racing the cancel may still arrive before the channel closes.
	drainRecords(t, ch)
}

func TestKubeLogFetcher_StreamBackward_ReadsBatchesInReverse(t *testing.T) {
	fl := &fakePodLog{records: fetcherTestRecords(25)}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{BatchSizeHint: 1})
	require.NoError(t, err)

	records := drainRecords(t, ch)
	assert.Equal(t, reversedMessages(25), recordMessages(records))
	for _, r := range records {
		assert.NoError(t, r.err)
		assert.Equal(t, fetcherTestSource, r.Source)
	}

	// One request for the first timestamp, then batches of at least ten
	// lines, each reaching ten lines further back.
	reqs := fl.requests()
	require.Len(t, reqs, 4)
	assert.Equal(t, new(int64(100)), reqs[0].LimitBytes)
	assert.Nil(t, reqs[0].TailLines)
	for i, want := range []int64{10, 20, 30} {
		require.NotNil(t, reqs[i+1].TailLines)
		assert.Equal(t, want, *reqs[i+1].TailLines)
		assert.True(t, reqs[i+1].Timestamps)
		assert.Equal(t, "app", reqs[i+1].Container)
	}
}

func TestKubeLogFetcher_StreamBackward_TimeBoundsAndGrep(t *testing.T) {
	fl := &fakePodLog{records: fetcherTestRecords(25)}
	f := NewKubeLogFetcher(fl.clientset())

	start := fetcherBaseTS.Add(3500 * time.Millisecond)
	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{
		StartTime: start,
		StopTime:  fetcherBaseTS.Add(20 * time.Second),
		GrepRegex: regexp.MustCompile(`line 1`),
	})
	require.NoError(t, err)

	records := drainRecords(t, ch)
	assert.Equal(t, lineMessages(19, 18, 17, 16, 15, 14, 13, 12, 11, 10), recordMessages(records))

	reqs := fl.requests()
	require.NotEmpty(t, reqs)
	require.NotNil(t, reqs[0].SinceTime)
	assert.True(t, start.Equal(reqs[0].SinceTime.Time))
}

func TestKubeLogFetcher_StreamBackward_WidensBatchWhenLogGrows(t *testing.T) {
	// Twenty lines are written after the first batch is read, so the second
	// batch starts after the first: the fetcher widens it and skips the new lines.
	all := fetcherTestRecords(45)
	fl := &fakePodLog{records: all[:25], grow: all[25:]}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)

	records := drainRecords(t, ch)
	assert.Equal(t, reversedMessages(25), recordMessages(records))

	var tails []int64
	for _, r := range fl.requests()[1:] {
		tails = append(tails, *r.TailLines)
	}
	assert.Equal(t, []int64{10, 20, 40, 60}, tails)
}

func TestKubeLogFetcher_StreamBackward_EmptyLogReturnsClosedChannel(t *testing.T) {
	fl := &fakePodLog{}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)
	assert.Empty(t, drainRecords(t, ch))
	assert.Len(t, fl.requests(), 1)
}

func TestKubeLogFetcher_StreamBackward_ReturnsFirstTimestampError(t *testing.T) {
	fl := &fakePodLog{err: errors.New("apiserver down")}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{})
	assert.ErrorContains(t, err, "apiserver down")
	assert.Nil(t, ch)
}

func TestKubeLogFetcher_StreamBackward_BatchErrorEndsStream(t *testing.T) {
	fl := &fakePodLog{
		records: fetcherTestRecords(5),
		err:     errors.New("apiserver down"),
		errIf:   func(opts *corev1.PodLogOptions) bool { return opts.TailLines != nil },
	}
	f := NewKubeLogFetcher(fl.clientset())

	ch, err := f.StreamBackward(t.Context(), fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)
	assert.Empty(t, drainRecords(t, ch))
}

func TestKubeLogFetcher_StreamBackward_StopsWhenContextIsCancelled(t *testing.T) {
	fl := &fakePodLog{records: fetcherTestRecords(25)}
	f := NewKubeLogFetcher(fl.clientset())

	ctx, cancel := context.WithCancel(t.Context())
	ch, err := f.StreamBackward(ctx, fetcherTestSource, FetcherOptions{})
	require.NoError(t, err)

	first := <-ch
	assert.Equal(t, "line 24", first.Message)
	cancel()

	assert.Less(t, len(drainRecords(t, ch)), 24)
}

// fakeAgent is a LogRecordsService that records each request and replies
// with its records, then err.
type fakeAgent struct {
	clusteragentpb.UnimplementedLogRecordsServiceServer

	records []*clusteragentpb.LogRecord
	err     error

	mu   sync.Mutex
	reqs []*clusteragentpb.LogRecordsStreamRequest
}

func (a *fakeAgent) StreamForward(req *clusteragentpb.LogRecordsStreamRequest, stream grpc.ServerStreamingServer[clusteragentpb.LogRecord]) error {
	return a.serve(req, stream)
}

func (a *fakeAgent) StreamBackward(req *clusteragentpb.LogRecordsStreamRequest, stream grpc.ServerStreamingServer[clusteragentpb.LogRecord]) error {
	return a.serve(req, stream)
}

func (a *fakeAgent) serve(req *clusteragentpb.LogRecordsStreamRequest, stream grpc.ServerStreamingServer[clusteragentpb.LogRecord]) error {
	a.mu.Lock()
	a.reqs = append(a.reqs, req)
	a.mu.Unlock()

	for _, r := range a.records {
		if err := stream.Send(r); err != nil {
			return err
		}
	}
	return a.err
}

func (a *fakeAgent) requests() []*clusteragentpb.LogRecordsStreamRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.reqs)
}

// fakeUnicaster dials one agent for every node and records the node asked for.
type fakeUnicaster struct {
	conn *grpc.ClientConn

	mu    sync.Mutex
	nodes []string
}

func (u *fakeUnicaster) UnicastSubscribeOnce(ctx context.Context, nodeName string, fn grpcdispatcher.DispatchHandler) error {
	u.mu.Lock()
	u.nodes = append(u.nodes, nodeName)
	u.mu.Unlock()

	// The dispatcher calls fn on its own goroutine.
	go fn(ctx, u.conn)
	return nil
}

func (u *fakeUnicaster) nodeNames() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.nodes)
}

// newTestAgentLogFetcher serves agent on 127.0.0.1 and returns a fetcher
// whose dispatcher sends every call there.
func newTestAgentLogFetcher(t *testing.T, agent *fakeAgent) (*AgentLogFetcher, *fakeUnicaster) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	clusteragentpb.RegisterLogRecordsServiceServer(srv, agent)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	u := &fakeUnicaster{conn: conn}
	return &AgentLogFetcher{grpcDispatcher: u}, u
}

func TestNewAgentLogFetcher_UsesDispatcher(t *testing.T) {
	d, err := grpcdispatcher.NewDispatcher(
		"kubernetes://kubetail-cluster-agent.kubetail-system:50051",
		grpcdispatcher.WithKubernetesClientset(fake.NewClientset()),
		grpcdispatcher.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Shutdown() })

	f := NewAgentLogFetcher(d)
	assert.Same(t, d, f.grpcDispatcher)
}

func agentTestRecords(n int) []*clusteragentpb.LogRecord {
	records := make([]*clusteragentpb.LogRecord, n)
	for i, r := range fetcherTestRecords(n) {
		records[i] = &clusteragentpb.LogRecord{Timestamp: timestamppb.New(r.Timestamp), Message: r.Message}
	}
	return records
}

type agentStreamFunc func(f *AgentLogFetcher, ctx context.Context, source LogSource, opts FetcherOptions) (<-chan LogRecord, error)

var agentStreamFuncs = map[string]agentStreamFunc{
	"forward":  (*AgentLogFetcher).StreamForward,
	"backward": (*AgentLogFetcher).StreamBackward,
}

func TestAgentLogFetcher_SendsRequestFields(t *testing.T) {
	start := fetcherBaseTS.Add(1500 * time.Millisecond)
	stop := fetcherBaseTS.Add(time.Hour)
	opts := FetcherOptions{StartTime: start, StopTime: stop, Grep: "(?i)err"}

	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{}
			f, u := newTestAgentLogFetcher(t, agent)

			ch, err := stream(f, t.Context(), fetcherTestSource, opts)
			require.NoError(t, err)
			assert.Empty(t, drainRecords(t, ch))
			assert.Equal(t, []string{"node-1"}, u.nodeNames())

			reqs := agent.requests()
			require.Len(t, reqs, 1)
			req := reqs[0]
			assert.Equal(t, "ns", req.Namespace)
			assert.Equal(t, "pod-1", req.PodName)
			assert.Equal(t, "app", req.ContainerName)
			assert.Equal(t, "containerd://abc", req.ContainerId)
			assert.Equal(t, "(?i)err", req.Grep)
			assert.Equal(t, start.Format(time.RFC3339Nano), req.StartTime)
			assert.Equal(t, stop.Format(time.RFC3339Nano), req.StopTime)
		})
	}
}

func TestAgentLogFetcher_OmitsZeroTimeBounds(t *testing.T) {
	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{}
			f, _ := newTestAgentLogFetcher(t, agent)

			ch, err := stream(f, t.Context(), fetcherTestSource, FetcherOptions{})
			require.NoError(t, err)
			drainRecords(t, ch)

			reqs := agent.requests()
			require.Len(t, reqs, 1)
			assert.Empty(t, reqs[0].StartTime)
			assert.Empty(t, reqs[0].StopTime)
			assert.Empty(t, reqs[0].Grep)
		})
	}
}

func TestAgentLogFetcher_StreamForward_MapsFollowFrom(t *testing.T) {
	tests := []struct {
		followFrom FollowFrom
		want       clusteragentpb.FollowFrom
	}{
		{FollowFromNoop, clusteragentpb.FollowFrom_NOOP},
		{FollowFromDefault, clusteragentpb.FollowFrom_DEFAULT},
		{FollowFromEnd, clusteragentpb.FollowFrom_END},
	}

	for _, tt := range tests {
		t.Run(string(tt.followFrom), func(t *testing.T) {
			agent := &fakeAgent{}
			f, _ := newTestAgentLogFetcher(t, agent)

			ch, err := f.StreamForward(t.Context(), fetcherTestSource, FetcherOptions{FollowFrom: tt.followFrom})
			require.NoError(t, err)
			drainRecords(t, ch)

			reqs := agent.requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, tt.want, reqs[0].FollowFrom)
		})
	}
}

func TestAgentLogFetcher_StreamForward_UnknownFollowFromSendsNothing(t *testing.T) {
	agent := &fakeAgent{}
	f, _ := newTestAgentLogFetcher(t, agent)

	ch, err := f.StreamForward(t.Context(), fetcherTestSource, FetcherOptions{FollowFrom: "sideways"})
	require.NoError(t, err)
	assert.Empty(t, drainRecords(t, ch))
	assert.Empty(t, agent.requests())
}

func TestAgentLogFetcher_RelaysRecordsInOrder(t *testing.T) {
	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{records: agentTestRecords(3)}
			f, _ := newTestAgentLogFetcher(t, agent)

			ch, err := stream(f, t.Context(), fetcherTestSource, FetcherOptions{})
			require.NoError(t, err)

			records := drainRecords(t, ch)
			want := fetcherTestRecords(3)
			for i := range want {
				want[i].Source = fetcherTestSource
			}
			assert.Equal(t, want, records)
		})
	}
}

func TestAgentLogFetcher_ReportsAgentError(t *testing.T) {
	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{records: agentTestRecords(1), err: status.Error(codes.PermissionDenied, "not allowed")}
			f, _ := newTestAgentLogFetcher(t, agent)

			ch, err := stream(f, t.Context(), fetcherTestSource, FetcherOptions{})
			require.NoError(t, err)

			records := drainRecords(t, ch)
			require.Len(t, records, 2)
			assert.Equal(t, "line 0", records[0].Message)
			assert.Equal(t, codes.PermissionDenied, status.Code(records[1].err))
		})
	}
}

func TestAgentLogFetcher_IgnoresCancelledStatus(t *testing.T) {
	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{records: agentTestRecords(1), err: status.Error(codes.Canceled, "cancelled")}
			f, _ := newTestAgentLogFetcher(t, agent)

			ch, err := stream(f, t.Context(), fetcherTestSource, FetcherOptions{})
			require.NoError(t, err)

			records := drainRecords(t, ch)
			require.Len(t, records, 1)
			assert.NoError(t, records[0].err)
		})
	}
}

func TestAgentLogFetcher_ReportsCallFailure(t *testing.T) {
	for name, stream := range agentStreamFuncs {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{}
			f, _ := newTestAgentLogFetcher(t, agent)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			ch, err := stream(f, ctx, fetcherTestSource, FetcherOptions{})
			require.NoError(t, err)

			records := drainRecords(t, ch)
			require.Len(t, records, 1)
			assert.Equal(t, codes.Canceled, status.Code(records[0].err))
			assert.Empty(t, agent.requests())
		})
	}
}

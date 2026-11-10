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

package graph

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
)

const testDeadline = 5 * time.Second

type fakeWatchAgent struct {
	clusteragentpb.UnimplementedLogMetadataServiceServer
	watch func(stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error
}

func (a *fakeWatchAgent) Watch(_ *clusteragentpb.LogMetadataWatchRequest, stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
	return a.watch(stream)
}

// startWatchAgent serves watch as an agent's LogMetadataService.Watch and
// returns a connection to it.
func startWatchAgent(t *testing.T, watch func(grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error) *grpc.ClientConn {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	clusteragentpb.RegisterLogMetadataServiceServer(srv, &fakeWatchAgent{watch: watch})

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return dialAgent(t, lis.Addr().String())
}

// fakeFanout runs the handler once per connection, as the dispatcher does
// for each agent it knows. handlersDone closes when every handler returns.
type fakeFanout struct {
	conns        []*grpc.ClientConn
	unsubscribed chan struct{}
	handlersDone chan struct{}
}

func newFakeFanout(conns ...*grpc.ClientConn) *fakeFanout {
	return &fakeFanout{
		conns:        conns,
		unsubscribed: make(chan struct{}),
		handlersDone: make(chan struct{}),
	}
}

func (f *fakeFanout) subscribe(ctx context.Context, fn grpcdispatcher.DispatchHandler) (func(), error) {
	var wg sync.WaitGroup
	for _, conn := range f.conns {
		wg.Go(func() { fn(ctx, conn) })
	}
	go func() {
		wg.Wait()
		close(f.handlersDone)
	}()
	return func() { close(f.unsubscribed) }, nil
}

// errorRecorder stands in for transport.AddSubscriptionError.
type errorRecorder struct {
	mu   sync.Mutex
	errs []*gqlerror.Error
}

func (r *errorRecorder) add(_ context.Context, err *gqlerror.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *errorRecorder) get() []*gqlerror.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.errs
}

func waitClosed[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	deadline := time.After(testDeadline)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel did not close")
		}
	}
}

func waitDone(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testDeadline):
		t.Fatal(msg)
	}
}

func TestWatchLogMetadata_ForwardsEventsAndClosesWhenContextIsDone(t *testing.T) {
	t.Parallel()

	conn := startWatchAgent(t, func(stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
		if err := stream.Send(&clusteragentpb.LogMetadataWatchEvent{Type: "ADDED"}); err != nil {
			return err
		}
		<-stream.Context().Done()
		return nil
	})
	fanout := newFakeFanout(conn)
	rec := &errorRecorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	outCh, err := watchLogMetadata(ctx, &clusteragentpb.LogMetadataWatchRequest{}, fanout.subscribe, rec.add)
	require.NoError(t, err)

	select {
	case ev := <-outCh:
		assert.Equal(t, "ADDED", ev.GetType())
	case <-time.After(testDeadline):
		t.Fatal("no event received")
	}

	cancel()

	waitClosed(t, outCh)
	waitDone(t, fanout.unsubscribed, "not unsubscribed")
	waitDone(t, fanout.handlersDone, "handler still running")
	assert.Empty(t, rec.get())
}

func TestWatchLogMetadata_ReportsAgentErrorAndCloses(t *testing.T) {
	t.Parallel()

	failing := startWatchAgent(t, func(grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
		return status.Error(codes.PermissionDenied, "denied")
	})
	healthy := startWatchAgent(t, func(stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
		<-stream.Context().Done()
		return nil
	})
	fanout := newFakeFanout(failing, healthy)
	rec := &errorRecorder{}

	outCh, err := watchLogMetadata(context.Background(), &clusteragentpb.LogMetadataWatchRequest{}, fanout.subscribe, rec.add)
	require.NoError(t, err)

	waitClosed(t, outCh)
	waitDone(t, fanout.handlersDone, "handler still running")

	errs := rec.get()
	require.Len(t, errs, 1)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", errs[0].Extensions["code"])
	assert.Contains(t, errs[0].Message, "denied")
}

func TestWatchLogMetadata_SenderDoesNotBlockAfterDisconnect(t *testing.T) {
	t.Parallel()

	conn := startWatchAgent(t, func(stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
		for _, typ := range []string{"ADDED", "MODIFIED"} {
			if err := stream.Send(&clusteragentpb.LogMetadataWatchEvent{Type: typ}); err != nil {
				return err
			}
		}
		<-stream.Context().Done()
		return nil
	})
	fanout := newFakeFanout(conn)
	rec := &errorRecorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	outCh, err := watchLogMetadata(ctx, &clusteragentpb.LogMetadataWatchRequest{}, fanout.subscribe, rec.add)
	require.NoError(t, err)

	select {
	case <-outCh:
	case <-time.After(testDeadline):
		t.Fatal("no event received")
	}

	// Negative assertion: with nobody reading, the handler stays blocked
	// sending the second event until the client leaves.
	select {
	case <-fanout.handlersDone:
		t.Fatal("handler returned while the client was connected")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	waitDone(t, fanout.handlersDone, "handler blocked after disconnect")
	waitClosed(t, outCh)
	assert.Empty(t, rec.get())
}

func TestWatchLogMetadata_HandlerStartedAfterCloseDoesNotSend(t *testing.T) {
	t.Parallel()

	conn := startWatchAgent(t, func(stream grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
		return stream.Send(&clusteragentpb.LogMetadataWatchEvent{Type: "ADDED"})
	})

	var handler grpcdispatcher.DispatchHandler
	subscribe := func(_ context.Context, fn grpcdispatcher.DispatchHandler) (func(), error) {
		handler = fn
		return func() {}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	outCh, err := watchLogMetadata(ctx, &clusteragentpb.LogMetadataWatchRequest{}, subscribe, (&errorRecorder{}).add)
	require.NoError(t, err)

	cancel()
	waitClosed(t, outCh)

	// The dispatcher can start a handler for a new agent as the watch closes.
	// A live context makes the handler reach its send, which must not panic.
	handlerCtx, handlerCancel := context.WithTimeout(context.Background(), testDeadline)
	defer handlerCancel()
	handler(handlerCtx, conn)
}

func TestWatchLogMetadata_AgentEndingQuietlyKeepsWatchOpen(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"end of stream", nil},
		{"agent unavailable", status.Error(codes.Unavailable, "restarting")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			conn := startWatchAgent(t, func(grpc.ServerStreamingServer[clusteragentpb.LogMetadataWatchEvent]) error {
				return tt.err
			})
			fanout := newFakeFanout(conn)
			rec := &errorRecorder{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			outCh, err := watchLogMetadata(ctx, &clusteragentpb.LogMetadataWatchRequest{}, fanout.subscribe, rec.add)
			require.NoError(t, err)

			waitDone(t, fanout.handlersDone, "handler still running")

			// Negative assertion: the watch must stay open for agents yet to come.
			select {
			case <-outCh:
				t.Fatal("watch ended with the agent's stream")
			case <-time.After(50 * time.Millisecond):
			}

			cancel()
			waitClosed(t, outCh)
			assert.Empty(t, rec.get())
		})
	}
}

func TestWatchLogMetadata_ReturnsSubscribeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("subscribe failed")
	subscribe := func(context.Context, grpcdispatcher.DispatchHandler) (func(), error) {
		return nil, wantErr
	}

	outCh, err := watchLogMetadata(context.Background(), &clusteragentpb.LogMetadataWatchRequest{}, subscribe, (&errorRecorder{}).add)
	require.ErrorIs(t, err, wantErr)
	assert.Nil(t, outCh)
}

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
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/kubetail-org/kubetail/libs/go-common/clusteragentpb"
	"github.com/kubetail-org/kubetail/libs/go-common/grpchelpers"
	"github.com/kubetail-org/kubetail/libs/go-common/k8shelpers"
)

type recordingMetadataServer struct {
	clusteragentpb.UnimplementedLogMetadataServiceServer
	mu  sync.Mutex
	got metadata.MD
}

func (s *recordingMetadataServer) List(ctx context.Context, _ *clusteragentpb.LogMetadataListRequest) (*clusteragentpb.LogMetadataList, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.mu.Lock()
	s.got = md.Copy()
	s.mu.Unlock()
	return &clusteragentpb.LogMetadataList{}, nil
}

func (s *recordingMetadataServer) capturedMetadata() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got
}

func startRecordingAgent(t *testing.T) (string, *recordingMetadataServer) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	rec := &recordingMetadataServer{}
	srv := grpc.NewServer()
	clusteragentpb.RegisterLogMetadataServiceServer(srv, rec)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), rec
}

// dialAgent installs the same impersonation interceptors mustNewGrpcDispatcher
// uses in production. The dispatcher itself is bypassed: its own tests cover
// Fanout's context preservation, and its endpoint-discovery informer doesn't
// sync against a fake k8s clientset. What we lock down here is the wire-level
// chain: ctx-with-ImpersonateInfo → ImpersonateUnaryClientInterceptor →
// x-remote-* metadata. A regression that drops the interceptor would silently
// downgrade every call to run as the cluster-api ServiceAccount.
func dialAgent(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		addr,
		grpc.WithUnaryInterceptor(grpchelpers.ImpersonateUnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpchelpers.ImpersonateStreamClientInterceptor),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestImpersonateInfoOnContextReachesAgentAsRemoteHeaders(t *testing.T) {
	t.Parallel()
	addr, rec := startRecordingAgent(t)
	conn := dialAgent(t, addr)
	c := clusteragentpb.NewLogMetadataServiceClient(conn)

	info := &k8shelpers.ImpersonateInfo{
		User:   "alice@example.com",
		Groups: []string{"system:authenticated", "devs"},
		Extras: map[string][]string{"scopes": {"read", "write"}},
	}
	ctx, cancel := context.WithTimeout(
		context.WithValue(context.Background(), k8shelpers.K8SImpersonateCtxKey, info),
		5*time.Second,
	)
	defer cancel()

	_, err := c.List(ctx, &clusteragentpb.LogMetadataListRequest{})
	require.NoError(t, err)

	got := rec.capturedMetadata()
	require.Equal(t, []string{"alice@example.com"}, got.Get("x-remote-user"))
	require.ElementsMatch(t, []string{"system:authenticated", "devs"}, got.Get("x-remote-group"))
	// Extras ride in a single JSON header — see grpchelpers.withImpersonateMetadata.
	require.Len(t, got.Get("x-remote-extras"), 1)
	require.Contains(t, got.Get("x-remote-extras")[0], `"scopes"`)
}

// Inverse of the above: with no ImpersonateInfo on ctx, the chain must not
// synthesize a default identity — letting the agent reject is the right
// outcome.
func TestNoImpersonateInfoOnContextSendsNoRemoteHeaders(t *testing.T) {
	t.Parallel()
	addr, rec := startRecordingAgent(t)
	conn := dialAgent(t, addr)
	c := clusteragentpb.NewLogMetadataServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.List(ctx, &clusteragentpb.LogMetadataListRequest{})
	require.NoError(t, err)

	got := rec.capturedMetadata()
	require.Empty(t, got.Get("x-remote-user"))
	require.Empty(t, got.Get("x-remote-group"))
	require.Empty(t, got.Get("x-remote-extras"))
}

// recordingLogRecordsServer captures the gRPC metadata seen on a streaming
// call so tests can pin the wire-level identity headers reaching the agent.
// Both StreamForward and StreamBackward record under the same fields — these
// are server-streaming RPCs and the client sends headers when the stream is
// established, before any messages flow.
type recordingLogRecordsServer struct {
	clusteragentpb.UnimplementedLogRecordsServiceServer
	mu             sync.Mutex
	forwardCalled  bool
	backwardCalled bool
	forwardMD      metadata.MD
	backwardMD     metadata.MD
}

func (s *recordingLogRecordsServer) StreamForward(_ *clusteragentpb.LogRecordsStreamRequest, stream clusteragentpb.LogRecordsService_StreamForwardServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	s.mu.Lock()
	s.forwardCalled = true
	s.forwardMD = md.Copy()
	s.mu.Unlock()
	return nil
}

func (s *recordingLogRecordsServer) StreamBackward(_ *clusteragentpb.LogRecordsStreamRequest, stream clusteragentpb.LogRecordsService_StreamBackwardServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	s.mu.Lock()
	s.backwardCalled = true
	s.backwardMD = md.Copy()
	s.mu.Unlock()
	return nil
}

func (s *recordingLogRecordsServer) capturedForward() (bool, metadata.MD) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forwardCalled, s.forwardMD
}

func (s *recordingLogRecordsServer) capturedBackward() (bool, metadata.MD) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backwardCalled, s.backwardMD
}

func startRecordingLogRecordsAgent(t *testing.T) (string, *recordingLogRecordsServer) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	rec := &recordingLogRecordsServer{}
	srv := grpc.NewServer()
	clusteragentpb.RegisterLogRecordsServiceServer(srv, rec)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), rec
}

// drainStream reads frames until the stream completes (clean EOF) or the
// context fires. Both StreamForward and StreamBackward are server-streaming
// RPCs; the test recording server returns nil immediately, so a successful
// drain ends with io.EOF surfacing as a non-nil err on Recv.
func drainStream(t *testing.T, recv func() error) {
	t.Helper()
	for {
		if err := recv(); err != nil {
			return
		}
	}
}

// Pins the streaming counterpart of TestImpersonateInfoOnContextReachesAgentAsRemoteHeaders.
// LogRecordsFetch / LogRecordsFollow / the download handler all reach the
// cluster-agent via LogRecordsService.StreamForward (and StreamBackward for
// reverse-time download). gRPC streaming uses ImpersonateStreamClientInterceptor,
// a separate code path from the unary interceptor — a regression that breaks
// ctx propagation through the streaming wrapper would silently downgrade
// every streaming call to run as the cluster-api ServiceAccount.
func TestImpersonateInfoOnContextReachesAgentOnStreamingRPCs(t *testing.T) {
	tests := []struct {
		name string
		open func(ctx context.Context, c clusteragentpb.LogRecordsServiceClient) (interface {
			Recv() (*clusteragentpb.LogRecord, error)
		}, error)
		read func(rec *recordingLogRecordsServer) (bool, metadata.MD)
	}{
		{
			name: "StreamForward",
			open: func(ctx context.Context, c clusteragentpb.LogRecordsServiceClient) (interface {
				Recv() (*clusteragentpb.LogRecord, error)
			}, error) {
				return c.StreamForward(ctx, &clusteragentpb.LogRecordsStreamRequest{})
			},
			read: (*recordingLogRecordsServer).capturedForward,
		},
		{
			name: "StreamBackward",
			open: func(ctx context.Context, c clusteragentpb.LogRecordsServiceClient) (interface {
				Recv() (*clusteragentpb.LogRecord, error)
			}, error) {
				return c.StreamBackward(ctx, &clusteragentpb.LogRecordsStreamRequest{})
			},
			read: (*recordingLogRecordsServer).capturedBackward,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			addr, rec := startRecordingLogRecordsAgent(t)
			conn := dialAgent(t, addr)
			c := clusteragentpb.NewLogRecordsServiceClient(conn)

			info := &k8shelpers.ImpersonateInfo{
				User:   "alice@example.com",
				Groups: []string{"system:authenticated", "devs"},
				Extras: map[string][]string{"scopes": {"read", "write"}},
			}
			ctx, cancel := context.WithTimeout(
				context.WithValue(context.Background(), k8shelpers.K8SImpersonateCtxKey, info),
				5*time.Second,
			)
			defer cancel()

			stream, err := tt.open(ctx, c)
			require.NoError(t, err)
			drainStream(t, func() error { _, err := stream.Recv(); return err })

			called, got := tt.read(rec)
			require.True(t, called, "server method must have been invoked")
			require.Equal(t, []string{"alice@example.com"}, got.Get("x-remote-user"))
			require.ElementsMatch(t, []string{"system:authenticated", "devs"}, got.Get("x-remote-group"))
			require.Len(t, got.Get("x-remote-extras"), 1)
			require.Contains(t, got.Get("x-remote-extras")[0], `"scopes"`)
		})
	}
}

// Inverse of the above for streaming RPCs: ctx without ImpersonateInfo must
// produce no x-remote-* metadata so the agent rejects rather than acting as
// some default identity.
func TestNoImpersonateInfoOnContextSendsNoRemoteHeadersOnStreamingRPCs(t *testing.T) {
	t.Parallel()
	addr, rec := startRecordingLogRecordsAgent(t)
	conn := dialAgent(t, addr)
	c := clusteragentpb.NewLogRecordsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := c.StreamForward(ctx, &clusteragentpb.LogRecordsStreamRequest{})
	require.NoError(t, err)
	drainStream(t, func() error { _, err := stream.Recv(); return err })

	called, got := rec.capturedForward()
	require.True(t, called)
	require.Empty(t, got.Get("x-remote-user"))
	require.Empty(t, got.Get("x-remote-group"))
	require.Empty(t, got.Get("x-remote-extras"))
}

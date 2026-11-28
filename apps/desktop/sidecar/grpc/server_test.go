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

package grpcserver_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	grpcserver "github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// IsGRPCRequest is the gRPC half of the socket's routing rule: HTTP/2 *and* the
// gRPC content-type. HTTP/1.1 (GraphQL POST/SSE) and a hypothetical HTTP/2
// GraphQL client must both fall through.
func TestIsGRPCRequest(t *testing.T) {
	tests := []struct {
		name        string
		protoMajor  int
		contentType string
		want        bool
	}{
		{"http2 grpc", 2, "application/grpc", true},
		{"http2 grpc+proto", 2, "application/grpc+proto", true},
		{"http1 grpc", 1, "application/grpc", false},
		{"http2 graphql json", 2, "application/json", false},
		{"http2 no content-type", 2, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{ProtoMajor: tt.protoMajor, Header: http.Header{}}
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}
			require.Equal(t, tt.want, grpcserver.IsGRPCRequest(r))
		})
	}
}

// Stop waits for a method handler still running after its client reset the stream.
// ServeHTTP has returned by then, so Stop is the only join on that handler.
func TestStopWaitsForAResetStreamsHandler(t *testing.T) {
	svc := signedInFakeAuthSvc(oauth.Identity{})
	svc.logoutEntered = make(chan struct{})
	svc.logoutGate = make(chan struct{})
	release := sync.OnceFunc(func() { close(svc.logoutGate) })
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(release)

	ctx, reset := context.WithCancel(context.Background())
	called := make(chan struct{})
	go func() {
		defer close(called)
		_, _ = authpb.NewAuthServiceClient(conn).Logout(ctx, &authpb.LogoutRequest{})
	}()
	testutil.WaitClosed(t, svc.logoutEntered, "Logout to start")
	reset()
	testutil.Wait(t, called, "the client to give up on Logout")

	stopped := make(chan struct{})
	go func() {
		grpcSrv.Stop()
		close(stopped)
	}()
	// A negative assertion: Stop has nothing to wake it but the handler's return.
	testutil.NoRecv(t, stopped, 50*time.Millisecond, "Stop while Logout runs")
	release()
	testutil.Wait(t, stopped, "Stop to return once Logout has")
}

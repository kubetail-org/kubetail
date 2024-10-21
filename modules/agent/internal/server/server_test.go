// Copyright 2024 Andres Morey
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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/modules/common/config"
	"github.com/kubetail-org/kubetail/modules/common/grpchelpers"
	"github.com/kubetail-org/kubetail/modules/common/testpb"
)

func TestAuthModeNotToken(t *testing.T) {
	tests := []struct {
		name        string
		setAuthMode config.AuthMode
	}{
		{"cluster", config.AuthModeCluster},
		{"local", config.AuthModeLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.AuthMode = tt.setAuthMode

			// init server
			server := NewTestServer(cfg)
			defer server.Stop()

			// init client
			client, err := server.NewTestClient()
			require.Nil(t, err)
			defer client.Close()

			// execute request
			resp, err := client.Echo(context.Background(), &testpb.EchoRequest{Message: "xxx"})
			require.Nil(t, err)
			require.Equal(t, "xxx", resp.Message)
		})
	}
}

func TestRequestWithoutAuthClientInterceptor(t *testing.T) {
	cfg := config.DefaultConfig()

	// init server
	server := NewTestServer(cfg)
	defer server.Stop()

	// init client
	client, err := server.NewTestClient()
	require.Nil(t, err)
	defer client.Close()

	// execute request
	resp, err := client.Echo(context.Background(), &testpb.EchoRequest{})
	require.Nil(t, resp)
	require.ErrorIs(t, err, status.Errorf(codes.Unauthenticated, "missing token"))
}

func TestRequestWithAuthClientInterceptorSuccess(t *testing.T) {
	cfg := config.DefaultConfig()

	// init server
	server := NewTestServer(cfg)
	defer server.Stop()

	// init client
	client, err := server.NewTestClient(grpc.WithUnaryInterceptor(grpchelpers.NewUnaryAuthClientInterceptor(cfg)))
	require.Nil(t, err)
	defer client.Close()

	// add token to context
	ctx := context.WithValue(context.Background(), grpchelpers.K8STokenCtxKey, "token-value")

	// execute request
	resp, err := client.Echo(ctx, &testpb.EchoRequest{Message: "xxx"})
	require.Nil(t, err)
	require.Equal(t, "xxx", resp.Message)
}

func TestRequestWithAuthClientInterceptorFailure(t *testing.T) {
	cfg := config.DefaultConfig()

	// init server
	server := NewTestServer(cfg)
	defer server.Stop()

	// init client
	client, err := server.NewTestClient(grpc.WithUnaryInterceptor(grpchelpers.NewUnaryAuthClientInterceptor(cfg)))
	require.Nil(t, err)
	defer client.Close()

	// execute request
	resp, err := client.Echo(context.Background(), &testpb.EchoRequest{Message: "xxx"})
	require.Nil(t, resp)
	require.ErrorIs(t, err, status.Errorf(codes.FailedPrecondition, "missing token in context"))
}

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
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpcserver "github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/pokepb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// A PokeService.Poke RPC must broadcast a SourceHost resync to every in-process
// subscriber — this is the path the native host drives on OS resume / network-on.
func TestPokeServiceBroadcastsSourceHost(t *testing.T) {
	// A real broadcaster; we never start its detector, so the only signal is the
	// one the RPC fires.
	pk := poke.New()
	sigs, cancel := pk.ListenForNotifications()
	defer cancel()

	grpcSrv := grpcserver.NewServer(context.Background(), nil, pk, nil)
	t.Cleanup(grpcSrv.Stop)
	conn := newGRPCTestConn(t, grpcSrv)
	client := pokepb.NewPokeServiceClient(conn)

	_, err := client.Poke(context.Background(), &pokepb.PokeRequest{})
	require.NoError(t, err)

	sig := testutil.Recv(t, sigs, "the Poke RPC to broadcast a signal")
	require.Equal(t, poke.SourceHost, sig.Source)
}

// A nil broadcaster (degraded run) keeps the method safe: Poke returns
// Unavailable rather than panicking, mirroring the nil-auth guard.
func TestPokeServiceNilDegradesUnavailable(t *testing.T) {
	grpcSrv := grpcserver.NewServer(context.Background(), nil, nil, nil)
	t.Cleanup(grpcSrv.Stop)
	conn := newGRPCTestConn(t, grpcSrv)
	client := pokepb.NewPokeServiceClient(conn)

	_, err := client.Poke(context.Background(), &pokepb.PokeRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
}

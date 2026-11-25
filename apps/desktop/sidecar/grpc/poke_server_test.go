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

	grpcSrv := grpcserver.NewServer(context.Background(), nil, pk)
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
	grpcSrv := grpcserver.NewServer(context.Background(), nil, nil)
	t.Cleanup(grpcSrv.Stop)
	conn := newGRPCTestConn(t, grpcSrv)
	client := pokepb.NewPokeServiceClient(conn)

	_, err := client.Poke(context.Background(), &pokepb.PokeRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
}

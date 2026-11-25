// Package grpcserver builds the sidecar's gRPC surface: AuthService (the tray's account
// section) and PokeService (the host's wake/network-return nudge). It rides the same
// socket as GraphQL via h2c multiplexing keyed on IsGRPCRequest, and is consumed only by
// the native host, never the webview.
// See docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.
package grpcserver

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/pokepb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// IsGRPCRequest IS the definition of a gRPC request — HTTP/2 plus the gRPC content-type —
// so it lives beside the server the h2c dispatcher routes with. Requiring the
// content-type, not just HTTP/2, keeps a future HTTP/2 GraphQL client falling through.
func IsGRPCRequest(r *http.Request) bool {
	return r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
}

// NewServer registers both services on a fresh *grpc.Server; a nil authSvc/pokeSvc
// degrades safely. Ending streamsCtx ends every streaming handler, so grpc flushes OK
// trailers — the analogue of an SSE terminal frame.
//
// Stop waits for every method handler: a handler runs on its own goroutine, so
// ServeHTTP returns before it does once the client resets the stream.
//
// net/http owns the connection, so grpc-go's keepalive options would not apply. An open
// AuthStateWatch keeps its connection out of the HTTP server's IdleTimeout, which counts
// only a connection with no stream open.
func NewServer(streamsCtx context.Context, authSvc AuthConnector, pokeSvc *poke.Service) *grpc.Server {
	srv := grpc.NewServer(grpc.WaitForHandlers(true))
	authpb.RegisterAuthServiceServer(srv, &authServer{
		auth:       authSvc,
		streamsCtx: streamsCtx,
	})
	pokepb.RegisterPokeServiceServer(srv, &pokeServer{pokeSvc: pokeSvc})
	return srv
}

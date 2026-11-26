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

//lint:file-ignore SA1019 the test serves gRPC over h2c, as internal/app does until kstackhq/kstack#448.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	grpcserver "github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// fakeAuthSvc is the Kubetail Cloud connector as the gRPC server uses it: signed out
// by default; Connect signs in and publishes, Disconnect signs out and
// publishes, WatchState is latest-value (current-on-subscribe, then changes).
type fakeAuthSvc struct {
	mu      sync.Mutex
	loginAs oauth.Identity

	connectErr    error // when set, Connect returns this error without signing in
	disconnectErr error // when set, Disconnect returns this error without signing out

	// When set, Disconnect closes logoutEntered and blocks until logoutGate closes.
	logoutEntered chan struct{}
	logoutGate    chan struct{}

	hub *watch.Hub[connectors.State]
	tx  *watch.Sender[connectors.State]
}

func newFakeAuthSvc(loginAs oauth.Identity) *fakeAuthSvc {
	return newFakeAuthSvcState(connectors.State{Phase: connectors.SignedOut}, loginAs)
}

func signedInFakeAuthSvc(id oauth.Identity) *fakeAuthSvc {
	return newFakeAuthSvcState(connectedAs(id), id)
}

func newFakeAuthSvcState(st connectors.State, loginAs oauth.Identity) *fakeAuthSvc {
	f := &fakeAuthSvc{loginAs: loginAs, hub: watch.New(st)}
	f.tx = f.hub.Sender()
	return f
}

// connectedAs is the state of a session signed in as id.
func connectedAs(id oauth.Identity) connectors.State {
	return connectors.State{Phase: connectors.Connected, Subject: id.UserID, Email: id.Email, Name: id.Name}
}

func (f *fakeAuthSvc) Connect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connectErr != nil {
		return f.connectErr
	}
	f.publishLocked(connectedAs(f.loginAs))
	return nil
}

func (f *fakeAuthSvc) Disconnect(context.Context) error {
	if f.logoutGate != nil {
		close(f.logoutEntered)
		<-f.logoutGate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.disconnectErr != nil {
		return f.disconnectErr
	}
	f.publishLocked(connectors.State{Phase: connectors.SignedOut})
	return nil
}

func (f *fakeAuthSvc) WatchState() (<-chan connectors.State, func()) {
	rx := f.hub.Receiver()
	return rx.Chan(), rx.Close
}

// publishLocked makes st the state (f.mu held).
func (f *fakeAuthSvc) publishLocked(st connectors.State) {
	f.tx.Send(st) //nolint:errcheck // Send never blocks; a closed hub is a no-op
}

// newGRPCTestConn stands up an h2c HTTP server over the gRPC server and returns
// a connected client connection. Cleaned up on t.Cleanup.
func newGRPCTestConn(t *testing.T, grpcSrv *grpc.Server) *grpc.ClientConn {
	t.Helper()
	_, conn := serveGRPC(t, grpcSrv)
	return conn
}

// serveGRPC is newGRPCTestConn, also returning the HTTP server for a test that
// drives its shutdown.
func serveGRPC(t *testing.T, grpcSrv *grpc.Server) (*http.Server, *grpc.ClientConn) {
	t.Helper()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: grpcSrv, Protocols: &protocols}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return srv, conn
}

// TestAuthStateWatchSnapshotThenDelta verifies the server-streaming behavior:
// the first message is the current state (signed-out on subscribe), and after
// StartLogin the next message delivers the authenticated identity.
func TestAuthStateWatchSnapshotThenDelta(t *testing.T) {
	svc := newFakeAuthSvc(oauth.Identity{UserID: "u1", Email: "ada@example.com", Name: "Ada"})
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)

	// First frame: current-on-subscribe = signed out.
	first, err := stream.Recv()
	require.NoError(t, err)
	require.False(t, first.GetAuthenticated())
	require.Nil(t, first.GetIdentity())

	// StartLogin signs in on the fake at once; the watch must deliver a delta.
	_, err = client.StartLogin(context.Background(), &authpb.StartLoginRequest{})
	require.NoError(t, err)

	// Second frame: signed in with identity.
	second, err := stream.Recv()
	require.NoError(t, err)
	require.True(t, second.GetAuthenticated())
	require.NotNil(t, second.GetIdentity())
	require.Equal(t, "ada@example.com", second.GetIdentity().GetEmail())
	require.Equal(t, "Ada", second.GetIdentity().GetName())
	require.Equal(t, "u1", second.GetIdentity().GetUserId())
}

// TestStartLoginAndLogoutRPC verifies the unary RPCs succeed on a functional
// fake and that Logout causes the next watch to start signed-out.
func TestStartLoginAndLogoutRPC(t *testing.T) {
	svc := signedInFakeAuthSvc(oauth.Identity{Email: "ada@example.com"})
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)

	// StartLogin should succeed (fake does not error by default).
	_, err := client.StartLogin(context.Background(), &authpb.StartLoginRequest{})
	require.NoError(t, err)

	// Logout should succeed.
	_, err = client.Logout(context.Background(), &authpb.LogoutRequest{})
	require.NoError(t, err)

	// A fresh watch must now start signed-out.
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	first, err := stream.Recv()
	require.NoError(t, err)
	require.False(t, first.GetAuthenticated())
}

// TestAuthRPCsNilServiceTolerant verifies that passing nil auth to NewServer
// degrades safely: AuthStateWatch ends immediately (clean io.EOF), and the
// unary RPCs return Unavailable — mirroring the GraphQL resolver's nil-Auth
// degrade.
func TestAuthRPCsNilServiceTolerant(t *testing.T) {
	grpcSrv := grpcserver.NewServer(context.Background(), nil, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)

	// AuthStateWatch on a nil service must end immediately with io.EOF.
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)

	// Unary RPCs must return Unavailable.
	_, err = client.StartLogin(context.Background(), &authpb.StartLoginRequest{})
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unavailable, st.Code())

	_, err = client.Logout(context.Background(), &authpb.LogoutRequest{})
	st, _ = status.FromError(err)
	require.Equal(t, codes.Unavailable, st.Code())
}

// TestAuthStateWatchEndsOnShutdown verifies that ending the streams context ends
// the watch: the handler returns nil (grpc flushes OK trailers), and the client
// sees io.EOF.
func TestAuthStateWatchEndsOnShutdown(t *testing.T) {
	svc := newFakeAuthSvc(oauth.Identity{})
	streams, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	grpcSrv := grpcserver.NewServer(streams, svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)

	client := authpb.NewAuthServiceClient(conn)
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	// Consume the initial snapshot so the handler is blocked in its select loop.
	_, err = stream.Recv()
	require.NoError(t, err)

	recvErr := make(chan error, 1)
	go func() {
		_, e := stream.Recv()
		recvErr <- e
	}()

	endStreams()
	require.ErrorIs(t, testutil.Recv(t, recvErr, "the AuthStateWatch stream to end after the streams ended"), io.EOF)

	grpcSrv.Stop()
}

// A subscription the auth service closes ends the watch cleanly: the client sees
// io.EOF.
func TestAuthStateWatchEndsWhenTheSubscriptionCloses(t *testing.T) {
	svc := newFakeAuthSvc(oauth.Identity{})
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)

	svc.hub.Close()
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}

// A frame that cannot be sent ends the watch with the send's error. proto3 refuses to
// marshal a string that is not UTF-8, so such an email fails the first send.
func TestAuthStateWatchEndsOnAFailedSend(t *testing.T) {
	svc := signedInFakeAuthSvc(oauth.Identity{Email: "\xff"})
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)
	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, "UTF-8")
}

// HTTP shutdown ends a watch at once but lets an in-flight call finish: the
// AuthStateWatch stream ends with OK trailers while Logout is still running, and
// Shutdown returns only after Logout has answered.
func TestShutdownEndsWatchesAndFinishesCalls(t *testing.T) {
	svc := signedInFakeAuthSvc(oauth.Identity{Email: "ada@example.com"})
	svc.logoutEntered = make(chan struct{})
	svc.logoutGate = make(chan struct{})
	streams, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	grpcSrv := grpcserver.NewServer(streams, svc, nil)
	t.Cleanup(grpcSrv.Stop)
	srv, conn := serveGRPC(t, grpcSrv)
	srv.RegisterOnShutdown(endStreams)
	client := authpb.NewAuthServiceClient(conn)

	stream, err := client.AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)

	logoutErr := make(chan error, 1)
	go func() {
		_, err := client.Logout(context.Background(), &authpb.LogoutRequest{})
		logoutErr <- err
	}()
	testutil.Wait(t, svc.logoutEntered, "Logout to start")

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- srv.Shutdown(context.Background()) }()

	watchErr := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		watchErr <- err
	}()
	require.ErrorIs(t, testutil.Recv(t, watchErr, "the watch to end"), io.EOF)
	select {
	case err := <-shutdownErr:
		t.Fatalf("Shutdown returned %v with Logout still running", err)
	default:
	}

	close(svc.logoutGate)
	require.NoError(t, testutil.Recv(t, logoutErr, "Logout to answer"))
	require.NoError(t, testutil.Recv(t, shutdownErr, "Shutdown to return"))
}

// An auth service that fails reports Internal, not a silent success: the tray
// menu drives these RPCs, and a swallowed error would leave it showing the
// wrong session.
func TestAuthUnaryRPCsSurfaceServiceFailures(t *testing.T) {
	svc := newFakeAuthSvc(oauth.Identity{})
	svc.connectErr = errors.New("kubetail: the sign-in could not start: Refused")
	svc.disconnectErr = errors.New("keyring locked")
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	client := authpb.NewAuthServiceClient(conn)

	_, err := client.StartLogin(context.Background(), &authpb.StartLoginRequest{})
	st, _ := status.FromError(err)
	require.Equal(t, codes.Internal, st.Code())
	require.Contains(t, st.Message(), "the sign-in could not start: Refused")

	_, err = client.Logout(context.Background(), &authpb.LogoutRequest{})
	st, _ = status.FromError(err)
	require.Equal(t, codes.Internal, st.Code())
	require.Contains(t, st.Message(), "keyring locked")
}

// A connector that has not read the keyring yet is restoring on the wire, and
// says nothing of a session.
func TestAuthStateWatchCarriesRestoring(t *testing.T) {
	svc := newFakeAuthSvcState(connectors.State{Phase: connectors.Restoring}, oauth.Identity{})
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)

	stream, err := authpb.NewAuthServiceClient(conn).AuthStateWatch(context.Background(), &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)
	first, err := stream.Recv()
	require.NoError(t, err)

	require.True(t, first.GetRestoring())
	require.False(t, first.GetAuthenticated())
	require.Nil(t, first.GetIdentity())
}

// A sign-in or sign-out asked for while the connector restores is refused as
// a failed precondition, not an internal failure.
func TestAuthUnaryRPCsRefuseWhileRestoring(t *testing.T) {
	svc := newFakeAuthSvc(oauth.Identity{})
	svc.connectErr = connectors.ErrRestoring
	svc.disconnectErr = connectors.ErrRestoring
	grpcSrv := grpcserver.NewServer(context.Background(), svc, nil)
	conn := newGRPCTestConn(t, grpcSrv)
	t.Cleanup(grpcSrv.Stop)
	client := authpb.NewAuthServiceClient(conn)

	_, err := client.StartLogin(context.Background(), &authpb.StartLoginRequest{})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = client.Logout(context.Background(), &authpb.LogoutRequest{})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

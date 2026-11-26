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

package grpcserver

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors"
)

// AuthConnector is the Kubetail Cloud connector as the tray drives it.
type AuthConnector interface {
	WatchState() (<-chan connectors.State, func())
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

// authServer implements authpb.AuthServiceServer; a nil auth degrades safely
// (Unavailable / empty stream).
type authServer struct {
	authpb.UnimplementedAuthServiceServer
	auth AuthConnector
	// streamsCtx ends at shutdown, ending every watch cleanly.
	streamsCtx context.Context
}

// AuthStateWatch streams the AuthState current-on-subscribe, then on every change
// of account, returning when the client cancels or the sidecar shuts down.
func (s *authServer) AuthStateWatch(_ *authpb.AuthStateWatchRequest, stream authpb.AuthService_AuthStateWatchServer) error {
	if s.auth == nil {
		return nil // clean OK
	}

	ch, cancel := s.auth.WatchState()
	defer cancel()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.streamsCtx.Done():
			return nil // shutting down; end cleanly
		case st, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(toAuthState(st)); err != nil {
				return err
			}
		}
	}
}

// StartLogin returns once the browser opens, and errs when the sign-in could not
// start; the sign-in completes in the background and is delivered via
// AuthStateWatch.
func (s *authServer) StartLogin(ctx context.Context, _ *authpb.StartLoginRequest) (*authpb.StartLoginResponse, error) {
	if s.auth == nil {
		return nil, status.Error(codes.Unavailable, "no auth service")
	}
	if err := s.auth.Connect(ctx); err != nil {
		return nil, authError(err)
	}
	return &authpb.StartLoginResponse{}, nil
}

// Logout clears local credentials and revokes on a goroutine; it errs only when the
// sign-out could not be recorded and the credential is still stored.
func (s *authServer) Logout(ctx context.Context, _ *authpb.LogoutRequest) (*authpb.LogoutResponse, error) {
	if s.auth == nil {
		return nil, status.Error(codes.Unavailable, "no auth service")
	}
	if err := s.auth.Disconnect(ctx); err != nil {
		return nil, authError(err)
	}
	return &authpb.LogoutResponse{}, nil
}

// authError is a failed sign-in or sign-out as a status: a connector that has
// not read the keyring yet refuses it as FailedPrecondition, and anything else
// is Internal.
func authError(err error) error {
	if errors.Is(err, connectors.ErrRestoring) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// toAuthState projects the connector's state to the wire; Identity is set only
// while signed in.
func toAuthState(s connectors.State) *authpb.AuthState {
	if s.Phase != connectors.Connected {
		return &authpb.AuthState{Restoring: s.Phase == connectors.Restoring}
	}
	return &authpb.AuthState{
		Authenticated: true,
		Identity:      &authpb.Identity{UserId: s.Subject, Email: s.Email, Name: s.Name},
	}
}

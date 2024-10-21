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

package grpchelpers

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/modules/common/config"
)

type ctxKey int

const K8STokenCtxKey ctxKey = iota

// Create new auth server interceptor
func NewUnaryAuthServerInterceptor(cfg *config.Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// continue if auth-mode is not `token`
		if cfg.AuthMode != config.AuthModeToken {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		authorization := md["authorization"]

		if len(authorization) < 1 {
			return nil, status.Errorf(codes.Unauthenticated, "missing token")
		}

		// add token to context
		newCtx := context.WithValue(ctx, K8STokenCtxKey, authorization[0])

		return handler(newCtx, req)
	}
}

// Create new auth client interceptor
func NewUnaryAuthClientInterceptor(cfg *config.Config) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// continue if auth-mode is not `token`
		if cfg.AuthMode != config.AuthModeToken {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		// get token from context
		token, ok := ctx.Value(K8STokenCtxKey).(string)
		if !ok {
			return status.Errorf(codes.FailedPrecondition, "missing token in context")
		}

		// add to metadata and continue execution
		newCtx := metadata.AppendToOutgoingContext(ctx, "authorization", token)
		return invoker(newCtx, method, req, reply, cc, opts...)
	}
}

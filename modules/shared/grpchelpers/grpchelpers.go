// Copyright 2024-2025 Andres Morey
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
	"google.golang.org/grpc/metadata"
)

type ctxKey int

const K8STokenCtxKey ctxKey = iota

// Represents wrap of original stream that returns modified context
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context {
	return s.ctx
}

// Create new auth server unary interceptor
func AuthUnaryServerInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Add token to context, if present
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		authorization := md["authorization"]
		if len(authorization) > 0 {
			// Add token to context
			ctx = context.WithValue(ctx, K8STokenCtxKey, authorization[0])
		}
	}

	// Continue
	return handler(ctx, req)
}

// Create new auth client unary interceptor
func AuthUnaryClientInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	// Get token context and add to metadata, if present
	if token, ok := ctx.Value(K8STokenCtxKey).(string); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
	}

	// Continue
	return invoker(ctx, method, req, reply, cc, opts...)
}

// Create new auth server stream interceptor
func AuthStreamServerInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()

	// Add token to context, if present
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		authorization := md["authorization"]
		if len(authorization) > 0 {
			// Add token to context
			ctx = context.WithValue(ctx, K8STokenCtxKey, authorization[0])
		}
	}

	newStream := &wrappedStream{
		ServerStream: ss,
		ctx:          ctx,
	}

	// Continue
	return handler(srv, newStream)
}

// Create new auth client stream interceptor
func AuthStreamClientInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// Get token from context and add to metadata, if present
	if token, ok := ctx.Value(K8STokenCtxKey).(string); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
	}

	// Call the original streamer to proceed with the RPC
	clientStream, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}

	return clientStream, nil
}

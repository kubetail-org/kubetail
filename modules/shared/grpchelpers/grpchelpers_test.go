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

package grpchelpers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func TestAuthUnaryServerInterceptor(t *testing.T) {
	t.Run("unauthenticated ctx", func(t *testing.T) {
		mockHandler := func(ctx context.Context, req any) (any, error) {
			val := ctx.Value(K8STokenCtxKey)
			assert.Nil(t, val)
			return req, nil
		}

		ctxIn := context.Background()
		AuthUnaryServerInterceptor(ctxIn, nil, nil, mockHandler)
	})

	t.Run("authenticated ctx", func(t *testing.T) {
		mockHandler := func(ctx context.Context, req any) (any, error) {
			val, _ := ctx.Value(K8STokenCtxKey).(string)
			assert.Equal(t, "xxx", val)
			return req, nil
		}

		ctxIn := context.WithValue(context.Background(), K8STokenCtxKey, "xxx")
		AuthUnaryServerInterceptor(ctxIn, nil, nil, mockHandler)
	})
}

func TestAuthUnaryClientInterceptor(t *testing.T) {
	t.Run("unauthenticated ctx", func(t *testing.T) {
		mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			val := ctx.Value(K8STokenCtxKey)
			assert.Nil(t, val)
			return nil
		}

		ctxIn := context.Background()
		AuthUnaryClientInterceptor(ctxIn, "", nil, nil, nil, mockInvoker)
	})

	t.Run("authenticated ctx", func(t *testing.T) {
		mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			val, _ := ctx.Value(K8STokenCtxKey).(string)
			assert.Equal(t, "xxx", val)
			return nil
		}

		ctxIn := context.WithValue(context.Background(), K8STokenCtxKey, "xxx")
		AuthUnaryClientInterceptor(ctxIn, "", nil, nil, nil, mockInvoker)
	})
}

func TestAuthStreamServerInterceptor(t *testing.T) {
	t.Run("unauthenticated ctx", func(t *testing.T) {
		mockHandler := func(srv any, stream grpc.ServerStream) error {
			ctx := stream.Context()
			val := ctx.Value(K8STokenCtxKey)
			assert.Nil(t, val)
			return nil
		}

		ctxIn := context.Background()
		ss := &wrappedStream{ctx: ctxIn}
		AuthStreamServerInterceptor(nil, ss, nil, mockHandler)
	})

	t.Run("authenticated ctx", func(t *testing.T) {
		mockHandler := func(srv any, stream grpc.ServerStream) error {
			ctx := stream.Context()
			val, _ := ctx.Value(K8STokenCtxKey).(string)
			assert.Equal(t, "xxx", val)
			return nil
		}

		ctxIn := context.WithValue(context.Background(), K8STokenCtxKey, "xxx")
		ss := &wrappedStream{ctx: ctxIn}
		AuthStreamServerInterceptor(nil, ss, nil, mockHandler)
	})

}

func TestAuthStreamClientInterceptor(t *testing.T) {
	t.Run("unauthenticated ctx", func(t *testing.T) {
		mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			val := ctx.Value(K8STokenCtxKey)
			assert.Nil(t, val)
			return nil, nil
		}

		ctxIn := context.Background()
		AuthStreamClientInterceptor(ctxIn, nil, nil, "", mockStreamer)
	})

	t.Run("authenticated ctx", func(t *testing.T) {
		mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			val, _ := ctx.Value(K8STokenCtxKey).(string)
			assert.Equal(t, "xxx", val)
			return nil, nil
		}

		ctxIn := context.WithValue(context.Background(), K8STokenCtxKey, "xxx")
		AuthStreamClientInterceptor(ctxIn, nil, nil, "", mockStreamer)
	})
}

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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/kubetail-org/kubetail/libs/go-common/k8shelpers"
)

func TestImpersonateUnaryClientInterceptor(t *testing.T) {
	t.Run("no info passes through unchanged", func(t *testing.T) {
		var captured context.Context
		invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			captured = ctx
			return nil
		}
		err := ImpersonateUnaryClientInterceptor(context.Background(), "", nil, nil, nil, invoker)
		assert.NoError(t, err)

		md, _ := metadata.FromOutgoingContext(captured)
		assert.Empty(t, md.Get("x-remote-user"))
		assert.Empty(t, md.Get("x-remote-group"))
	})

	t.Run("forwards user and groups; extras as JSON in single header", func(t *testing.T) {
		info := &k8shelpers.ImpersonateInfo{
			User:   "alice",
			Groups: []string{"system:authenticated", "devs"},
			Extras: map[string][]string{
				"scopes": {"read", "write"},
				"authentication.kubernetes.io/credential-id": {"abc"},
			},
		}
		ctx := context.WithValue(context.Background(), k8shelpers.K8SImpersonateCtxKey, info)

		var captured context.Context
		invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			captured = ctx
			return nil
		}
		err := ImpersonateUnaryClientInterceptor(ctx, "", nil, nil, nil, invoker)
		assert.NoError(t, err)

		md, ok := metadata.FromOutgoingContext(captured)
		assert.True(t, ok)
		assert.Equal(t, []string{"alice"}, md.Get("x-remote-user"))
		assert.ElementsMatch(t, []string{"system:authenticated", "devs"}, md.Get("x-remote-group"))

		extrasJSON := md.Get("x-remote-extras")
		assert.Len(t, extrasJSON, 1)
		var got map[string][]string
		assert.NoError(t, json.Unmarshal([]byte(extrasJSON[0]), &got))
		assert.Equal(t, info.Extras, got)
	})

	t.Run("user only, no groups, no extras", func(t *testing.T) {
		info := &k8shelpers.ImpersonateInfo{User: "alice"}
		ctx := context.WithValue(context.Background(), k8shelpers.K8SImpersonateCtxKey, info)

		var captured context.Context
		invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			captured = ctx
			return nil
		}
		_ = ImpersonateUnaryClientInterceptor(ctx, "", nil, nil, nil, invoker)

		md, _ := metadata.FromOutgoingContext(captured)
		assert.Equal(t, []string{"alice"}, md.Get("x-remote-user"))
		assert.Empty(t, md.Get("x-remote-group"))
	})
}

func TestImpersonateStreamClientInterceptor(t *testing.T) {
	info := &k8shelpers.ImpersonateInfo{
		User:   "alice",
		Groups: []string{"devs"},
	}
	ctx := context.WithValue(context.Background(), k8shelpers.K8SImpersonateCtxKey, info)

	var captured context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		captured = ctx
		return nil, nil
	}
	_, err := ImpersonateStreamClientInterceptor(ctx, nil, nil, "", streamer)
	assert.NoError(t, err)

	md, ok := metadata.FromOutgoingContext(captured)
	assert.True(t, ok)
	assert.Equal(t, []string{"alice"}, md.Get("x-remote-user"))
	assert.Equal(t, []string{"devs"}, md.Get("x-remote-group"))
}

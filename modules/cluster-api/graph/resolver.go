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

// Package graph implements the cluster-api GraphQL resolvers.
//
// Authentication is enforced upstream by the aggregation auth middleware:
// every request reaching a resolver has already been verified via mTLS or
// front-proxy, and the authenticated identity rides on the request context
// as *k8shelpers.ImpersonateInfo. Per-request Kubernetes API calls run as
// the originating user via the ImpersonatingRoundTripper, so resolvers do
// not perform their own auth checks or token forwarding.
package graph

import (
	"context"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	cm                k8shelpers.ConnectionManager
	grpcDispatcher    *grpcdispatcher.Dispatcher
	allowedNamespaces []string
}

// resolveAllowedNamespaces returns the allowed-namespaces list effective for
// this request: the server-wide list, optionally narrowed (never widened) by
// the session scope forwarded from the dashboard reverse proxy.
func (r *Resolver) resolveAllowedNamespaces(ctx context.Context) []string {
	return k8shelpers.ResolveAllowedNamespaces(ctx, r.allowedNamespaces)
}

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

package graph

import (
	"context"
	"slices"

	"github.com/nats-io/nats.go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/backend/server/internal/grpchelpers"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	k8sCfg            *rest.Config
	nc                *nats.Conn
	gcm               grpchelpers.ConnectionManagerInterface
	allowedNamespaces []string
	TestClientset     *fake.Clientset
	TestDynamicClient *dynamicFake.FakeDynamicClient
}

func (r *Resolver) K8SClientset(ctx context.Context) kubernetes.Interface {
	if r.TestClientset != nil {
		return r.TestClientset
	}

	// copy config
	cfg := rest.CopyConfig(r.k8sCfg)

	// get token from context
	token, ok := ctx.Value(K8STokenCtxKey).(string)
	if ok {
		cfg.BearerToken = token
		cfg.BearerTokenFile = ""
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	return clientset
}

func (r *Resolver) K8SDynamicClient(ctx context.Context) dynamic.Interface {
	if r.TestDynamicClient != nil {
		return r.TestDynamicClient
	}

	// copy config
	cfg := rest.CopyConfig(r.k8sCfg)

	// get token from context
	token, ok := ctx.Value(K8STokenCtxKey).(string)
	if ok {
		cfg.BearerToken = token
		cfg.BearerTokenFile = ""
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	return dynamicClient
}

func (r *Resolver) ToNamespace(namespace *string) (string, error) {
	ns := metav1.NamespaceDefault
	if namespace != nil {
		ns = *namespace
	}

	// perform auth
	if len(r.allowedNamespaces) > 0 && !slices.Contains(r.allowedNamespaces, ns) {
		return "", ErrForbidden
	}

	return ns, nil
}

func (r *Resolver) ToNamespaces(namespace *string) ([]string, error) {
	var namespaces []string

	ns := metav1.NamespaceDefault
	if namespace != nil {
		ns = *namespace
	}

	// perform auth
	if ns != "" && len(r.allowedNamespaces) > 0 && !slices.Contains(r.allowedNamespaces, ns) {
		return nil, ErrForbidden
	}

	// listify
	if ns == "" && len(r.allowedNamespaces) > 0 {
		namespaces = r.allowedNamespaces
	} else {
		namespaces = []string{ns}
	}

	return namespaces, nil
}

func NewResolver(cfg *rest.Config, nc *nats.Conn, gcm *grpchelpers.ConnectionManager, allowedNamespaces []string) (*Resolver, error) {
	// init resolver
	r := &Resolver{
		k8sCfg:            cfg,
		nc:                nc,
		gcm:               gcm,
		allowedNamespaces: allowedNamespaces,
	}

	return r, nil
}

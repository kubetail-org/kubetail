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
	"net/http"
	"slices"
	"time"

	zlog "github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/modules/common/k8shelpers"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	k8sCfg               *rest.Config
	clientset            *kubernetes.Clientset
	clientsetReadyCh     chan struct{}
	dynamicClient        *dynamic.DynamicClient
	dynamicClientReadyCh chan struct{}
	grpcDispatcher       *grpcdispatcher.Dispatcher
	allowedNamespaces    []string
	TestClientset        *fake.Clientset
	TestDynamicClient    *dynamicFake.FakeDynamicClient
}

func (r *Resolver) K8SClientset(ctx context.Context) kubernetes.Interface {
	if r.TestClientset != nil {
		return r.TestClientset
	}

	// If undefined, create new clientset
	if r.clientset == nil {
		cfg := rest.CopyConfig(r.k8sCfg)

		cfg.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
			return k8shelpers.NewBearerTokenRoundTripper(transport)
		}

		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			zlog.Fatal().Err(err).Send()
		}

		r.clientset = clientset
	}

	return r.clientset
}

func (r *Resolver) K8SDynamicClient(ctx context.Context) dynamic.Interface {
	if r.TestDynamicClient != nil {
		return r.TestDynamicClient
	}

	// If undefined, create new dynamic client
	if r.dynamicClient == nil {
		cfg := rest.CopyConfig(r.k8sCfg)

		cfg.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
			return k8shelpers.NewBearerTokenRoundTripper(transport)
		}

		dynamicClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			zlog.Fatal().Err(err).Send()
		}

		r.dynamicClient = dynamicClient
	}

	return r.dynamicClient
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

func (r *Resolver) WarmUp() {
	if r.k8sCfg == nil {
		return
	}

	// warm up clientset in background
	go func() {
		if r.TestClientset != nil {
			close(r.clientsetReadyCh)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		clientset := r.K8SClientset(ctx)
		clientset.Discovery().ServerVersion()
		close(r.clientsetReadyCh)
	}()

	// warm up dynamic client in background
	go func() {
		if r.TestDynamicClient != nil {
			close(r.dynamicClientReadyCh)
			return
		}

		namespaceGVR := schema.GroupVersionResource{
			Group:    "",   // Core API group
			Version:  "v1", // API version
			Resource: "namespaces",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		// Make a lightweight API call to list namespaces
		client := r.K8SDynamicClient(ctx)
		client.Resource(namespaceGVR).List(ctx, metav1.ListOptions{Limit: 1})
		close(r.dynamicClientReadyCh)
	}()
}

func NewResolver(cfg *rest.Config, grpcDispatcher *grpcdispatcher.Dispatcher, allowedNamespaces []string) (*Resolver, error) {
	// init resolver
	r := &Resolver{
		k8sCfg:               cfg,
		clientsetReadyCh:     make(chan struct{}),
		dynamicClientReadyCh: make(chan struct{}),
		grpcDispatcher:       grpcDispatcher,
		allowedNamespaces:    allowedNamespaces,
	}

	return r, nil
}

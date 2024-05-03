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
	//"os"

	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	k8sCfg            *rest.Config
	allowedNamespaces []string
	TestClientset     *fake.Clientset
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

func (r *Resolver) ToNamespace(namespace *string) string {
	// check configured namespace
	if len(r.allowedNamespaces) > 0 {
		if slices.Contains(r.allowedNamespaces, *namespace) {
			return *namespace
		} else {
			panic("xxx")
		}
	}

	// use default behavior
	ns := metav1.NamespaceDefault
	if namespace != nil {
		ns = *namespace
	}
	return ns
}

func NewResolver(cfg *rest.Config, allowedNamespaces []string) (*Resolver, error) {
	// try in-cluster config
	return &Resolver{k8sCfg: cfg, allowedNamespaces: allowedNamespaces}, nil
}

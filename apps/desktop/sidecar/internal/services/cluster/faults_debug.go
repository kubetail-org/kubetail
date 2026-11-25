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

//go:build debug

// KUBETAIL_FAULTS' store fault, a debug build's: the request faults live in kubeconn. A release
// build has the stub in faults_release.go.
package cluster

import (
	"errors"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
)

// syncStores is the store manager kubesync arms caches with: one that refuses every open when
// the rules hold store=fail, and mgr otherwise.
func syncStores(mgr *kubestore.Manager, faults kubeconn.Faults) interface {
	OpenOrCreate(cacheID int64) (*kubestore.Store, error)
} {
	if faults.StoreFails() {
		return failingStores{}
	}
	return mgr
}

// failingStores is a store manager whose every open fails.
type failingStores struct{}

func (failingStores) OpenOrCreate(int64) (*kubestore.Store, error) {
	return nil, errors.New("injected by KUBETAIL_FAULTS")
}

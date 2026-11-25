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

// Package kubelet reads a container's log as the kubelet serves it, through the
// API server: a claim on the cluster's connection, a forward or backward read of
// one log over it, the windows a backward read sizes, and the limits a read keeps
// within.
package kubelet

import "context"

// Kubelet is the Backend that reads each log as the kubelet serves it, through
// the API server, over get.
type Kubelet struct{ get Getter }

// New is the kubelet backend over get.
func New(get Getter) Backend { return Kubelet{get} }

func (k Kubelet) Forward(ctx context.Context, l Log, o Options) *Read {
	return readForward(ctx, k.get, l, o)
}

func (k Kubelet) Backward(ctx context.Context, l Log, o Options) *Read {
	return readBackward(ctx, k.get, l, o)
}

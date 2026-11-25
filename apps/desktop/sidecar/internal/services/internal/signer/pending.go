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

package signer

import (
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// Pending is a binding made before the restore landed. Each Sign waits for
// ready, then signs with what resolve answers. The request's context ends the
// wait with its error, and done ends it with llm.ErrSignedOut. done is the
// service's end: a restore its Close cancels never closes ready. resolve is
// called at each use, so it must answer the same binding every time.
func Pending(ready, done <-chan struct{}, resolve func() llm.Signer) llm.Signer {
	return pending{ready: ready, done: done, resolve: resolve}
}

type pending struct {
	ready, done <-chan struct{}
	resolve     func() llm.Signer
}

func (p pending) Sign(r *http.Request) error {
	select {
	case <-p.ready:
		return p.resolve().Sign(r)
	case <-r.Context().Done():
		return r.Context().Err()
	case <-p.done:
		return llm.ErrSignedOut
	}
}

// Refused reaches the resolved binding once ready; before, nothing signed.
func (p pending) Refused(r *http.Request) {
	select {
	case <-p.ready:
		p.resolve().Refused(r)
	default:
	}
}

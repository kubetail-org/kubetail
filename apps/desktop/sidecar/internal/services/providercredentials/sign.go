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

package providercredentials

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

// errOffBase is a request under none of the provider's base URLs.
var errOffBase = errors.New("providercredentials: the request is not for the provider's API")

// Signer is id's signer, nil for an unknown id.
func (s *Service) Signer(id string) llm.SessionSigner {
	p, ok := s.byID[id]
	if !ok {
		return nil
	}
	return providerSigner{s: s, p: p}
}

// providerSigner is a provider's signer: the connector while it has a
// session, else whichever key is current.
type providerSigner struct {
	s *Service
	p *provider
}

// Sign waits for the provider to leave Restoring, then signs with the highest
// credential present.
func (ps providerSigner) Sign(req *http.Request) error {
	if err := ps.awaitRestore(req.Context()); err != nil {
		return err
	}
	if c := ps.p.connector; c != nil {
		if err := c.Sign(req); !errors.Is(err, llm.ErrSignedOut) {
			return err
		}
	}
	return ps.current().Sign(req)
}

// awaitRestore waits for the provider to leave Restoring. ctx ends the wait
// with its error, and Close with llm.ErrSignedOut.
func (ps providerSigner) awaitRestore(ctx context.Context) error {
	select {
	case <-ps.p.restored:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-ps.s.ctx.Done():
		return llm.ErrSignedOut
	}
}

func (ps providerSigner) Refused(req *http.Request) {
	if c := ps.p.connector; c != nil {
		c.Refused(req)
	}
	ps.current().Refused(req)
}

// Bind is the connector's binding while it has a session, else one for the
// current key alone, which fails with llm.ErrCredentialChanged once the saved
// key changes or the connector starts a session. While Restoring nothing signs
// until both are known: a saved key must not sign and then be replaced by the
// connector a moment later.
func (ps providerSigner) Bind() llm.Signer {
	if ps.s.restoring(ps.p) {
		return ps.pending()
	}
	if c := ps.p.connector; c != nil {
		if b := c.Bind(); b != llm.Signer(signer.Ended{}) {
			return b
		}
	}
	b := ps.current()
	if b.key() == "" {
		return signer.Ended{}
	}
	return b
}

// pending is a binding made while Restoring. It resolves to the connector's
// binding when the connector had a session at the bind, else to the key's.
// Once the connector has restored, its binding says which, as in Bind. Before,
// its binding is a signer.Pending, so the landing says: never the current
// session, since one started after the bind is not the bind's.
func (ps providerSigner) pending() llm.Signer {
	var connector llm.Signer = signer.Ended{}
	connectorRestored := true
	if c := ps.p.connector; c != nil {
		select {
		case <-c.Restored():
		default:
			connectorRestored = false
		}
		connector = c.Bind()
	}
	key := ps.current()
	return signer.Pending(ps.p.restored, ps.s.ctx.Done(), func() llm.Signer {
		ps.s.mu.Lock()
		defer ps.s.mu.Unlock()
		hasSession := connector != llm.Signer(signer.Ended{})
		if !connectorRestored {
			hasSession = ps.p.landedGen != 0
		}
		if hasSession {
			return connector
		}
		return key
	})
}

// current is a binding of the provider's key as it is now.
func (ps providerSigner) current() keyBinding {
	ps.s.mu.Lock()
	defer ps.s.mu.Unlock()
	return keyBinding{s: ps.s, p: ps.p, gen: ps.p.keyGen}
}

// keyBinding signs with a provider's key under one generation of its keys.
type keyBinding struct {
	s   *Service
	p   *provider
	gen uint64
}

// key is the key that signs: the saved key, else the environment's; "" with
// neither, and once the generation has moved.
func (k keyBinding) key() string {
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	return k.keyLocked()
}

func (k keyBinding) keyLocked() string {
	if k.gen != k.p.keyGen {
		return ""
	}
	return cmp.Or(k.p.saved, k.p.env)
}

// Sign sets the key in the header of every route whose base covers req.
func (k keyBinding) Sign(req *http.Request) error {
	k.s.mu.Lock()
	changed, key := k.gen != k.p.keyGen, k.keyLocked()
	k.s.mu.Unlock()
	switch {
	case changed:
		return llm.ErrCredentialChanged
	case key == "":
		return llm.ErrSignedOut
	}
	routes := k.p.covering(req)
	if len(routes) == 0 {
		return errOffBase
	}
	for _, rt := range routes {
		req.Header.Set(rt.Header, rt.Prefix+key)
	}
	return nil
}

// Refused marks the key rejected when req carried it. One 401 marks it; the
// key is kept and still signs.
func (k keyBinding) Refused(req *http.Request) {
	routes := k.p.covering(req)
	if len(routes) == 0 {
		return
	}
	sent := strings.TrimPrefix(req.Header.Get(routes[0].Header), routes[0].Prefix)
	k.s.mu.Lock()
	defer k.s.mu.Unlock()
	if key := k.keyLocked(); key == "" || sent != key || k.p.rejected {
		return
	}
	k.p.rejected = true
	k.s.publishLocked(k.p)
}

// covering is every route whose base covers req.
func (p *provider) covering(req *http.Request) []Route {
	var routes []Route
	for i, rt := range p.Routes {
		if signer.Covers(p.bases[i], req.URL) {
			routes = append(routes, rt)
		}
	}
	return routes
}

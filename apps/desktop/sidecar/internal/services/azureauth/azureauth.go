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

// Package azureauth is the user's Azure login, read through the user's own az
// outside the sandbox: the account it is signed into, and a Resource Manager
// token per tenant, held in memory. Nothing from a sandbox, a model or a
// cluster reaches az's arguments.
package azureauth

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
)

// Runner runs the user's az with args and answers its standard output.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Service is the user's Azure login. Discovery runs when a run first asks for
// Service is the user's Azure login. Discovery runs when a run first asks for
// the account, never at startup, and again when the user's files change.
type Service struct {
	run   Runner
	stamp func() string // the user's files' mtimes; changes when any does
	now   func() time.Time

	mu         sync.Mutex
	discovered bool
	seen       string // the stamp the last discovery read under
	account    azureproxy.Account
	ok         bool
	// login counts discoveries: a token belongs to the login it was asked
	// under, and each discovery drops every held token, since the user may
	// have signed in as someone else.
	login int
	// discovering is closed when the one discovery running ends; nil with
	// none.
	discovering chan struct{}

	tokens tokens
}

func newService(run Runner, stamp func() string, now func() time.Time) *Service {
	return &Service{run: run, stamp: stamp, now: now, tokens: newTokens()}
}

// Account is the account as last discovered. The first call discovers it,
// and so does a call after the user's files change: one with no account
// waits for the discovery until ctx ends, one with an account answers that
// and lets the discovery run.
func (s *Service) Account(ctx context.Context) (azureproxy.Account, bool) {
	stamp := s.stamp()
	s.mu.Lock()
	if s.discovered && s.seen == stamp {
		defer s.mu.Unlock()
		return s.account, s.ok
	}
	done := s.discover(stamp)
	acct, ok := s.account, s.ok
	s.mu.Unlock()
	if ok {
		return acct, ok
	}
	select {
	case <-done:
	case <-ctx.Done():
		return azureproxy.Account{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account, s.ok
}

// discover starts a discovery under stamp, unless one runs, and answers the
// channel closed when the one running ends. It runs apart from any run's ctx,
// each az under its own bound, so a run that gives up leaves it running for
// the next. Its caller holds mu.
func (s *Service) discover(stamp string) <-chan struct{} {
	if s.discovering != nil {
		return s.discovering
	}
	done := make(chan struct{})
	s.discovering = done
	go func() {
		s.discoverUnder(context.Background(), stamp)
		s.mu.Lock()
		s.discovering = nil
		s.mu.Unlock()
		close(done)
	}()
	return done
}

// discovery is closed once the discovery running ends, or at once with none.
func (s *Service) discovery() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discovering != nil {
		return s.discovering
	}
	done := make(chan struct{})
	close(done)
	return done
}

// discoverUnder runs one discovery and keeps its result, and drops every
// held token. One whose az did not run to an answer keeps nothing, so the
// next mount discovers again.
func (s *Service) discoverUnder(ctx context.Context, stamp string) {
	acct, ok, err := s.read(ctx)
	if errors.Is(err, ErrNotRun) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discovered, s.seen, s.account, s.ok = true, stamp, acct, ok
	s.login++
	s.tokens.clear()
}

// minAz is the first az whose managed identity asks IDENTITY_ENDPOINT. One
// before it asks IMDS, which with network on an Azure VM answers the VM's own
// identity, past the proxy.
var minAz = [3]int{2, 73, 0}

// read runs az for the account, and answers az's error when it gave no
// output. An az before minAz is no login. Every command reads local files
// only.
func (s *Service) read(ctx context.Context) (azureproxy.Account, bool, error) {
	out, err := s.run(ctx, "version", "--output", "json")
	if err != nil {
		return azureproxy.Account{}, false, err
	}
	var version struct {
		CLI string `json:"azure-cli"`
	}
	if json.Unmarshal(out, &version) != nil || !atLeast(version.CLI, minAz) {
		return azureproxy.Account{}, false, nil
	}
	out, err = s.run(ctx, "account", "list", "--output", "json")
	if err != nil {
		return azureproxy.Account{}, false, err
	}
	var subs []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		TenantID  string `json:"tenantId"`
		IsDefault bool   `json:"isDefault"`
		State     string `json:"state"`
		CloudName string `json:"cloudName"`
	}
	if json.Unmarshal(out, &subs) != nil {
		return azureproxy.Account{}, false, nil
	}
	out, err = s.run(ctx, "cloud", "show", "--output", "json")
	if err != nil {
		return azureproxy.Account{}, false, err
	}
	var cloud struct {
		Name      string             `json:"name"`
		Endpoints map[string]*string `json:"endpoints"`
		Suffixes  map[string]*string `json:"suffixes"`
	}
	if json.Unmarshal(out, &cloud) != nil {
		return azureproxy.Account{}, false, nil
	}
	if _, known := azureproxy.Clouds[cloud.Name]; !known {
		return azureproxy.Account{}, false, nil
	}
	acct := azureproxy.Account{Cloud: azureproxy.Cloud{
		Name: cloud.Name, Endpoints: present(cloud.Endpoints), Suffixes: present(cloud.Suffixes),
	}}
	for _, sub := range subs {
		if sub.State != "Enabled" || sub.CloudName != cloud.Name {
			continue
		}
		if !guid.MatchString(sub.ID) || !guid.MatchString(sub.TenantID) {
			return azureproxy.Account{}, false, nil
		}
		acct.Subscriptions = append(acct.Subscriptions, azureproxy.Subscription{
			ID: sub.ID, Name: sub.Name, TenantID: sub.TenantID, IsDefault: sub.IsDefault,
		})
	}
	if len(acct.Subscriptions) == 0 {
		return azureproxy.Account{}, false, nil
	}
	return acct, true, nil
}

// atLeast is whether version, major.minor.patch, is min or later.
func atLeast(version string, min [3]int) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		v[i] = n
	}
	return slices.Compare(v[:], min[:]) >= 0
}

// present is m with its null values left out.
func present(m map[string]*string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

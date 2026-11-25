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

package azureauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/azureproxy"
)

const (
	sub1    = "00000000-0000-0000-0000-000000000001"
	sub2    = "00000000-0000-0000-0000-000000000002"
	tenantA = "0000000a-0000-0000-0000-000000000000"
	tenantB = "0000000b-0000-0000-0000-000000000000"
)

const accountList = `[
  {"id": "` + sub1 + `", "name": "Pay-As-You-Go", "tenantId": "` + tenantA + `", "isDefault": true, "state": "Enabled", "cloudName": "AzureCloud"},
  {"id": "` + sub2 + `", "name": "Dev", "tenantId": "` + tenantB + `", "isDefault": false, "state": "Enabled", "cloudName": "AzureCloud"},
  {"id": "00000000-0000-0000-0000-000000000003", "name": "Off", "tenantId": "` + tenantA + `", "isDefault": false, "state": "Disabled", "cloudName": "AzureCloud"},
  {"id": "00000000-0000-0000-0000-000000000004", "name": "Gov", "tenantId": "` + tenantA + `", "isDefault": false, "state": "Enabled", "cloudName": "AzureUSGovernment"}
]`

const cloudShow = `{"name": "AzureCloud", "isActive": true,
  "endpoints": {"resourceManager": "https://management.azure.com/", "activeDirectoryResourceId": "https://management.core.windows.net/", "batchResourceId": null},
  "suffixes": {"storageEndpoint": "core.windows.net"}}`

// fakeAz answers az's arguments from a table, counting each call. A call
// with no answer fails.
type fakeAz struct {
	mu      sync.Mutex
	answers map[string]string
	fail    map[string]error
	calls   []string
	// gate, when set, holds each call until the test sends on it.
	gate chan struct{}
	// before runs a line's hook before it is answered, as az would run while
	// something else happens.
	before map[string]func()
}

func newFakeAz() *fakeAz {
	return &fakeAz{answers: map[string]string{
		"version --output json":      `{"azure-cli": "2.93.0", "azure-cli-core": "2.93.0", "extensions": {}}`,
		"account list --output json": accountList,
		"cloud show --output json":   cloudShow,
	}, fail: map[string]error{}, before: map[string]func(){}}
}

func (f *fakeAz) run(ctx context.Context, args ...string) ([]byte, error) {
	line := strings.Join(args, " ")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, line)
	gate, before := f.gate, f.before[line]
	f.mu.Unlock()
	if before != nil {
		before()
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[line]; err != nil {
		return nil, err
	}
	answer, ok := f.answers[line]
	if !ok {
		return nil, errors.New("exit status 1")
	}
	return []byte(answer), nil
}

func (f *fakeAz) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeAz) setGate(gate chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = gate
}

func (f *fakeAz) set(line, answer string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[line] = answer
}

// profile is the user's files' stamp as a test changes it.
type profile struct {
	mu    sync.Mutex
	stamp string
}

func (p *profile) get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stamp
}

func (p *profile) set(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stamp = s
}

func newTestService(az *fakeAz, p *profile) *Service {
	return newService(az.run, p.get, time.Now)
}

// Discovery keeps the active cloud's enabled subscriptions, and the cloud's
// name and tables.
func TestDiscoveryKeepsTheActiveCloudsSubscriptions(t *testing.T) {
	s := newTestService(newFakeAz(), &profile{stamp: "1"})
	acct, ok := s.Account(t.Context())
	require.True(t, ok)
	assert.Equal(t, []azureproxy.Subscription{
		{ID: sub1, Name: "Pay-As-You-Go", TenantID: tenantA, IsDefault: true},
		{ID: sub2, Name: "Dev", TenantID: tenantB},
	}, acct.Subscriptions)
	assert.Equal(t, "AzureCloud", acct.Cloud.Name)
	assert.Equal(t, map[string]string{
		"resourceManager":           "https://management.azure.com/",
		"activeDirectoryResourceId": "https://management.core.windows.net/",
	}, acct.Cloud.Endpoints)
	assert.Equal(t, map[string]string{"storageEndpoint": "core.windows.net"}, acct.Cloud.Suffixes)
}

func TestAnAccountWithABadIdOrAnUnknownCloudIsNone(t *testing.T) {
	for name, edit := range map[string]func(*fakeAz){
		"a subscription id that is not a GUID": func(az *fakeAz) {
			az.set("account list --output json", strings.Replace(accountList, sub2, "../../x", 1))
		},
		"a tenant id that is not a GUID": func(az *fakeAz) {
			az.set("account list --output json", strings.Replace(accountList, tenantB, "--tenant", 1))
		},
		"a cloud not in the table": func(az *fakeAz) {
			az.set("cloud show --output json", strings.Replace(cloudShow, "AzureCloud", "AzureStack", 1))
		},
		"no subscription":         func(az *fakeAz) { az.set("account list --output json", "[]") },
		"no login":                func(az *fakeAz) { delete(az.answers, "account list --output json") },
		"no az version":           func(az *fakeAz) { delete(az.answers, "version --output json") },
		"not JSON":                func(az *fakeAz) { az.set("cloud show --output json", "AzureCloud") },
		"a list that is not JSON": func(az *fakeAz) { az.set("account list --output json", "[") },
		"no cloud":                func(az *fakeAz) { delete(az.answers, "cloud show --output json") },
	} {
		az := newFakeAz()
		edit(az)
		_, ok := newTestService(az, &profile{stamp: "1"}).Account(t.Context())
		assert.False(t, ok, name)
	}
}

// Nothing runs az until a run mounts and asks for the account.
func TestDiscoveryWaitsForAMount(t *testing.T) {
	az := newFakeAz()
	s := newTestService(az, &profile{stamp: "1"})
	assert.Zero(t, az.count(""))
	_, ok := s.Account(t.Context())
	assert.True(t, ok)
	assert.Equal(t, 3, az.count(""))
	_, _ = s.Account(t.Context())
	assert.Equal(t, 3, az.count(""), "an unchanged profile is not read again")
}

// A failed discovery is remembered until the user's files change; a run with
// no account then waits for the new one.
func TestAFailedDiscoveryIsNotRepeatedUntilTheProfileChanges(t *testing.T) {
	az := newFakeAz()
	delete(az.answers, "account list --output json")
	p := &profile{stamp: ""}
	s := newTestService(az, p)
	_, ok := s.Account(t.Context())
	require.False(t, ok)
	calls := az.count("")
	_, ok = s.Account(t.Context())
	assert.False(t, ok)
	assert.Equal(t, calls, az.count(""), "a second mount runs no az")

	az.set("account list --output json", accountList)
	p.set("appeared")
	_, ok = s.Account(t.Context())
	assert.True(t, ok, "the mount after the profile appears waits for the new discovery")
}

// A run that has an account does not wait for a rediscovery, which runs in
// the background and is seen by a later run.
func TestARunWithAnAccountDoesNotWaitForARediscovery(t *testing.T) {
	az := newFakeAz()
	p := &profile{stamp: "1"}
	s := newTestService(az, p)
	_, ok := s.Account(t.Context())
	require.True(t, ok)

	gate := make(chan struct{})
	az.setGate(gate)
	az.set("account list --output json", strings.Replace(accountList, "Pay-As-You-Go", "Renamed", 1))
	p.set("2")
	acct, ok := s.Account(t.Context())
	require.True(t, ok)
	assert.Equal(t, "Pay-As-You-Go", acct.Subscriptions[0].Name, "the old account, unwaited")

	gate <- struct{}{}
	gate <- struct{}{}
	gate <- struct{}{}
	testutil.Wait(t, s.discovery(), "the rediscovery")
	acct, _ = s.Account(t.Context())
	assert.Equal(t, "Renamed", acct.Subscriptions[0].Name)
}

// discovering is whether a discovery is running.
func (s *Service) isDiscovering() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.discovering != nil
}

// Two runs that mount at once share one discovery and get its account.
func TestRunsMountingAtOnceShareADiscovery(t *testing.T) {
	az := newFakeAz()
	gate := make(chan struct{})
	az.setGate(gate)
	s := newTestService(az, &profile{stamp: "1"})
	done := make(chan bool, 2)
	mount := func() {
		_, ok := s.Account(t.Context())
		done <- ok
	}
	go mount()
	require.Eventually(t, s.isDiscovering, testutil.Timeout, time.Millisecond)
	go mount()
	close(gate)
	assert.True(t, testutil.Recv(t, done, "the first mount"))
	assert.True(t, testutil.Recv(t, done, "the second mount"))
	assert.Equal(t, 3, az.count(""), "one discovery: three az calls")
}

// A mount that gives up stops waiting at once, and the discovery it started
// runs on: the next mount finds its account.
func TestAMountThatGivesUpLeavesTheDiscoveryRunning(t *testing.T) {
	az := newFakeAz()
	gate := make(chan struct{})
	az.setGate(gate)
	s := newTestService(az, &profile{stamp: "1"})
	ctx, cancel := context.WithCancel(t.Context())
	gaveUp := make(chan bool)
	go func() {
		_, ok := s.Account(ctx)
		gaveUp <- ok
	}()
	require.Eventually(t, s.isDiscovering, testutil.Timeout, time.Millisecond)
	cancel()
	assert.False(t, testutil.Recv(t, gaveUp, "the mount that gave up"))

	close(gate)
	testutil.Wait(t, s.discovery(), "the discovery")
	_, ok := s.Account(t.Context())
	assert.True(t, ok)
	assert.Equal(t, 3, az.count(""), "the discovery ran once")
}

// A rediscovery already running is not started again, and with none running
// there is nothing to wait for.
func TestOneRediscoveryRunsAtATime(t *testing.T) {
	az := newFakeAz()
	p := &profile{stamp: "1"}
	s := newTestService(az, p)
	testutil.Wait(t, s.discovery(), "no rediscovery")
	_, ok := s.Account(t.Context())
	require.True(t, ok)

	gate := make(chan struct{})
	az.setGate(gate)
	p.set("2")
	_, _ = s.Account(t.Context())
	_, _ = s.Account(t.Context())
	gate <- struct{}{}
	gate <- struct{}{}
	gate <- struct{}{}
	testutil.Wait(t, s.discovery(), "the rediscovery")
	assert.Equal(t, 6, az.count(""), "one rediscovery: three more az calls")
}

// A discovery whose context ended keeps nothing: the next mount discovers
// again and finds the account.
func TestACancelledDiscoveryIsNotRemembered(t *testing.T) {
	az := newFakeAz()
	s := newTestService(az, &profile{stamp: "1"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok := s.Account(ctx)
	require.False(t, ok)

	_, ok = s.Account(t.Context())
	assert.True(t, ok)
}

// An az that did not run to an answer, missing or past its bound, is not
// remembered: the next mount discovers again, as after the user installs az
// or fixes PATH.
func TestADiscoveryAzDidNotAnswerIsNotRemembered(t *testing.T) {
	az := newFakeAz()
	az.fail["account list --output json"] = fmt.Errorf("%w: az not found", ErrNotRun)
	s := newTestService(az, &profile{stamp: "1"})
	_, ok := s.Account(t.Context())
	require.False(t, ok)

	delete(az.fail, "account list --output json")
	_, ok = s.Account(t.Context())
	assert.True(t, ok)
}

// An az before 2.73 asks IMDS for a managed identity's token, never the run's
// identity endpoint, so it counts as no login: with network on an Azure VM it
// would take the VM's identity past the proxy.
func TestAnAzBeforeManagedIdentityOverMSALIsNoLogin(t *testing.T) {
	for version, want := range map[string]bool{
		"2.72.0": false, "2.9.0": false, "1.99.0": false, "garbled": false, "2.x.0": false,
		"2.73.0": true, "2.100.1": true, "3.0.0": true,
	} {
		az := newFakeAz()
		az.set("version --output json", `{"azure-cli": "`+version+`"}`)
		_, ok := newTestService(az, &profile{stamp: "1"}).Account(t.Context())
		assert.Equal(t, want, ok, version)
	}
}

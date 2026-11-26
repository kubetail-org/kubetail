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

package bash

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/envvars"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// tokenHeader is where the fake cloud platform's client carries its grant's
// token.
const tokenHeader = "X-Kubetail-Token"

// fakePlatform is a CloudPlatform whose routes are hosts. A request holding
// its grant's token is a read when it is a GET, answered "read <target>", and
// otherwise a change put through Decide, answered "changed" once forwarded.
type fakePlatform struct {
	target permissions.Target
	hosts  []string
	tls    bool // its routes are TLS
	env    []string
	// exports adds FAKE_TOKEN, the grant's token, FAKE_CA, the run's CAFile,
	// and FAKE_DOOR, the run's loopback authority, to env.
	exports bool
	// atDoor routes the run's own loopback authority in place of hosts: on
	// macOS its port is the run's.
	atDoor bool
	// onMount, when set, runs with each run before the mount is made.
	onMount func(CloudRun)
	err     error
	// mounts gets each run's CloudRun and the grant made for it.
	mounts chan fakeMount
}

type fakeMount struct {
	run   CloudRun
	grant *runproxy.Grant
}

func newFakePlatform(target permissions.Target, hosts ...string) *fakePlatform {
	return &fakePlatform{target: target, hosts: hosts, mounts: make(chan fakeMount, 8)}
}

func (p *fakePlatform) Target() permissions.Target { return p.target }

func (p *fakePlatform) Mount(_ context.Context, run CloudRun) (CloudMount, error) {
	if p.err != nil {
		return CloudMount{}, p.err
	}
	if p.onMount != nil {
		p.onMount(run)
	}
	g := runproxy.NewGrant(p.target, run.On, run.Session, run.Asker, run.Refusal, cloudLimits)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, done, ok := g.Admit(w, r, r.Header.Get(tokenHeader))
		if !ok {
			return
		}
		defer done()
		if r.Method == http.MethodGet {
			fmt.Fprint(w, "read "+string(p.target))
			return
		}
		out := g.Decide(r.Context(), runproxy.Request{
			Action: permissions.Action{AccessLevel: permissions.Write, Scope: "account/place", AccountName: "account", Service: "svc", Operation: "change", Resource: "thing", Summary: "change " + string(p.target)},
			Sent:   runproxy.Sent{Host: "upstream.example", Method: r.Method, Path: r.URL.RequestURI()},
		}, func() error {
			_, err := fmt.Fprint(w, "changed")
			return err
		})
		if !out.Forwarded {
			http.Error(w, out.Message, out.Status)
		}
	})
	door := "127.0.0.1:" + strconv.Itoa(run.Port)
	hosts := p.hosts
	if p.atDoor {
		hosts = []string{door}
	}
	routes := make([]Route, 0, len(hosts))
	for _, h := range hosts {
		routes = append(routes, Route{Host: h, TLS: p.tls, Handler: handler})
	}
	env := p.env
	if p.exports {
		env = append(slices.Clone(env), "FAKE_TOKEN="+g.Token(), "FAKE_CA="+run.CAFile, "FAKE_DOOR="+door)
	}
	p.mounts <- fakeMount{run, g}
	return CloudMount{Routes: routes, Env: env, Grant: g}, nil
}

// askSession is a chat's, asking for every change, as a fresh Settings does.
var askSession = session.Session{
	Kind: session.Chat,
	Policy: func(context.Context, permissions.Target, string) permissions.Policy {
		return permissions.Policy{Mode: permissions.ModeAsk}
	},
}

// switchedOn is a cloud platform's switch that is always on.
func switchedOn(context.Context) bool { return true }

// switches is each cloud platform's switch as a test flips it; err fails
// every read.
type switches struct {
	mu  sync.Mutex
	on  map[permissions.Target]bool
	err error
}

func switchesOn(targets ...permissions.Target) *switches {
	s := &switches{on: map[permissions.Target]bool{}}
	for _, t := range targets {
		s.on[t] = true
	}
	return s
}

// read is every switch, as Cloud.Switches reads them.
func (s *switches) read(context.Context) (settings.CloudPlatformSwitches, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return settings.CloudPlatformSwitches{}, s.err
	}
	on := s.on
	return settings.CloudPlatformSwitches{AWS: on[permissions.AWS], GitHub: on[permissions.GitHub], GCP: on[permissions.GCP], Azure: on[permissions.Azure]}, nil
}

// fail makes every read fail with err.
func (s *switches) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *switches) set(t permissions.Target, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on[t] = on
}

// answeringAsker hands each Ask to the test and waits for its answer.
type answeringAsker struct {
	asked    chan tools.ActionRequest
	answers  chan bool
	recorded atomic.Int32
}

func newAnsweringAsker() *answeringAsker {
	return &answeringAsker{asked: make(chan tools.ActionRequest, 8), answers: make(chan bool, 8)}
}

func (a *answeringAsker) Ask(ctx context.Context, r tools.ActionRequest) (tools.Answer, error) {
	a.asked <- r
	select {
	case ok := <-a.answers:
		return tools.Answer{Approved: ok, Duration: permissions.DurationOnce}, nil
	case <-ctx.Done():
		return tools.Answer{}, ctx.Err()
	}
}

func (a *answeringAsker) Record(context.Context, tools.ActionRequest, permissions.Decision, string) error {
	a.recorded.Add(1)
	return nil
}

// mountFor mounts p for a foreground chat run and answers its routes and
// grant.
func mountFor(t *testing.T, p *fakePlatform) CloudMount {
	t.Helper()
	return mountSwitched(t, p, switchesOn(p.target))
}

// mountSwitched is p mounted for a chat's run, its grant reading sw.
func mountSwitched(t *testing.T, p *fakePlatform, sw *switches) CloudMount {
	t.Helper()
	m, err := p.Mount(t.Context(), CloudRun{Session: session.Session{Kind: session.Chat}, Refusal: "kubetail: no", On: Cloud{Switches: sw.read}.switchOf(p.target)})
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	return m
}

func TestTwoCloudPlatformsCannotSetOneVariableTwoWays(t *testing.T) {
	_, err := cloudEnv([]CloudMount{{Env: []string{"A=1", "B=2"}}, {Env: []string{"B=2", "C=3"}}})
	assert.NoError(t, err, "one value set twice agrees")
	_, err = cloudEnv([]CloudMount{{Env: []string{"A=1"}}, {Env: []string{"A=2"}}})
	assert.ErrorContains(t, err, "A")
}

func TestAProxyVariableBringsNoProxy(t *testing.T) {
	env, err := cloudEnv([]CloudMount{{Env: []string{"GH_HOST=github.localhost"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"GH_HOST=github.localhost"}, env)

	env, err = cloudEnv([]CloudMount{{Env: []string{"HTTPS_PROXY=http://127.0.0.1:6443"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"HTTPS_PROXY=http://127.0.0.1:6443", "NO_PROXY=127.0.0.1", "no_proxy=127.0.0.1"}, env)
	var proxies []string
	for name := range proxyVars {
		proxies = append(proxies, name+"=")
	}
	assertNoUserCanSet(t, append(proxies, env...))

	_, err = cloudEnv([]CloudMount{{Env: []string{"HTTPS_PROXY=http://127.0.0.1:6443", "NO_PROXY=example.com"}}})
	assert.ErrorContains(t, err, "NO_PROXY", "a cloud platform's own NO_PROXY clashes with the loopback's")
}

// cloudAsker hands the runtime's asker every field of what was sent, as the
// request's cloud half, and the action as decided.
func TestACloudRequestReachesTheAskerWhole(t *testing.T) {
	a := newAnsweringAsker()
	a.answers <- true
	req := runproxy.Request{
		Action:    permissions.Action{Target: permissions.AWS, AccessLevel: permissions.Write, Scope: "123456789012/eu-west-1", Service: "ec2", Operation: "X", Summary: "AWS: call ec2:X"},
		Grantable: true, CommandRule: "Allow X of ec2 \"\" in AWS 123456789012 / eu-west-1 for this command", ChatRule: "Allow writes in AWS 123456789012 / eu-west-1",
		Sent: runproxy.Sent{
			Host: "ec2.amazonaws.com", Method: "POST", Path: "/?a=1", ContentType: "text/plain",
			Body: []byte("Action=X"), Unshown: "", Details: []runproxy.Detail{{Label: "Region", Value: "eu-west-1"}},
		},
	}
	answer, err := cloudAsker{a}.Ask(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, permissions.Answer{Approved: true, Duration: permissions.DurationOnce}, answer, "the answer carries its duration")
	got := testutil.Recv(t, a.asked, "the request")
	assert.Equal(t, tools.ActionRequest{
		Action: req.Action, Grantable: true, CommandRule: req.CommandRule, ChatRule: req.ChatRule,
		Cloud: &tools.CloudRequest{
			Host: "ec2.amazonaws.com", Method: "POST", Path: "/?a=1", ContentType: "text/plain",
			Body: "Action=X", Details: []tools.Detail{{Label: "Region", Value: "eu-west-1"}},
		},
	}, got)

	require.NoError(t, cloudAsker{a}.Record(t.Context(), req, permissions.Denied, "no"))
	assert.Equal(t, int32(1), a.recorded.Load())
}

// A grant's requests go to the runtime's asker for a foreground call, and are
// refused naming the cloud platform for a background command or no asker.
func TestCloudAskerFor(t *testing.T) {
	a := newAnsweringAsker()
	asker, refusal := cloudAskerFor(tools.Runtime{ActionAsker: a}, false, permissions.GCP)
	assert.Equal(t, cloudAsker{a}, asker)
	assert.Empty(t, refusal)

	asker, refusal = cloudAskerFor(tools.Runtime{ActionAsker: a}, true, permissions.GCP)
	assert.Nil(t, asker)
	assert.Equal(t, "kubetail: a background command cannot change Google Cloud. Run it in the foreground.", refusal)

	asker, refusal = cloudAskerFor(tools.Runtime{}, false, permissions.AWS)
	assert.Nil(t, asker)
	assert.Equal(t, "kubetail: this sandbox reads AWS and changes nothing.", refusal)
}

// grantlessPlatform mounts with no grant, a wiring mistake.
type grantlessPlatform struct{}

func (grantlessPlatform) Target() permissions.Target { return permissions.AWS }

func (grantlessPlatform) Mount(context.Context, CloudRun) (CloudMount, error) {
	return CloudMount{Env: []string{"A=1"}}, nil
}

// A mount with no grant fails the run, and keeps nothing the run's end would
// reach.
func TestAMountWithNoGrantFailsTheRun(t *testing.T) {
	p := &runProxy{}
	_, err := p.mount(t.Context(), Cloud{}, []CloudPlatform{grantlessPlatform{}}, tools.Runtime{}, false, CloudRun{})
	require.EqualError(t, err, "the AWS mount names no grant")
	assert.Empty(t, p.mounts)
	p.end()
}

// assertNoUserCanSet checks that no variable of the user's for a sandboxed
// command can name anything env sets, so a mount's variables stay Kubetail's.
func assertNoUserCanSet(t *testing.T, env []string) {
	t.Helper()
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		assert.Error(t, envvars.Check(name, "", envvars.Sandbox), "the user could set %s", name)
	}
}

// durationAsker answers every Ask approved for the command.
type durationAsker struct{ *answeringAsker }

func (durationAsker) Ask(context.Context, tools.ActionRequest) (tools.Answer, error) {
	return tools.Answer{Approved: true, Duration: permissions.DurationCommand}, nil
}

// The user's duration reaches the grant, so a command answer adds its rule.
func TestACloudAnswerCarriesItsDuration(t *testing.T) {
	answer, err := cloudAsker{durationAsker{newAnsweringAsker()}}.Ask(t.Context(), runproxy.Request{})
	require.NoError(t, err)
	assert.Equal(t, permissions.Answer{Approved: true, Duration: permissions.DurationCommand}, answer)
}

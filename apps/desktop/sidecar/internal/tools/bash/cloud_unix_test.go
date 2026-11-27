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

//go:build !windows

package bash

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/gcpauth"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// cloudTool is a tool on a fake sandbox whose build has cloud platforms, each
// switched as sw says.
func cloudTool(t *testing.T, sw *switches, platforms ...CloudPlatform) *Tool {
	t.Helper()
	tl := tool(t)
	tl.sandboxer = &fakeSandboxer{}
	tl.cloud = Cloud{Platforms: platforms, Switches: sw.read}
	return tl
}

// chatRuntime is a fresh chat with no cluster, of kind, asking through asker.
func chatRuntime(t *testing.T, kind session.Kind, asker tools.ActionAsker) tools.Runtime {
	return tools.Runtime{Dir: testChatDir(t), Session: session.Session{Kind: kind, Policy: askSession.Policy}, ActionAsker: asker}
}

// startRun makes a sandboxed run of rt, ended when the test ends.
func startRun(t *testing.T, tl *Tool, rt tools.Runtime, background bool) *sandboxedRun {
	t.Helper()
	r, err := tl.sandboxedRunFor(t.Context(), tl.sandboxer, rt, tools.WorkspacePath(rt.Dir), background, session.NoNetwork)
	require.NoError(t, err)
	t.Cleanup(r.end)
	return r
}

// overSocket is a client whose every request reaches the run's socket.
func overSocket(r *sandboxedRun) *http.Client {
	socket := r.dir.socket()
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
}

// send sends method to host on the run's socket carrying token, and answers
// the status and body.
func send(t *testing.T, r *sandboxedRun, method, host, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, "http://"+host+"/x", nil)
	require.NoError(t, err)
	req.Header.Set(tokenHeader, token)
	resp, err := overSocket(r).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestARunWithACloudPlatformAndNoClusterHasARelay(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	tl := cloudTool(t, switchesOn(permissions.AWS), p)
	r := startRun(t, tl, chatRuntime(t, session.Chat, nil), false)

	assert.Equal(t, []sandbox.Relay{{Port: 6443, Socket: r.dir.socket()}}, r.run.Policy.Network.Relays)
	m := testutil.Recv(t, p.mounts, "the mount")
	assert.Equal(t, 6443, m.run.Port)
	assert.Equal(t, r.dir.path, m.run.Dir)
	assert.Equal(t, r.dir.tmp, m.run.TmpDir)
	code, body := send(t, r, http.MethodGet, "127.0.0.1:6443", m.grant.Token())
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read aws", body)
	assert.NotContains(t, strings.Join(r.run.Env, "\n"), "KUBECONFIG=")
}

func TestARunWithNeitherHasNoRelay(t *testing.T) {
	t.Parallel()

	boxer := &fakeSandboxer{}
	tl := cloudTool(t, switchesOn())
	tl.sandboxer = boxer
	r := startRun(t, tl, chatRuntime(t, session.Chat, nil), false)
	assert.Empty(t, r.run.Policy.Network.Relays)
	assert.Zero(t, boxer.portsAsked())
	assert.Nil(t, r.proxy)
}

// A cloud platform whose switch is off at the run's start, a monitor's run and
// the probe's get no grant at all.
func TestACloudPlatformThatIsOffHasNoRoute(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	r := startRun(t, cloudTool(t, switchesOn(), p), chatRuntime(t, session.Chat, nil), false)
	assert.Empty(t, p.mounts)
	assert.Empty(t, r.run.Policy.Network.Relays)
}

// A run whose switches cannot be read mounts no cloud platform.
func TestARunThatCannotReadTheSwitchesHasNoPlatform(t *testing.T) {
	t.Parallel()

	sw := switchesOn(permissions.AWS)
	sw.fail(errors.New("the database is locked"))
	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	r := startRun(t, cloudTool(t, sw, p), chatRuntime(t, session.Chat, nil), false)
	assert.Empty(t, p.mounts)
	assert.Empty(t, r.run.Policy.Network.Relays)
}

// A run's grant reads the switch live: once it cannot be read, the route is
// refused.
func TestASwitchThatCannotBeReadMidRunIsOff(t *testing.T) {
	t.Parallel()

	sw := switchesOn(permissions.AWS)
	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	r := startRun(t, cloudTool(t, sw, p), chatRuntime(t, session.Chat, nil), false)
	token := testutil.Recv(t, p.mounts, "the mount").grant.Token()
	sw.fail(errors.New("the database is locked"))
	code, _ := send(t, r, http.MethodGet, "127.0.0.1:6443", token)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestAMonitorHasNoPlatform(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	startRun(t, cloudTool(t, switchesOn(permissions.AWS), p), chatRuntime(t, session.Monitor, nil), false)
	assert.Empty(t, p.mounts)
}

func TestTheProbeHasNoPlatform(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	tl := cloudTool(t, switchesOn(permissions.AWS), p)
	// The probe's runtime names no kind: no turn of the user's started it.
	startRun(t, tl, tools.Runtime{Dir: testChatDir(t)}, false)
	assert.Empty(t, p.mounts)
}

// A subagent's run reaches a cloud platform as its parent's would; a background
// command's grant has no asker and refuses every change naming the cloud
// platform.
func TestABackgroundCommandsGrantHasNoAsker(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid")
	tl := cloudTool(t, switchesOn(permissions.GCP), p)
	a := newAnsweringAsker()

	startRun(t, tl, chatRuntime(t, session.Subagent, a), false)
	assert.Equal(t, cloudAsker{a}, testutil.Recv(t, p.mounts, "the subagent's mount").run.Asker)

	r := startRun(t, tl, chatRuntime(t, session.Chat, a), true)
	m := testutil.Recv(t, p.mounts, "the background mount")
	assert.Nil(t, m.run.Asker)
	code, body := send(t, r, http.MethodPost, "storage.gcp.kubetail.invalid", m.grant.Token())
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, "a background command cannot change Google Cloud")
	assert.Empty(t, a.asked)
}

// A run started while a switch was off never gets its route, whatever the
// switch does later.
func TestARunStartedWhileOffNeverGetsARoute(t *testing.T) {
	t.Parallel()

	sw := switchesOn()
	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	other := newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid")
	sw.set(permissions.GCP, true)
	r := startRun(t, cloudTool(t, sw, p, other), chatRuntime(t, session.Chat, nil), false)
	sw.set(permissions.AWS, true)
	code, _ := send(t, r, http.MethodGet, "127.0.0.1:6443", "anything")
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, p.mounts)
}

// A change asked before the switch turns off is still forwarded once
// approved: it was already past the door.
func TestARequestWaitingOnTheUserOutlivesTheSwitch(t *testing.T) {
	t.Parallel()

	sw := switchesOn(permissions.AWS)
	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	a := newAnsweringAsker()
	r := startRun(t, cloudTool(t, sw, p), chatRuntime(t, session.Chat, a), false)
	token := testutil.Recv(t, p.mounts, "the mount").grant.Token()

	type answer struct {
		code int
		body string
	}
	answered := make(chan answer, 1)
	go func() {
		code, body := send(t, r, http.MethodPost, "127.0.0.1:6443", token)
		answered <- answer{code, body}
	}()
	asked := testutil.Recv(t, a.asked, "the change")
	assert.Equal(t, permissions.AWS, asked.Action.Target)
	sw.set(permissions.AWS, false)
	a.answers <- true
	got := testutil.Recv(t, answered, "the change's answer")
	assert.Equal(t, answer{http.StatusOK, "changed"}, got)
	code, _ := send(t, r, http.MethodGet, "127.0.0.1:6443", token)
	assert.Equal(t, http.StatusUnauthorized, code, "the next request reads the switch")
}

// A mount that fails fails the run, ends every grant already mounted and
// leaves nothing behind.
func TestAFailedMountEndsTheEarlierOnes(t *testing.T) {
	t.Parallel()

	first := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	failing := newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid")
	failing.err = errors.New("no login")
	tl := cloudTool(t, switchesOn(permissions.AWS, permissions.GCP), first, failing)
	var dir string
	tl.onRunDir = func(path string) { dir = path }
	rt := chatRuntime(t, session.Chat, nil)

	_, err := tl.sandboxedRunFor(t.Context(), tl.sandboxer, rt, tools.WorkspacePath(rt.Dir), false, session.NoNetwork)

	assert.ErrorContains(t, err, "no login")
	m := testutil.Recv(t, first.mounts, "the first mount")
	assert.False(t, m.grant.Holds(m.grant.Token()), "its grant has ended")
	assert.NotEmpty(t, dir)
	assert.NoDirExists(t, dir)
}

// A run whose cloud platforms' routes or variables clash fails as a failed
// mount does.
func TestAClashFailsTheRun(t *testing.T) {
	t.Parallel()

	for name, second := range map[string]*fakePlatform{
		"a host": newFakePlatform(permissions.GCP, "127.0.0.1:6443"),
		"a variable": func() *fakePlatform {
			p := newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid")
			p.env = []string{"SHARED=b"}
			return p
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			first := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
			first.env = []string{"SHARED=a"}
			tl := cloudTool(t, switchesOn(permissions.AWS, permissions.GCP), first, second)
			rt := chatRuntime(t, session.Chat, nil)
			_, err := tl.sandboxedRunFor(t.Context(), tl.sandboxer, rt, tools.WorkspacePath(rt.Dir), false, session.NoNetwork)
			assert.Error(t, err)
			m := testutil.Recv(t, first.mounts, "the first mount")
			assert.False(t, m.grant.Holds(m.grant.Token()))
		})
	}
}

// A cloud platform's variables ride the run's environment.
func TestACloudPlatformsVariablesRideTheRun(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	p.env = []string{"FAKE_ENDPOINT=http://127.0.0.1:6443"}
	r := startRun(t, cloudTool(t, switchesOn(permissions.AWS), p), chatRuntime(t, session.Chat, nil), false)
	assert.Contains(t, r.run.Env, "FAKE_ENDPOINT=http://127.0.0.1:6443")
}

// Once the run ends, every grant has ended, and a request waiting on the
// user was cancelled before end returned.
func TestTheRunEndsEveryGrant(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	a := newAnsweringAsker()
	tl := cloudTool(t, switchesOn(permissions.AWS), p)
	rt := chatRuntime(t, session.Chat, a)
	r, err := tl.sandboxedRunFor(t.Context(), tl.sandboxer, rt, tools.WorkspacePath(rt.Dir), false, session.NoNetwork)
	require.NoError(t, err)
	m := testutil.Recv(t, p.mounts, "the mount")

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:6443/x", nil)
		req.Header.Set(tokenHeader, m.grant.Token())
		if resp, err := overSocket(r).Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	testutil.Recv(t, a.asked, "the change")
	r.end()
	assert.False(t, m.grant.Holds(m.grant.Token()))
	// No answer was given, so the Ask returned on the grant's end, before
	// end did.
	assert.Empty(t, a.answers)
	testutil.Wait(t, sent, "the request to end")
	assert.Empty(t, a.asked, "nothing is asked once the run is over")
}

// tlsOverSocket is a TLS client of the run's socket trusting roots.
func tlsOverSocket(r *sandboxedRun, roots *x509.CertPool) *http.Client {
	c := overSocket(r)
	c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"}
	return c
}

// A run with a TLS route writes ca.pem where the mount was told it would be,
// and the door answers TLS with the certificate it holds.
func TestARunWithATLSRouteHasItsCertificate(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.Azure, "127.0.0.1:6443")
	p.tls = true
	r := startRun(t, cloudTool(t, switchesOn(permissions.Azure), p), chatRuntime(t, session.Chat, nil), false)
	m := testutil.Recv(t, p.mounts, "the mount")
	assert.Equal(t, r.dir.caFile(), m.run.CAFile)

	bundle, err := os.ReadFile(r.dir.caFile())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(bundle))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:6443/x", nil)
	require.NoError(t, err)
	req.Header.Set(tokenHeader, m.grant.Token())
	resp, err := tlsOverSocket(r, roots).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "read azure", string(b))
}

// A run with no TLS route has no ca.pem, and its door closes a handshake.
func TestARunWithNoTLSRouteHasNoCertificate(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.AWS, "127.0.0.1:6443")
	r := startRun(t, cloudTool(t, switchesOn(permissions.AWS), p), chatRuntime(t, session.Chat, nil), false)
	testutil.Recv(t, p.mounts, "the mount")
	assert.NoFileExists(t, r.dir.caFile())

	_, err := tlsOverSocket(r, x509.NewCertPool()).Get("https://127.0.0.1:6443/x")
	assert.Error(t, err)
	code, _ := send(t, r, http.MethodGet, "127.0.0.1:6443", "")
	assert.Equal(t, http.StatusUnauthorized, code, "plain HTTP is still served")
}

// Through the real sandbox, curl trusting ca.pem alone reaches a TLS route.
func TestACurlTrustsTheRunsCertificate(t *testing.T) {
	t.Parallel()

	tl := confiningTool(t)
	withTool(t, tl, "curl")
	p := newFakePlatform(permissions.Azure)
	p.tls, p.exports, p.atDoor = true, true, true
	tl.cloud = Cloud{Platforms: []CloudPlatform{p}, Switches: switchesOn(permissions.Azure).read}

	text, isError := tl.Run(t.Context(), chatRuntime(t, session.Chat, nil),
		command(`curl -sS --cacert "$FAKE_CA" -H "`+tokenHeader+`: $FAKE_TOKEN" "https://$FAKE_DOOR/x"`))

	require.False(t, isError, text)
	assert.Equal(t, "read azure", text)
}

// A ca.pem that cannot be written fails the run, and ends the grant mounted.
func TestABundleThatCannotBeWrittenFailsTheRun(t *testing.T) {
	t.Parallel()

	p := newFakePlatform(permissions.Azure, "127.0.0.1:6443")
	p.tls = true
	p.onMount = func(run CloudRun) { _ = os.WriteFile(run.CAFile, nil, 0o600) }
	tl := cloudTool(t, switchesOn(permissions.Azure), p)
	rt := chatRuntime(t, session.Chat, nil)
	_, err := tl.sandboxedRunFor(t.Context(), tl.sandboxer, rt, tools.WorkspacePath(rt.Dir), false, session.NoNetwork)
	require.Error(t, err)
	m := testutil.Recv(t, p.mounts, "the mount")
	assert.False(t, m.grant.Holds(m.grant.Token()))
}

// recordingUpstream is a cloud platform's API over TLS that answers body to
// every request and keeps the Authorization each one carried.
func recordingUpstream(t *testing.T, body string) (*httptest.Server, chan string) {
	t.Helper()
	seen := make(chan string, 8)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

// Through the real sandbox, with network off, one run on a cluster with all
// four cloud platforms on reads each cloud platform as the user, and its
// client still reads the cluster. No variable is set twice.
func TestEveryCloudPlatformAndTheClusterAnswerInOneRun(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t)
	tl := proxyTool(t, &fakeLease{serverUID: "uid-1", conn: api.connection()})
	tl.sandboxer = confining(t)
	withTool(t, tl, "curl")

	awsUpstream, awsSeen := recordingUpstream(t, `{"aws":"ok"}`)
	githubUpstream, githubSeen := recordingUpstream(t, `{"login":"octo"}`)
	google := newFakeGoogle(t)
	_, azureSeen, azureTransport := fakeResourceManager(t)
	login := fakeLogin{account: azureAccount("Pay-As-You-Go"), ok: true, token: "users-entra-token"}
	tl.cloud = Cloud{
		Platforms: []CloudPlatform{
			awsAs(fakeAWSLogin{"eu-west-1"}, &http.Client{Transport: testutil.DialAnywhere(awsUpstream)}),
			newTestGitHub(upstreamsOf(githubUpstream)),
			NewGCP(gcpauth.New(helperAnswering("users-google-token", "proj-1")), google),
			NewAzure(login, azureTransport),
		},
		Switches: switchesOn(permissions.AWS, permissions.GitHub, permissions.GCP, permissions.Azure).read,
	}
	rt := tools.Runtime{ClusterID: "7", Dir: testChatDir(t), Session: session.Session{Kind: session.Chat, Policy: askSession.Policy}}

	text, isError := tl.Run(t.Context(), rt, command(
		`curl -sS --aws-sigv4 aws:amz:eu-west-1:ec2 -u "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" `+
			`"$AWS_ENDPOINT_URL/?Action=DescribeInstances&Version=2016-11-15"; echo; `+
			`curl -sS -x "$HTTP_PROXY" http://api.github.localhost/user; echo; `+
			`curl -sS -x "http://127.0.0.1:$CLOUDSDK_PROXY_PORT" -H "Authorization: Bearer $CLOUDSDK_AUTH_ACCESS_TOKEN" `+
			`http://logging.gcp.kubetail.invalid/v2/projects/proj-1/logs; echo; `+
			`curl -sS --cacert "$REQUESTS_CA_BUNDLE" -H "Authorization: Bearer $IDENTITY_HEADER" `+
			`"${IDENTITY_ENDPOINT%/kubetail/identity}/subscriptions/`+azSub+`/providers/Microsoft.ContainerService/managedClusters?api-version=1"; echo; `+
			// The client's own output is left out: under coverage its exit
			// warning names a directory outside the sandbox.
			clientLine()+` >/dev/null 2>&1; echo "cluster $?"; `+
			`echo "--- env"; env`))

	require.False(t, isError, text)
	out, env, found := strings.Cut(text, "--- env\n")
	require.True(t, found, text)
	assert.Contains(t, out, `{"aws":"ok"}`)
	assert.Contains(t, out, `{"login":"octo"}`)
	assert.Contains(t, out, `{"logNames":[]}`)
	assert.Contains(t, out, `"aks-1"`)
	assert.Contains(t, out, "cluster 0\n")

	assert.Contains(t, testutil.Recv(t, awsSeen, "AWS's read"), "Credential="+awsTestCreds.AccessKeyID+"/")
	assert.Equal(t, "Bearer "+usersToken, testutil.Recv(t, githubSeen, "GitHub's read"))
	assert.Equal(t, "Bearer users-google-token", testutil.Recv(t, google.auths, "Google's read"))
	assert.Equal(t, "Bearer users-entra-token", testutil.Recv(t, azureSeen, "Azure's read"))
	assert.Equal(t, []string{"Bearer users-own"}, api.credentials())

	names := map[string]bool{}
	for line := range strings.Lines(env) {
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		assert.False(t, names[name], "%s is set twice", name)
		names[name] = true
	}
	assert.True(t, names["AWS_ENDPOINT_URL"] && names["HTTP_PROXY"] && names["CLOUDSDK_AUTH_ACCESS_TOKEN"] && names["IDENTITY_ENDPOINT"] && names["KUBECONFIG"])
}

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

// The five probes over one kube-context's server: what each asks and how the answers classify.
// Scheduling belongs to the supervisor (internal/lib/supervisor) — each probe behind reachability declares its
// dependency on the connection rather than testing it.
//
// While the connection has not succeeded the supervisor records the four behind it as
// DependencyFailed rather than dialing: one timeout per cycle, not one per probe.
package kubeconn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/supervisor"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/kubeconfig"
)

// Registration names are how probes are addressed everywhere: the edges, Wake, and every read
// take one. A probe body reads a sibling with supervisor.GetJobObservation[T](snap, nameConnection).
const (
	nameConnection    = "connection"
	nameReadiness     = "readiness"
	nameServerUID     = "serverUID"
	nameServerVersion = "serverVersion"
	namePrincipal     = "principal"
)

// probeNames is the full set in registration order, for readers that walk every probe.
var probeNames = [...]string{nameConnection, nameReadiness, nameServerUID, nameServerVersion, namePrincipal}

// keyConnection reads the connection's observable — the only one another probe needs, so the
// only one with a key.
var keyConnection = supervisor.NewKey[connInfo](nameConnection)

// connInfo is the connection probe's observable: the context's standing in the kubeconfig, and
// the connection reaching its server yields.
//
// Comparable, which the commit-only-on-a-change guard rests on: a rebuilt connection is a new
// pointer and moves the value, a re-dialed one is not.
type connInfo struct {
	// departed is what the probe's last read of the kubeconfig said: true once the file
	// stops naming this context.
	departed bool
	// conn is the connection the probes run over, nil while the context resolves to nothing.
	conn *Connection
	// endpoint is the API server URL that answered.
	endpoint string
	// fingerprint describes the credentials conn was built from — what tells a rotation
	// from a kubeconfig write that changed nothing.
	fingerprint string
}

// connectionTimeout is how long one dial may take. Named because a caller waiting on a probe
// sizes its own ceiling off it (RetryAndWait), and a second literal would let a change here
// silently undersize that wait.
const connectionTimeout = 10 * time.Second

func registerProbes(e *supervisor.Supervisor, kubecfg kubeconfigService, connOpts []Option) {
	// A reachability check that has taken ten seconds has answered, so the timeout is
	// shorter than the supervisor's default (a whole interval).
	supervisor.RegisterJob(e, nameConnection, &connectionProbe{kubecfgSvc: kubecfg, connOpts: connOpts},
		supervisor.WithInterval(30*time.Second), supervisor.WithTimeout(connectionTimeout))

	// The four behind reachability declare both edges on it: they cannot run without a
	// connection, and a connection that moves must re-run them.
	dependsOnConn, watchesConn := supervisor.WithDependencies(nameConnection), supervisor.WithWatches(nameConnection)
	supervisor.RegisterJob(e, nameReadiness, readinessProbe{}, supervisor.WithInterval(30*time.Second), dependsOnConn, watchesConn)
	supervisor.RegisterJob(e, nameServerUID, serverUIDProbe{}, supervisor.WithInterval(10*time.Minute), dependsOnConn, watchesConn)
	supervisor.RegisterJob(e, nameServerVersion, serverVersionProbe{}, supervisor.WithInterval(5*time.Minute), dependsOnConn, watchesConn)
	supervisor.RegisterJob(e, namePrincipal, principalProbe{}, supervisor.WithInterval(5*time.Minute), dependsOnConn, watchesConn)
}

// The paths the four probes behind reachability read. selfSubjectReviewPath is
// authentication.k8s.io/v1 from 1.27 on, so an older server answers it with a 404.
const (
	readyzPath            = "/readyz"
	kubeSystemPath        = "/api/v1/namespaces/kube-system"
	versionPath           = "/version"
	selfSubjectReviewPath = "/apis/authentication.k8s.io/v1/selfsubjectreviews"
)

// selfSubjectReviewBody is what is posted to selfSubjectReviewPath. A create endpoint decodes a
// resource from the request body, so an empty POST is a 400 and the username silently comes back
// missing; the server fills in the status itself. Sent raw rather than through a typed client,
// which would link the API types this binary keeps out.
var selfSubjectReviewBody = []byte(`{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview"}`)

// connFrom is the connection a probe behind reachability runs over. Nil is unreachable while the
// supervisor holds them behind the connection probe's success — a departure commits no connection and
// suspends, which reads as a failed dependency — so a run that finds one parks for the wake the
// connection's next move sends rather than recording an answer no request produced.
func connFrom(snap supervisor.Snapshot) *Connection { return keyConnection.From(snap).Value.conn }

// failed is what a run concluded from a request that did not answer.
//
// Cancellation is the caller going away — the supervisor stopping, not the cluster — so the run
// records nothing at all rather than opening a failure streak against a server that refused
// nothing. Every other error is classified.
func failed(err error) supervisor.Result {
	if errors.Is(err, context.Canceled) {
		return supervisor.Skip()
	}
	return supervisor.Fail(classify(err), err)
}

// endpointGone reports whether err is the 404 that means the endpoint itself is absent.
//
// **Only the probe knows which 404 it is looking at**: an object that is missing is news about the
// cluster and a probe that keeps asking, an endpoint that is missing is terminal for this
// connection. Classifying on the status code alone suspends a probe that should have kept running.
func endpointGone(err error) bool {
	var status *httpErr
	return errors.As(err, &status) && status.code == http.StatusNotFound
}

// apiDiscoveryPath is the one request this probe makes: the cheapest that proves the whole path
// (DNS, TCP, TLS, authentication), the only endpoint of the five probes' that can answer 401 or
// 403, and the one whose body tells a Kubernetes API server from a captive portal answering 200
// to anything.
const apiDiscoveryPath = "/api"

// connectionProbe answers whether the server behind the context can be reached. It also owns the
// context's lifecycle: resolving the kubeconfig is the first step of reaching a server, so a
// context that left the file is this probe's to find. The kubeconfig watch wakes it on every
// change.
type connectionProbe struct {
	kubecfgSvc kubeconfigService
	// connOpts tune every connection this probe builds.
	connOpts []Option
}

// Run resolves the context, keeps its connection current, and dials the server once.
func (p *connectionProbe) Run(ctx context.Context, pass *supervisor.JobPass[connInfo]) supervisor.Result {
	cfg, fingerprint, err := p.kubecfgSvc.RESTConfig(pass.Subject())
	if errors.Is(err, kubeconfig.ErrNotRead) {
		// An unread kubeconfig names nothing; saying so would report every context gone
		// for as long as the first read takes. The watch wakes us when it is read.
		return supervisor.Skip()
	}

	next := pass.Prev()
	// Deferred so no return path can skip it: a connection built by a run that then failed
	// must still be committed, or nothing can reach it to retire it. Only on a change — an
	// unconditional commit would re-run the four probes watching this value every cycle.
	defer func() {
		if next != pass.Prev() {
			pass.Commit(next)
		}
	}()

	if errors.Is(err, kubeconfig.ErrContextNotFound) {
		next = connInfo{departed: true}
		return supervisor.Suspend(ReasonContextNotFound, "kubeconfig no longer names this context")
	}
	next.departed = false
	if err != nil {
		// The file still names the context, so this is not a departure — it is a file the
		// user has to fix, retried on the ladder because a fix can be invisible to the
		// kubeconfig watch (a CA path that now opens). The connection stands: a read that
		// failed says nothing about the credentials behind it, and an editor saving
		// non-atomically fails reads for a moment. Credentials that really moved come back
		// as a changed fingerprint.
		return supervisor.Fail(ReasonResolveFailed, err)
	}

	// Rebuild on a changed fingerprint or no connection — never the fingerprint alone: a
	// failed build commits the fingerprint with nothing behind it, and a fingerprint-only
	// check would find it unchanged and never build again.
	//
	// The third arm is the server replaced behind credentials that never moved: nothing about
	// the file says so, and the connection is the only thing that knows. Asked of the
	// connection rather than compared against the identity observable — that pairing is stale
	// by a dispatch plus a round trip, while the conflict was recorded by the probe that made
	// the request over this connection. A first identification records none, so nothing here
	// rebuilds a connection merely because the probe behind it answered.
	if next.conn == nil || next.fingerprint != fingerprint || next.conn.conflicted() {
		conn, err := NewConnection(cfg, p.connOpts...)
		next = connInfo{conn: conn, fingerprint: fingerprint}
		if err != nil {
			// Nothing was dialed; the remedy is the same file fix a context that will
			// not resolve needs.
			return supervisor.Fail(ReasonResolveFailed, err)
		}
		next.endpoint = conn.BaseURL.String()
	}

	var discovery struct {
		Versions []string `json:"versions"`
	}
	if err := next.conn.getJSON(ctx, apiDiscoveryPath, &discovery); err != nil {
		// A cancellation records nothing, and the supervisor hands the connection this run
		// built back through Discard.
		return failed(err)
	}
	if len(discovery.Versions) == 0 {
		return supervisor.Fail(ReasonMalformed, fmt.Errorf("%s: answered without API versions", apiDiscoveryPath))
	}
	return supervisor.Succeeded()
}

// Discard retires a connection the supervisor dropped — a run whose commit it refused because the
// context was released mid-run, one that concluded Skip, or one that panicked. Such a value
// reaches neither a snapshot nor an entry, so nothing else can retire what it built.
//
// Retiring outright is safe because a committed connInfo always carries a connection this run
// built: every change either clears the connection (a departure) or rebuilds it (a changed
// fingerprint), so a value that moved never carries a connection someone else holds.
func (p *connectionProbe) Discard(v connInfo) {
	if v.conn != nil {
		v.conn.Retire()
	}
}

// readinessProbe reads the API server's own account of whether it is fit to serve.
//
// /readyz is the one endpoint whose *failure* carries the answer: a 500 body names the checks that
// are not ok, and a probe reading only the status code would know the cluster is unwell without
// being able to say which part.
type readinessProbe struct{}

func (readinessProbe) Run(ctx context.Context, pass *supervisor.JobPass[ComponentStatus]) supervisor.Result {
	conn := connFrom(pass.Snapshot())
	if conn == nil {
		return supervisor.Skip()
	}

	_, err := conn.getText(ctx, readyzPath)
	var status *httpErr
	switch {
	case err == nil:
		commitStatus(pass, ComponentStatus{})
		return supervisor.Succeeded()
	case endpointGone(err):
		// A managed distribution that withholds it will not start serving it: terminal for
		// this connection, and a new one re-arms the probe.
		return supervisor.Suspend(ReasonUnsupported, "the server does not serve "+readyzPath)
	case errors.As(err, &status) && status.code == http.StatusInternalServerError:
		failing := failingComponents(status.body)
		if len(failing) == 0 {
			// A 500 from something that is not the readyz handler: it answered, but not
			// with the one thing this endpoint's failure is supposed to carry.
			return supervisor.Fail(ReasonInternalError, err)
		}
		commitStatus(pass, ComponentStatus{Failing: failing})
		return supervisor.Fail(ReasonComponentsFailing, fmt.Errorf("%s: %s", readyzPath, strings.Join(failing, ", ")))
	default:
		return failed(err)
	}
}

// commitStatus records what /readyz said, on the first answer and whenever the set moves after
// it. Two guards, because a healthy server's answer *is* the zero value: without the first, a
// cluster that has never had a failing component never commits, and its readiness reads as never
// observed. ComponentStatus carries a slice, so neither can be the == the comparable observables
// use.
func commitStatus(pass *supervisor.JobPass[ComponentStatus], next ComponentStatus) {
	if !pass.Known() || !slices.Equal(next.Failing, pass.Prev().Failing) {
		pass.Commit(next)
	}
}

// failingComponents are the checks a readyz body reports as not ok, in the order it named them.
// Every check is a line — "[+]etcd ok" or "[-]etcd failed: reason withheld" — whether or not
// verbose was asked for, so the plain endpoint carries the detail and no query string is needed.
func failingComponents(body string) []string {
	var failing []string
	for line := range strings.Lines(body) {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "[-]")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, " ")
		if name != "" {
			failing = append(failing, name)
		}
	}
	return failing
}

// serverUIDProbe reads kube-system's UID, conventionally the cluster's own identity: the one thing
// that tells a rebuilt cluster from the one that was there before, behind credentials and an
// endpoint that never moved.
type serverUIDProbe struct{}

func (serverUIDProbe) Run(ctx context.Context, pass *supervisor.JobPass[string]) supervisor.Result {
	conn := connFrom(pass.Snapshot())
	if conn == nil {
		return supervisor.Skip()
	}

	var ns struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err := conn.getJSON(ctx, kubeSystemPath, &ns); err != nil {
		if endpointGone(err) {
			// The namespace is absent, not the endpoint — news about the cluster, and a
			// probe that keeps asking, since it can be created.
			return supervisor.Fail(ReasonNotFound, err)
		}
		return failed(err)
	}
	if ns.Metadata.UID == "" {
		return supervisor.Fail(ReasonMalformed, fmt.Errorf("%s: answered without a UID", kubeSystemPath))
	}

	// Stamped unconditionally, committed only on a change: the two answer different
	// questions. The commit says the CONTEXT's identity moved, which drives the fleet
	// signal; the stamp says THIS connection has been identified, which is true of the
	// first read whether or not the value changed. Gated on the commit, a rebuilt
	// connection to a cluster whose UID never moves would never be stamped at all.
	conn.setServerUID(ns.Metadata.UID)

	if ns.Metadata.UID != pass.Prev() {
		pass.Commit(ns.Metadata.UID)
	}
	return supervisor.Succeeded()
}

// serverVersionProbe reads the API server's reported version.
type serverVersionProbe struct{}

func (serverVersionProbe) Run(ctx context.Context, pass *supervisor.JobPass[VersionInfo]) supervisor.Result {
	conn := connFrom(pass.Snapshot())
	if conn == nil {
		return supervisor.Skip()
	}

	var body struct {
		GitVersion string `json:"gitVersion"`
		Major      string `json:"major"`
		Minor      string `json:"minor"`
	}
	if err := conn.getJSON(ctx, versionPath, &body); err != nil {
		return failed(err)
	}
	if body.GitVersion == "" {
		return supervisor.Fail(ReasonMalformed, fmt.Errorf("%s: answered without a version", versionPath))
	}

	next := VersionInfo{GitVersion: body.GitVersion, Major: body.Major, Minor: body.Minor}
	if next != pass.Prev() {
		pass.Commit(next)
	}
	return supervisor.Succeeded()
}

// principalProbe asks the server who these credentials authenticate as, which is the only
// authoritative answer: a token names its subject to the server, not to us.
type principalProbe struct{}

func (principalProbe) Run(ctx context.Context, pass *supervisor.JobPass[Principal]) supervisor.Result {
	conn := connFrom(pass.Snapshot())
	if conn == nil {
		return supervisor.Skip()
	}

	var review struct {
		Status struct {
			UserInfo struct {
				Username string   `json:"username"`
				Groups   []string `json:"groups"`
			} `json:"userInfo"`
		} `json:"status"`
	}
	if err := conn.postJSON(ctx, selfSubjectReviewPath, selfSubjectReviewBody, &review); err != nil {
		if endpointGone(err) {
			// A server too old to serve it will not grow the endpoint under us. Terminal
			// for this connection, and a new one re-arms the probe.
			return supervisor.Suspend(ReasonUnsupported, "the server does not serve "+selfSubjectReviewPath)
		}
		return failed(err)
	}
	if review.Status.UserInfo.Username == "" {
		return supervisor.Fail(ReasonMalformed, fmt.Errorf("%s: answered without a username", selfSubjectReviewPath))
	}

	next := Principal{Username: review.Status.UserInfo.Username, Groups: review.Status.UserInfo.Groups}
	// Sorted, so a server listing the same groups in another order is not a change every
	// watcher of this value has to be woken for.
	slices.Sort(next.Groups)
	if next.Username != pass.Prev().Username || !slices.Equal(next.Groups, pass.Prev().Groups) {
		pass.Commit(next)
	}
	return supervisor.Succeeded()
}

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

package kubeproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

const (
	configMapPath  = "/api/v1/namespaces/web/configmaps/c"
	deploymentPath = "/apis/apps/v1/namespaces/web/deployments/d"
)

// objectNow is the ConfigMap the object servers hold.
const objectNow = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"c","namespace":"web","resourceVersion":"1"},"data":{"k":"old"}}`

// dryRunSeen is a dry run an object server answered: its query, media type
// and body.
type dryRunSeen struct {
	query, contentType, body string
}

// objectServer is an API server holding one object: a GET answers now, and a
// write with dryRun=All answers dry with the body sent. dry nil answers the
// ConfigMap with k new. Every other request answers 200 and {}.
func objectServer(t *testing.T, now string, dry func(body string) (int, string)) (*apiServer, <-chan dryRunSeen) {
	t.Helper()
	if dry == nil {
		dry = func(string) (int, string) { return http.StatusOK, strings.Replace(objectNow, `"old"`, `"new"`, 1) }
	}
	seen := make(chan dryRunSeen, 16)
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, now)
		case r.URL.Query().Get("dryRun") == "All":
			body, _ := io.ReadAll(r.Body)
			seen <- dryRunSeen{query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"), body: string(body)}
			code, answer := dry(string(body))
			w.WriteHeader(code)
			_, _ = io.WriteString(w, answer)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	})
	return api, seen
}

// askedRequest sends a write through s, and answers the request it was asked
// as, once denied.
func askedRequest(t *testing.T, s *served, asker fakeAsker, method, path, contentType, body string) Request {
	t.Helper()
	done := s.sendAsync(t, s.writeRequest(t, method, path, contentType, body))
	a := asker.next(t)
	a.answer <- denied
	testutil.Recv(t, done, "the write to be answered")
	return a.r
}

// A PUT of an object that exists asks with the change as a diff: the object
// now against the dry run's answer, as YAML, what the server assigns dropped.
func TestADiffForAPutShowsTheChange(t *testing.T) {
	api, _ := objectServer(t, objectNow, nil)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", strings.Replace(objectNow, `"old"`, `"new"`, 1))

	assert.Equal(t, "@@ -1,6 +1,6 @@\n apiVersion: v1\n data:\n-  k: old\n+  k: new\n kind: ConfigMap\n metadata:\n   name: c\n", r.Diff)
	assert.False(t, r.DiffCut)
	assert.Empty(t, r.DiffError)
	require.NotNil(t, r.Write, "the request itself still rides beside the diff")
}

// A patch's dry run carries the body and media type as sent, and dryRun=All
// beside the query's own pairs: a merge, a strategic and an apply patch alike.
func TestADiffForAPatch(t *testing.T) {
	for _, contentType := range []string{"application/merge-patch+json", "application/strategic-merge-patch+json", "application/apply-patch+yaml"} {
		api, seen := objectServer(t, objectNow, nil)
		asker := make(fakeAsker, 1)
		s := serveAsking(t, api.upstream(), asker)
		body := `{"data":{"k":"new"}}`

		r := askedRequest(t, s, asker, "PATCH", configMapPath+"?fieldManager=kubectl&force=true", contentType, body)

		assert.Contains(t, r.Diff, "-  k: old\n+  k: new\n", contentType)
		dry := testutil.Recv(t, seen, "the dry run")
		assert.Equal(t, "fieldManager=kubectl&force=true&dryRun=All", dry.query, contentType)
		assert.Equal(t, contentType, dry.contentType)
		assert.Equal(t, body, dry.body)
	}
}

// A write the policy allows or refuses is never previewed: its body reaches
// no webhook unasked.
func TestOnlyAPromptedWriteIsPreviewed(t *testing.T) {
	for name, sess := range map[string]sessionCase{
		"auto":      {permissions.ModeAuto, nil},
		"a deny":    {permissions.ModeAuto, []permissions.Rule{rule(permissions.Deny, permissions.Write, "*")}},
		"read-only": {permissions.ModeReadOnly, nil},
	} {
		api, _ := objectServer(t, objectNow, nil)
		s := serveIn(t, api.upstream(), sessionIn(sess.mode, sess.rules...), newRecordingAsker())
		s.do(t, s.writeRequest(t, "PATCH", configMapPath, "application/merge-patch+json", `{}`))
		for _, r := range api.requests() {
			assert.False(t, r.Method == http.MethodGet || r.URL.Query().Get("dryRun") == "All", "%s: %s %s", name, r.Method, r.URL)
		}
	}
}

// sessionCase is a mode and the rules beside it.
type sessionCase struct {
	mode  permissions.Mode
	rules []permissions.Rule
}

// A write on a group version that may be an aggregated API is not previewed,
// since its server may ignore dryRun and apply the write.
func TestAnAPIThatMayIgnoreADryRunIsNotPreviewed(t *testing.T) {
	api, _ := objectServer(t, objectNow, nil)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PATCH", "/apis/example.com/v1/namespaces/web/widgets/w", "application/merge-patch+json", `{}`)

	assert.Empty(t, r.Diff)
	assert.Equal(t, previewUnhonored, r.DiffError)
	assert.Empty(t, api.requests())
}

// A POST has no object to read and a DELETE no change to draw: neither sends
// a read or a dry run, and both carry no diff and no reason.
func TestAPostOrADeleteHasNoDiff(t *testing.T) {
	api, _ := objectServer(t, objectNow, nil)
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	for _, c := range []struct{ method, path, contentType, body string }{
		{"POST", "/api/v1/namespaces/web/configmaps", "application/json", objectNow},
		{"DELETE", configMapPath, "", ""},
	} {
		r := askedRequest(t, s, asker, c.method, c.path, c.contentType, c.body)
		assert.Empty(t, r.Diff, c.method)
		assert.Empty(t, r.DiffError, c.method)
	}
	assert.Empty(t, api.requests())
}

// What the API server sets on every write shows no line; every other field is
// kept.
func TestADiffDropsWhatTheServerAssigns(t *testing.T) {
	now := `{"kind":"ConfigMap","metadata":{"name":"c","resourceVersion":"1","generation":1,"managedFields":[{"manager":"a"}],"labels":{"a":"1"}},"data":{"k":"v"}}`
	then := `{"kind":"ConfigMap","metadata":{"name":"c","resourceVersion":"2","generation":2,"managedFields":[{"manager":"b"}],"labels":{"a":"2"}},"data":{"k":"v"}}`
	api, _ := objectServer(t, now, func(string) (int, string) { return http.StatusOK, then })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", then)

	assert.Equal(t, "@@ -3,5 +3,5 @@\n kind: ConfigMap\n metadata:\n   labels:\n-    a: \"1\"\n+    a: \"2\"\n   name: c\n", r.Diff)
}

// A write to status, through the subresource or on a kind with none, shows
// its status line.
func TestADiffShowsAStatusWrite(t *testing.T) {
	for _, path := range []string{
		"/apis/apps/v1/namespaces/web/deployments/d/status",
		"/apis/apiextensions.k8s.io/v1/customresourcedefinitions/w.example.com",
	} {
		now := `{"kind":"X","metadata":{"name":"d"},"status":{"phase":"old"}}`
		api, _ := objectServer(t, now, func(string) (int, string) {
			return http.StatusOK, strings.Replace(now, `"old"`, `"new"`, 1)
		})
		asker := make(fakeAsker, 1)
		s := serveAsking(t, api.upstream(), asker)

		r := askedRequest(t, s, asker, "PUT", path, "application/json", now)

		assert.Contains(t, r.Diff, "-  phase: old\n+  phase: new\n", path)
	}
}

// A Secret's diff says which keys change and never what they hold: a changed
// value reads [redacted] against [redacted: changed], an unchanged one
// [redacted] on both sides, stringData alike.
func TestADiffOfASecretMarksTheChangedKeys(t *testing.T) {
	now := `{"kind":"Secret","metadata":{"name":"db"},"data":{"a":"b2xk","same":"c2FtZQ=="},"stringData":{"s":"one"}}`
	then := `{"kind":"Secret","metadata":{"name":"db"},"data":{"a":"bmV3","same":"c2FtZQ==","added":"eA=="},"stringData":{"s":"two"}}`
	api, _ := objectServer(t, now, func(string) (int, string) { return http.StatusOK, then })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", "/api/v1/namespaces/web/secrets/db", "application/json", then)

	assert.Equal(t, "@@ -1,8 +1,9 @@\n data:\n-  a: '[redacted]'\n+  a: '[redacted: changed]'\n+  added: '[redacted: changed]'\n   same: '[redacted]'\n kind: Secret\n metadata:\n   name: db\n stringData:\n-  s: '[redacted]'\n+  s: '[redacted: changed]'\n", r.Diff)
	for _, value := range []string{"b2xk", "bmV3", "c2FtZQ==", "eA==", "one", "two"} {
		assert.NotContains(t, r.Diff, value)
	}
}

// A last-applied annotation, which holds a whole manifest whatever the kind,
// is redacted in every diff, and a change to it alone still reads as one.
func TestADiffHidesTheLastApplied(t *testing.T) {
	secretManifest := `{\"kind\":\"Secret\",\"data\":{\"p\":\"aHVudGVyMg==\"}}`
	now := `{"kind":"ConfigMap","metadata":{"name":"c","annotations":{"` + lastApplied + `":"` + secretManifest + `"}},"data":{"k":"old"}}`
	then := strings.Replace(strings.Replace(now, `"old"`, `"new"`, 1), "aHVudGVyMg==", "bmV3", 1)
	api, _ := objectServer(t, now, func(string) (int, string) { return http.StatusOK, then })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", then)

	assert.NotContains(t, r.Diff, "aHVudGVyMg==")
	assert.Contains(t, r.Diff, "-  k: old\n+  k: new\n")

	alone := strings.Replace(now, "aHVudGVyMg==", "bmV3", 1)
	api, _ = objectServer(t, now, func(string) (int, string) { return http.StatusOK, alone })
	s = serveAsking(t, api.upstream(), asker)
	r = askedRequest(t, s, asker, "PUT", configMapPath, "application/json", alone)
	assert.Contains(t, r.Diff, "-    "+lastApplied+": '[redacted]'\n+    "+lastApplied+": '[redacted: changed]'\n")
}

// A last-applied annotation inside a workload's pod template is compared where
// it sits: a change to it alone changes the template, so it is never "No
// change." while the object's own annotation stands.
func TestADiffMarksANestedLastAppliedThatChanged(t *testing.T) {
	now := `{"kind":"Deployment","metadata":{"name":"d","annotations":{"` + lastApplied + `":"a"}},` +
		`"spec":{"template":{"metadata":{"annotations":{"` + lastApplied + `":"old"}}}}}`
	then := strings.Replace(now, `"old"`, `"new"`, 1)
	api, _ := objectServer(t, now, func(string) (int, string) { return http.StatusOK, then })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", deploymentPath, "application/json", then)

	assert.NotContains(t, r.Diff, noChange)
	assert.Contains(t, r.Diff, "-        "+lastApplied+": '[redacted]'\n+        "+lastApplied+": '[redacted: changed]'\n")
	assert.Equal(t, 1, strings.Count(r.Diff, changedMark), "the object's own annotation did not change")
}

// A webhook's refusal of the dry run is the reason there is no diff, and the
// write still asks and runs once approved.
func TestAFailingDryRunIsReportedAndTheWriteStillAsks(t *testing.T) {
	api, _ := objectServer(t, objectNow, func(string) (int, string) {
		return http.StatusBadRequest, `{"kind":"Status","message":"admission webhook \"x\" denied the request: no"}`
	})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	done := s.sendAsync(t, s.writeRequest(t, "PATCH", configMapPath, "application/merge-patch+json", `{}`))
	a := asker.next(t)
	assert.Empty(t, a.r.Diff)
	assert.Equal(t, `The dry run failed: admission webhook "x" denied the request: no`, a.r.DiffError)
	a.answer <- once
	r := testutil.Recv(t, done, "the write to be answered")
	require.NotNil(t, r.resp)
	assert.Equal(t, http.StatusOK, r.resp.StatusCode)
	assert.Len(t, changes(api.requests()), 1, "the approved write is forwarded")
}

// An object not there asks with the body alone: no diff and no reason.
func TestAnObjectNotThereHasNoDiff(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"kind":"Status","message":"not found"}`)
	})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PATCH", configMapPath, "application/merge-patch+json", `{}`)

	assert.Empty(t, r.Diff)
	assert.Empty(t, r.DiffError)
	assert.Len(t, api.requests(), 1, "a read alone")
}

// A diff past 2,000 lines is cut, says how many lines it left out, and is
// marked cut.
func TestADiffIsCutAtTwoThousandLines(t *testing.T) {
	var before, after strings.Builder
	for i := range 1500 {
		fmt.Fprintf(&before, `,"k%04d":"a"`, i)
		fmt.Fprintf(&after, `,"k%04d":"b"`, i)
	}
	now := `{"kind":"ConfigMap","metadata":{"name":"c"},"data":{"x":"x"` + before.String() + `}}`
	then := `{"kind":"ConfigMap","metadata":{"name":"c"},"data":{"x":"x"` + after.String() + `}}`
	api, _ := objectServer(t, now, func(string) (int, string) { return http.StatusOK, then })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", then)

	assert.True(t, r.DiffCut)
	lines := strings.Split(strings.TrimSuffix(r.Diff, "\n"), "\n")
	assert.Len(t, lines, maxDiffLines+1)
	assert.Regexp(t, `^… \d+ more lines not shown$`, lines[len(lines)-1])
}

// A write that changes nothing says so.
func TestAWriteThatChangesNothingSaysSo(t *testing.T) {
	api, _ := objectServer(t, objectNow, func(string) (int, string) { return http.StatusOK, objectNow })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)

	assert.Equal(t, "No change.", r.Diff)
}

// The endpoint's end cuts the preview's requests, and the write still asks.
// The endpoint retires while the read is in flight, and the read is held
// until the cut reaches the server, so the read is the request cut.
func TestADiffUsesTheEndpoint(t *testing.T) {
	done := make(chan struct{})
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(done)
		<-r.Context().Done()
	})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstreamUntil(done), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)

	assert.Empty(t, r.Diff)
	assert.True(t, strings.HasPrefix(r.DiffError, previewNoRead), r.DiffError)
}

// Each of the preview's bounds answers a reason and the write still asks: a
// dry run that hangs until the bound cuts it, a read or a dry run answering past
// maxDiffObject, a limiter whose next token comes past diffTimeout, and no
// slot free, in which case nothing is sent.
func TestTheDiffsRequestsAreBounded(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	dryRun := make(chan struct{})
	hanging := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, objectNow)
			return
		}
		// Read whole, so the server notices the client leave.
		_, _ = io.ReadAll(r.Body)
		close(dryRun)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	asker := make(fakeAsker, 1)
	g := NewGrant(hanging.upstream(), askSession, "c1", "dev", asker, noAsker, 1000, 1000, 32)
	// The bound fires once the dry run reaches the server, so the read never
	// races it.
	g.previewBound = func(ctx context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-dryRun:
				cancel()
			case <-ctx.Done():
			}
		}()
		return ctx, cancel
	}
	s := serveGrant(t, g)
	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)
	assert.True(t, strings.HasPrefix(r.DiffError, previewNoDryRun), r.DiffError)

	big := `{"kind":"ConfigMap","metadata":{"name":"c"},"data":{"k":"` + strings.Repeat("x", maxDiffObject) + `"}}`
	api, _ := objectServer(t, big, nil)
	s = serveAsking(t, api.upstream(), asker)
	r = askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)
	assert.Equal(t, previewTooLarge, r.DiffError)

	api, _ = objectServer(t, objectNow, func(string) (int, string) { return http.StatusOK, big })
	s = serveAsking(t, api.upstream(), asker)
	r = askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)
	assert.Equal(t, previewTooLarge, r.DiffError, "a dry run's answer is bounded too")

	// The read spends the one token, and the next comes long past diffTimeout.
	api, _ = objectServer(t, objectNow, nil)
	g = NewGrant(api.upstream(), askSession, "c1", "dev", asker, noAsker, 0.001, 1, 32)
	s = serveGrant(t, g)
	r = askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)
	assert.Equal(t, previewNoDryRun+"the cluster did not answer", r.DiffError)

	api, _ = objectServer(t, objectNow, nil)
	g = NewGrant(api.upstream(), askSession, "c1", "dev", asker, noAsker, 1000, 1000, 1)
	require.True(t, g.openRequests.TryAcquire(1))
	s = serveGrant(t, g)
	r = askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)
	assert.Equal(t, previewBusy, r.DiffError)
	assert.Empty(t, api.requests())
	g.openRequests.Release(1)
}

// A cluster Kubetail cannot reach is the reason there is no diff, in the
// upstream's own words, and the write still asks.
func TestAClusterNotReachedIsReported(t *testing.T) {
	up := upstreamFunc(func(context.Context) (Endpoint, error) { return Endpoint{}, ErrNotIdentified })
	asker := make(fakeAsker, 1)
	s := serveAsking(t, up, asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)

	assert.Empty(t, r.Diff)
	assert.Equal(t, previewNoRead+strings.TrimPrefix(unavailable(ErrNotIdentified), "kubetail: "), r.DiffError)
}

// A read the cluster refuses is reported with the Status's message, and the
// write still asks.
func TestARefusedReadIsReported(t *testing.T) {
	api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","message":"no reading"}`)
	})
	asker := make(fakeAsker, 1)
	s := serveAsking(t, api.upstream(), asker)

	r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)

	assert.Empty(t, r.Diff)
	assert.Equal(t, previewNoRead+"no reading", r.DiffError)
}

// A read the preview cannot make or finish is a cluster that did not answer,
// and the write still asks: a base whose host no URL can carry, and an answer
// cut short of its declared length.
func TestAReadThatFailsIsReported(t *testing.T) {
	badHost := upstreamFunc(func(context.Context) (Endpoint, error) {
		return Endpoint{Base: &url.URL{Scheme: "https", Host: "bad host"}, Client: http.DefaultClient}, nil
	})
	cut := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, `{"kind":`)
	}).upstream()
	for name, up := range map[string]Upstream{"a bad host": badHost, "a cut answer": cut} {
		asker := make(fakeAsker, 1)
		s := serveAsking(t, up, asker)

		r := askedRequest(t, s, asker, "PUT", configMapPath, "application/json", objectNow)

		assert.Empty(t, r.Diff, name)
		assert.Equal(t, previewNoRead+"the cluster did not answer", r.DiffError, name)
	}
}

// An answer that is not a JSON object names the side it came from.
func TestADiffOfAnswersThatAreNotObjectsSaysWhich(t *testing.T) {
	obj := []byte(objectNow)
	for name, tc := range map[string]struct {
		now, then string
		want      string
	}{
		"object not JSON":    {"nope", objectNow, previewNoRead + "it is not a JSON object"},
		"object null":        {"null", objectNow, previewNoRead + "it is not a JSON object"},
		"dry run not JSON":   {objectNow, "nope", previewNoDryRun + "its answer is not a JSON object"},
		"dry run not object": {objectNow, "[]", previewNoDryRun + "its answer is not a JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, preview{err: tc.want}, diffObjects([]byte(tc.now), []byte(tc.then), false))
		})
	}
	assert.Equal(t, preview{diff: noChange}, diffObjects(obj, obj, false))
}

// A last-applied annotation inside a list is compared by its index.
func TestADiffMarksALastAppliedInAList(t *testing.T) {
	now := `{"items":[{"metadata":{"annotations":{"` + lastApplied + `":"a"}}}]}`
	then := `{"items":[{"metadata":{"annotations":{"` + lastApplied + `":"b"}}}]}`
	p := diffObjects([]byte(now), []byte(then), false)
	assert.Contains(t, p.diff, "+      "+lastApplied+": '"+changedMark+"'")
	assert.NotContains(t, p.diff, ": b")
}

// A body with no Status message reads as the code's own text.
func TestStatusMessageFallsBackToTheCodesText(t *testing.T) {
	assert.Equal(t, "Forbidden", statusMessage(http.StatusForbidden, []byte("not json")))
	assert.Equal(t, "Forbidden", statusMessage(http.StatusForbidden, []byte(`{"message":""}`)))
	assert.Equal(t, "no", statusMessage(http.StatusForbidden, []byte(`{"message":"no"}`)))
}

// A Secret whose values cannot be hidden, on either side, has no diff.
func TestADiffOfASecretThatCannotBeHiddenIsNotDrawn(t *testing.T) {
	plain := `{"kind":"Secret","metadata":{"name":"r"},"data":{"k":"dg=="}}`
	bad := `{"kind":"Secret","metadata":{"name":"r"},"type":"` + helmReleaseType + `","data":{"release":1}}`
	assert.Equal(t, preview{err: previewHidden}, diffObjects([]byte(bad), []byte(plain), true))
	assert.Equal(t, preview{err: previewHidden}, diffObjects([]byte(plain), []byte(bad), true))
}

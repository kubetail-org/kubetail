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

package gcpproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// answer is a 200 from upstream whose JSON body is body, sent with no
// length.
func answer(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json; charset=UTF-8"}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: -1,
		Request: &http.Request{Method: http.MethodGet},
	}
}

// rewritten is body as svc's rewriter passes it on, its length dropped.
func rewritten(t *testing.T, svc, body string) string {
	t.Helper()
	resp := answer(body)
	resp.ContentLength = int64(len(body))
	require.NoError(t, rewriteAnswer(serviceNamed(svc), resp))
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), resp.ContentLength)
	return string(out)
}

const metadata = `{"fingerprint":"f","items":[{"key":"startup-script","value":"export TOKEN=s3cret"},{"key":"ssh-keys","value":"k"}]}`

func TestInstanceMetadataIsBlanked(t *testing.T) {
	const blanked = `{"fingerprint":"f","items":[{"key":"startup-script","value":"[redacted]"},{"key":"ssh-keys","value":"[redacted]"}]}`
	cases := map[string]struct{ in, want string }{
		"an instance": {
			`{"kind":"compute#instance","name":"i","id":"1234567890123456789","metadata":` + metadata + `}`,
			`{"kind":"compute#instance","name":"i","id":"1234567890123456789","metadata":` + blanked + `}`,
		},
		"a list": {
			`{"kind":"compute#instanceList","items":[{"name":"a","metadata":` + metadata + `},{"name":"b"}],"nextPageToken":"n"}`,
			`{"kind":"compute#instanceList","items":[{"metadata":` + blanked + `,"name":"a"},{"name":"b"}],"nextPageToken":"n"}`,
		},
		"an aggregated list": {
			`{"items":{"zones/z":{"instances":[{"name":"a","metadata":` + metadata + `}]}}}`,
			`{"items":{"zones/z":{"instances":[{"metadata":` + blanked + `,"name":"a"}]}}}`,
		},
		"a template": {
			`{"name":"t","properties":{"metadata":` + metadata + `}}`,
			`{"name":"t","properties":{"metadata":` + blanked + `}}`,
		},
		"a machine image": {
			`{"name":"m","instanceProperties":{"metadata":` + metadata + `},"sourceInstanceProperties":{"metadata":` + metadata + `}}`,
			`{"name":"m","instanceProperties":{"metadata":` + blanked + `},"sourceInstanceProperties":{"metadata":` + blanked + `}}`,
		},
		"the project": {
			`{"name":"p","commonInstanceMetadata":` + metadata + `}`,
			`{"name":"p","commonInstanceMetadata":` + blanked + `}`,
		},
		"a VPN tunnel": {
			`{"name":"v","sharedSecret":"psk","sharedSecretHash":"h"}`,
			`{"name":"v","sharedSecret":"[redacted]","sharedSecretHash":"h"}`,
		}, "zonal instance settings, whose items are a map": {
			`{"kind":"compute#instanceSettings","metadata":{"kind":"compute#instanceSettingsMetadata","items":{"startup-script":"s3cret","k":"v"}}}`,
			`{"kind":"compute#instanceSettings","metadata":{"kind":"compute#instanceSettingsMetadata","items":{"startup-script":"[redacted]","k":"[redacted]"}}}`,
		},
		"a per-instance config's preserved state": {
			`{"items":[{"name":"i","preservedState":{"metadata":{"startup-script":"s3cret","kind":"x"}}}]}`,
			`{"items":[{"name":"i","preservedState":{"metadata":{"startup-script":"[redacted]","kind":"[redacted]"}}}]}`,
		},
		"an instance group's all-instances config": {
			`{"name":"g","allInstancesConfig":{"properties":{"metadata":{"startup-script":"s3cret"}}}}`,
			`{"name":"g","allInstancesConfig":{"properties":{"metadata":{"startup-script":"[redacted]"}}}}`,
		},
		"partner metadata": {
			`{"name":"i","partnerMetadata":{"example.com":{"entries":{"token":"s3cret","nested":{"a":1}}},"odd.example":"s3cret"}}`,
			`{"name":"i","partnerMetadata":{"example.com":{"entries":{"token":"[redacted]","nested":"[redacted]"}},"odd.example":"[redacted]"}}`,
		},
		"a plain map whose keys are kind and fingerprint": {
			`{"name":"g","allInstancesConfig":{"properties":{"metadata":{"kind":"s3cret","fingerprint":"s3cret"}}}}`,
			`{"name":"g","allInstancesConfig":{"properties":{"metadata":{"kind":"[redacted]","fingerprint":"[redacted]"}}}}`,
		},
		"an empty metadata, blanked whole": {
			`{"name":"i","metadata":{"kind":"compute#metadata","fingerprint":"f"}}`,
			`{"name":"i","metadata":{"kind":"[redacted]","fingerprint":"[redacted]"}}`,
		},
		"a subnetwork's log setting, an enum": {
			`{"name":"s","logConfig":{"enable":true,"metadata":"INCLUDE_ALL_METADATA"}}`,
			`{"name":"s","logConfig":{"enable":true,"metadata":"INCLUDE_ALL_METADATA"}}`,
		},
	}
	for name, tc := range cases {
		assert.JSONEq(t, tc.want, rewritten(t, "compute", tc.in), name)
	}
}

func TestMasterAuthSecretsAreBlanked(t *testing.T) {
	cluster := `{"name":"dev","masterAuth":{"clusterCaCertificate":"ca","clientCertificate":"cert","clientKey":"key","username":"admin","password":"pw"}}`
	want := `{"name":"dev","masterAuth":{"clusterCaCertificate":"ca","clientCertificate":"cert","clientKey":"[redacted]","username":"admin","password":"[redacted]"}}`
	assert.JSONEq(t, want, rewritten(t, "container", cluster))
	assert.JSONEq(t, `{"clusters":[`+want+`]}`, rewritten(t, "container", `{"clusters":[`+cluster+`]}`))
}

func TestNodeMetadataIsBlanked(t *testing.T) {
	config := `{"machineType":"e2-medium","metadata":{"disable-legacy-endpoints":"true","startup":"s3cret"}}`
	blanked := `{"machineType":"e2-medium","metadata":{"disable-legacy-endpoints":"[redacted]","startup":"[redacted]"}}`
	pool := `{"name":"np","config":` + config + `}`
	poolBlanked := `{"name":"np","config":` + blanked + `}`
	cluster := `{"name":"dev","nodeConfig":` + config + `,"nodePools":[` + pool + `]}`
	clusterBlanked := `{"name":"dev","nodeConfig":` + blanked + `,"nodePools":[` + poolBlanked + `]}`
	assert.JSONEq(t, clusterBlanked, rewritten(t, "container", cluster))
	assert.JSONEq(t, `{"clusters":[`+clusterBlanked+`]}`, rewritten(t, "container", `{"clusters":[`+cluster+`]}`))
	assert.JSONEq(t, poolBlanked, rewritten(t, "container", pool), "a node pool read alone")
	assert.JSONEq(t, `{"nodePools":[`+poolBlanked+`]}`, rewritten(t, "container", `{"nodePools":[`+pool+`]}`))
}

func TestARewrittenAnswerKeepsItsText(t *testing.T) {
	assert.JSONEq(t, `{"a":"<b>&","n":12345678901234567890,"e":[],"o":{}}`,
		rewritten(t, "compute", `{"a":"<b>&","n":12345678901234567890,"e":[],"o":{}}`))
	assert.Equal(t, `{"a":"<b>&"}`, strings.TrimSpace(rewritten(t, "compute", `{"a":"<b>&"}`)), "no HTML escaping")
}

func TestAnAnswerThatCannotBeRewrittenFailsClosed(t *testing.T) {
	r := newRig(t, roomy)
	instance := "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances/i"

	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "metadata: s3cret")
	}
	refusal(t, r.get(t, instance))
	testutil.Recv(t, r.up.seen, "the text answer")

	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, "compressed")
	}
	refusal(t, r.get(t, instance))
	testutil.Recv(t, r.up.seen, "the encoded answer")

	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Past a piece, so the status line has gone when the body is cut.
		_, _ = io.WriteString(w, `{"items":[{"name":"`+strings.Repeat("a", pieceSize)+`"},{"name":"b","metadata":{"items":[{"key":"k","val`)
	}
	resp := r.get(t, instance+"s")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.Error(t, err, "a body cut short")
	assert.NotContains(t, string(body), `"b"`)
	testutil.Recv(t, r.up.seen, "the cut answer")

	refusal(t, r.get(t, instance+"?alt=media"))
	refusal(t, r.get(t, instance+"?alt=json&alt=proto"))
	refusal(t, r.get(t, instance+"?ALT=proto"))
	refusal(t, r.get(t, instance+"?$alt=proto"))
	assert.Empty(t, r.up.seen)
}

func TestARewrittenServiceAsksForPlainJSON(t *testing.T) {
	r := newRig(t, roomy)
	resp := r.send(t, http.MethodGet, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p", r.grant.Token(), nil,
		http.Header{"Accept-Encoding": {"br"}, "Range": {"bytes=0-10"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	s := testutil.Recv(t, r.up.seen, "the request")
	assert.Empty(t, s.header.Values("Range"))
	assert.NotEqual(t, "br", s.header.Get("Accept-Encoding"))
}

func TestAnAnswerWithNoBodyPasses(t *testing.T) {
	r := newRig(t, roomy)
	r.up.answer = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	resp := r.send(t, http.MethodHead, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p", r.grant.Token(), nil, nil)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestAnInstanceReadThroughTheProxyIsBlanked(t *testing.T) {
	r := newRig(t, roomy)
	r.up.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"i","metadata":`+metadata+`}`)
	}
	body := readAll(t, r.get(t, "http://compute.gcp.kubetail.invalid/compute/v1/projects/p/zones/z/instances/i"))
	assert.NotContains(t, body, "s3cret")
	assert.Contains(t, body, "startup-script")
}

// A body that fails before its first piece is refused whole, whatever part
// of it the rewriter was reading.
func TestAnAnswerThatIsNotOneObjectIsUnreadable(t *testing.T) {
	for _, body := range []string{
		`[1]`, `{} {}`, `{"a":1,}`, `{"a":`, `{"a":{"b":`, `{"a":1`, `{"items":[1`, `{"items":[1,}`,
		`{"a":{`, `{"a":{"k"`, `{"a":{"k":{`, `{"a":[`, `{"a":[1,`, `{"a":[[`, ``,
		`{"a":{"k":1,}}`, `{"a":[1,]}`, `{"a":{"b":[1,]}}`, `{"a":{"b":[{"k":1,}]}}`, `{"a":{"b":{"k":1,}}}`,
		`{"metadata":{"k":1,}}`, `{"metadata":{`, `{"metadata":{"k"`, `{"metadata":{"k":{`,
		`{"metadata":[`, `{"metadata":[1,]}`, `{"metadata":[{"k":1,}]}`,
	} {
		assert.ErrorIs(t, rewriteAnswer(serviceNamed("compute"), answer(body)), errUnreadable, body)
	}
}

// closeSignal is a body that says when it is closed.
type closeSignal struct {
	io.Reader
	closed chan struct{}
}

func (c closeSignal) Close() error {
	close(c.closed)
	return nil
}

func TestAClientThatLeavesStopsTheRewrite(t *testing.T) {
	body := closeSignal{
		Reader: strings.NewReader(`{"items":[{"name":"` + strings.Repeat("a", pieceSize) + `"},{"name":"b"}]}`),
		closed: make(chan struct{}),
	}
	resp := answer("")
	resp.Body = body
	require.NoError(t, rewriteAnswer(serviceNamed("compute"), resp))

	require.NoError(t, resp.Body.Close())

	testutil.Wait(t, body.closed, "the rewriter letting go of the upstream's body")
}

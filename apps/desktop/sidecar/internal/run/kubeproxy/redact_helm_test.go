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
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseValue is release as a release Secret's data.release holds it: helm's
// base64, of gzip when zipped, of the JSON, then the base64 of any Secret value.
func releaseValue(t *testing.T, release string, zipped bool) string {
	t.Helper()
	body := []byte(release)
	if zipped {
		body = gzipped(t, body)
	}
	helm := base64.StdEncoding.EncodeToString(body)
	return base64.StdEncoding.EncodeToString([]byte(helm))
}

// gzipped is body as one gzip stream.
func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(body)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// releaseBytes is a data.release value's JSON, read as helm's decodeRelease
// reads it: both base64 layers, then gzip when its magic bytes lead.
func releaseBytes(t *testing.T, value string) []byte {
	t.Helper()
	helm, err := base64.StdEncoding.DecodeString(value)
	require.NoError(t, err)
	body, err := base64.StdEncoding.DecodeString(string(helm))
	require.NoError(t, err)
	if bytes.HasPrefix(body, []byte{0x1f, 0x8b, 0x08}) {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		require.NoError(t, err)
		body, err = io.ReadAll(zr)
		require.NoError(t, err)
	}
	return body
}

// releaseOf is a data.release value's release, its top level alone parsed.
func releaseOf(t *testing.T, value string) map[string]json.RawMessage {
	t.Helper()
	var release map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(releaseBytes(t, value), &release))
	return release
}

const (
	testChart = `{"metadata":{"name":"web","version":"1.2.0"},"values":{"image":"nginx"},"templates":[{"name":"templates/a.yaml","data":"a2luZDogQ29uZmlnTWFw"}]}`
	testInfo  = `{"status":"deployed","notes":"Visit <http://web>"}`
)

// Every scalar of config reads [redacted], its keys and array lengths kept, and
// the chart and info pass byte for byte, zipped or not.
func TestAHelmReleasesValuesAreRedacted(t *testing.T) {
	release := `{"name":"web","version":3,"chart":` + testChart + `,"info":` + testInfo + `,
		"config":{"password":"hunter2","replicas":3,"tls":{"enabled":true,"hosts":["a.example","b.example"]},"extra":null}}`
	for name, zipped := range map[string]bool{"gzip": true, "plain": false} {
		t.Run(name, func(t *testing.T) {
			got, err := redactRelease(releaseValue(t, release, zipped))
			require.NoError(t, err)

			r := releaseOf(t, got)
			assert.JSONEq(t, `{"password":"[redacted]","replicas":"[redacted]",
				"tls":{"enabled":"[redacted]","hosts":["[redacted]","[redacted]"]},"extra":"[redacted]"}`, string(r["config"]))
			assert.Equal(t, testChart, string(r["chart"]))
			assert.Equal(t, testInfo, string(r["info"]))
			assert.Equal(t, `"web"`, string(r["name"]))
			assert.Equal(t, `3`, string(r["version"]))
		})
	}
}

// A release whose config, manifest and hooks are null reads back with them
// null.
func TestAReleasesNullFieldsPass(t *testing.T) {
	got, err := redactRelease(releaseValue(t, `{"name":"web","config":null,"manifest":null,"hooks":null}`, true))
	require.NoError(t, err)

	r := releaseOf(t, got)
	assert.Equal(t, "null", string(r["config"]))
	assert.Equal(t, "null", string(r["manifest"]))
	assert.Equal(t, "null", string(r["hooks"]))
}

// manifestOf is a release field that is a string, decoded.
func manifestOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	require.NoError(t, json.Unmarshal(raw, &s))
	return s
}

// A Secret in the manifest, in a List's items, after a separator helm reads
// and in a hook's manifest has its values redacted; a redacted document keeps
// its # Source: line and no other comment; the other documents and every
// separator keep their bytes.
// A manifest Secret's last-applied-configuration, which holds the Secret
// whole, reads [redacted], in a List's items too.
func TestAHelmManifestsLastAppliedIsRedacted(t *testing.T) {
	const lastApplied = `{"apiVersion":"v1","kind":"Secret","data":{"password":"aHVudGVyMg=="}}`
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  annotations:\n    kubectl.kubernetes.io/last-applied-configuration: " + marshaled(lastApplied) + "\n" +
		"---\napiVersion: v1\nkind: List\nitems:\n- kind: Secret\n  metadata:\n    annotations:\n      kubectl.kubernetes.io/last-applied-configuration: " + marshaled(lastApplied) + "\n"

	got, err := redactRelease(releaseValue(t, `{"manifest":`+marshaled(manifest)+`}`, true))
	require.NoError(t, err)

	m := manifestOf(t, releaseOf(t, got)["manifest"])
	assert.NotContains(t, m, "aHVudGVyMg==")
	assert.Equal(t, 2, strings.Count(m, "kubectl.kubernetes.io/last-applied-configuration: '[redacted]'"), m)
}

// A SecretList's items are Secrets whether or not they name their kind, since
// Kubernetes infers it from the list.
func TestASecretListsItemsAreRedacted(t *testing.T) {
	manifest := "apiVersion: v1\nkind: SecretList\nitems:\n- metadata:\n    name: db\n  data:\n    password: aHVudGVyMg==\n- kind: \"\"\n  metadata:\n    name: api\n  stringData:\n    token: t0ken\n"

	got, err := redactRelease(releaseValue(t, `{"manifest":`+marshaled(manifest)+`,"hooks":[{"manifest":`+marshaled(manifest)+`}]}`, true))
	require.NoError(t, err)

	r := releaseOf(t, got)
	var hooks []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r["hooks"], &hooks))
	for _, m := range []string{manifestOf(t, r["manifest"]), manifestOf(t, hooks[0]["manifest"])} {
		assert.NotContains(t, m, "aHVudGVyMg==")
		assert.NotContains(t, m, "t0ken")
		assert.Contains(t, m, "password: W3JlZGFjdGVkXQ==")
		assert.Contains(t, m, "token: '[redacted]'")
	}
}

func TestAHelmReleasesSecretsAreRedacted(t *testing.T) {
	const configMap = "# Source: web/templates/cm.yaml\napiVersion: v1\nkind: ConfigMap\ndata:\n  password: \"hunter2\"   # a comment"
	manifest := "---\n" + configMap + "\n---\n# Source: web/templates/secret.yaml\n# password: hunter2\napiVersion: v1\nkind: Secret\nmetadata:\n  name: db\ndata:\n  password: aHVudGVyMg==\nstringData:\n  user: admin\n" +
		"--- # a list\napiVersion: v1\nkind: List\nitems:\n- apiVersion: v1\n  kind: Secret\n  metadata:\n    name: api\n  stringData:\n    token: t0ken\n"
	hook := "# Source: web/templates/hook.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: hook\nstringData:\n  key: s3cret"
	release := `{"name":"web","manifest":` + marshaled(manifest) + `,"hooks":[{"name":"hook","events":["pre-install"],"manifest":` + marshaled(hook) + `}]}`

	got, err := redactRelease(releaseValue(t, release, true))
	require.NoError(t, err)

	r := releaseOf(t, got)
	assert.Equal(t, "---\n"+configMap+"\n---\n# Source: web/templates/secret.yaml\napiVersion: v1\ndata:\n  password: W3JlZGFjdGVkXQ==\nkind: Secret\nmetadata:\n  name: db\nstringData:\n  user: '[redacted]'\n"+
		"--- \napiVersion: v1\nitems:\n- apiVersion: v1\n  kind: Secret\n  metadata:\n    name: api\n  stringData:\n    token: '[redacted]'\nkind: List\n",
		manifestOf(t, r["manifest"]))
	var hooks []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r["hooks"], &hooks))
	require.Len(t, hooks, 1)
	assert.Equal(t, "# Source: web/templates/hook.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: hook\nstringData:\n  key: '[redacted]'", manifestOf(t, hooks[0]["manifest"]))
	assert.Equal(t, `["pre-install"]`, string(hooks[0]["events"]))
}

// A release that does not decode at any layer, or whose shape the rewriter
// cannot read, fails closed.
func TestAReleaseThatDoesNotDecodeFailsClosed(t *testing.T) {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	zipped := gzipped(t, make([]byte, 1<<10))

	for name, value := range map[string]string{
		"bad outer base64":      "not base64!",
		"bad inner base64":      b64("not base64!"),
		"bad gzip":              b64(b64("\x1f\x8b\x08garbage")),
		"bad JSON":              releaseValue(t, `{"name":`, true),
		"not an object":         releaseValue(t, `null`, true),
		"config not object":     releaseValue(t, `{"config":["hunter2"]}`, true),
		"manifest not string":   releaseValue(t, `{"manifest":{"kind":"Secret"}}`, true),
		"hooks not a list":      releaseValue(t, `{"hooks":{"manifest":""}}`, true),
		"hook not an object":    releaseValue(t, `{"hooks":["x"]}`, true),
		"not YAML":              releaseValue(t, `{"manifest":`+marshaled("kind: [Secret")+`}`, true),
		"a second document":     releaseValue(t, `{"manifest":`+marshaled("kind: ConfigMap\n...\nkind: Secret\ndata:\n  a: aHVudGVyMg==\n")+`}`, true),
		"stringData a string":   releaseValue(t, `{"manifest":`+marshaled("kind: Secret\nstringData: hunter2\n")+`}`, true),
		"data a string":         releaseValue(t, `{"manifest":`+marshaled("kind: Secret\ndata: aHVudGVyMg==\n")+`}`, true),
		"a nested bad Secret":   releaseValue(t, `{"manifest":`+marshaled("kind: Wrapper\nspec:\n  kind: Secret\n  data: x\n")+`}`, true),
		"a listed bad Secret":   releaseValue(t, `{"manifest":`+marshaled("kind: List\nitems:\n- kind: Secret\n  data: x\n")+`}`, true),
		"a bad first doc":       releaseValue(t, `{"manifest":`+marshaled("kind: [Secret\n---\nkind: ConfigMap\n")+`}`, true),
		"a value JSON lacks":    releaseValue(t, `{"manifest":`+marshaled("kind: Secret\na: .inf\n")+`}`, true),
		"a bad SecretList item": releaseValue(t, `{"manifest":`+marshaled("kind: SecretList\nitems:\n- data: x\n")+`}`, true),
		"a null hook":           releaseValue(t, `{"hooks":[null]}`, true),
		"a bad hook manifest":   releaseValue(t, `{"hooks":[{"manifest":`+marshaled("kind: [Secret")+`}]}`, true),
		"a cut gzip":            b64(base64.StdEncoding.EncodeToString(zipped[:len(zipped)/2])),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := redactRelease(value)
			assert.ErrorIs(t, err, errUnredactable)
		})
	}
}

// A release that inflates past the limit fails closed; one at it inflates.
func TestAReleasePastTheLimitFailsClosed(t *testing.T) {
	const size = 1 << 10
	zipped := gzipped(t, make([]byte, size))

	_, err := gunzip(zipped, size-1)
	assert.ErrorIs(t, err, errUnredactable)
	out, err := gunzip(zipped, size)
	require.NoError(t, err)
	assert.Len(t, out, size)
}

// The walk redacts a helm release Secret's data.release inside, and its other
// keys as a Secret's; a release that is not a string fails closed.
func TestTheWalkRedactsAHelmReleaseInside(t *testing.T) {
	release := releaseValue(t, `{"name":"web","config":{"password":"hunter2"}}`, true)
	v := decoded(t, `{"metadata":{"name":"sh.helm.release.v1.web.v1"},"type":"helm.sh/release.v1",
		"data":{"release":`+marshaled(release)+`,"other":"aHVudGVyMg=="}}`)

	require.NoError(t, redact(v))

	data := v.(map[string]any)["data"].(map[string]any)
	assert.Equal(t, redactedData, data["other"])
	assert.JSONEq(t, `{"password":"[redacted]"}`, string(releaseOf(t, data["release"].(string))["config"]))

	odd := decoded(t, `{"metadata":{},"type":"helm.sh/release.v1","data":{"release":7}}`)
	assert.ErrorIs(t, redact(odd), errUnredactable)
}

// helmRelease is the fields of a release helm reads to list and show.
type helmRelease struct {
	Name string `json:"name"`
	Info struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"metadata"`
		Values map[string]any `json:"values"`
	} `json:"chart"`
	Config   map[string]any `json:"config"`
	Manifest string         `json:"manifest"`
	Hooks    []struct {
		Name     string   `json:"name"`
		Manifest string   `json:"manifest"`
		Events   []string `json:"events"`
	} `json:"hooks"`
	Version   int    `json:"version"`
	Namespace string `json:"namespace"`
}

// helmReleaseOf is a data.release value's release, in the fields helm reads.
func helmReleaseOf(t *testing.T, value string) helmRelease {
	t.Helper()
	var r helmRelease
	require.NoError(t, json.Unmarshal(releaseBytes(t, value), &r))
	return r
}

// A release Secret in helm's own shape (testdata/helm-release.json, a chart
// with a Secret and a hook, written by hand in the shape helm 3 stores) reads
// back through helm's decoding, its values and Secrets redacted and the rest
// as helm wrote it.
func TestHelmReadsARedactedRelease(t *testing.T) {
	fixture, err := os.ReadFile("testdata/helm-release.json")
	require.NoError(t, err)
	s := serve(t, answering(t, string(fixture)).upstream())

	resp, body := s.send(t, "GET", "/api/v1/namespaces/web/secrets/sh.helm.release.v1.web.v1", s.g.Token())

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	var secret struct {
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &secret))
	assert.Equal(t, helmReleaseType, secret.Type)
	r := helmReleaseOf(t, secret.Data["release"])
	assert.Equal(t, "web", r.Name)
	assert.Equal(t, 1, r.Version)
	assert.Equal(t, "web", r.Namespace)
	assert.Equal(t, "deployed", r.Info.Status)
	assert.Equal(t, "Visit http://web.example", r.Info.Notes)
	assert.Equal(t, "web", r.Chart.Metadata.Name)
	assert.Equal(t, "0.1.0", r.Chart.Metadata.Version)
	assert.Equal(t, map[string]any{"password": "[redacted]", "replicas": "[redacted]", "hosts": []any{"[redacted]", "[redacted]"}}, r.Config)
	assert.Contains(t, r.Manifest, "# Source: web/templates/configmap.yaml\napiVersion: v1\nkind: ConfigMap\n")
	assert.Contains(t, r.Manifest, "password: W3JlZGFjdGVkXQ==")
	assert.NotContains(t, r.Manifest, "aHVudGVyMg==")
	require.Len(t, r.Hooks, 1)
	assert.Equal(t, []string{"pre-install"}, r.Hooks[0].Events)
	assert.Contains(t, r.Hooks[0].Manifest, "token: '[redacted]'")
	assert.NotContains(t, r.Hooks[0].Manifest, "s3cret-token")
	assert.NotContains(t, body, "hunter2")
}

// A get of a release Secret whose type follows its data, as the API server
// orders it, has data.release redacted inside, not replaced.
func TestAGetOfAHelmReleaseIsRedactedInside(t *testing.T) {
	release := releaseValue(t, `{"name":"web","config":{"password":"hunter2"}}`, true)
	s := serve(t, answering(t, `{"kind":"Secret","metadata":{"name":"r"},"data":{"release":`+marshaled(release)+`},"type":"helm.sh/release.v1"}`).upstream())

	resp, body := s.send(t, "GET", "/api/v1/namespaces/web/secrets/r", s.g.Token())

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	var secret struct {
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &secret))
	assert.Equal(t, map[string]any{"password": "[redacted]"}, helmReleaseOf(t, secret.Data["release"]).Config)
}

// marshaled is s as a JSON string.
func marshaled(s string) string { return string(marshal(s)) }

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
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// decoded is s decoded as the rewriter decodes a value.
func decoded(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	require.NoError(t, dec.Decode(&v))
	return v
}

// A map holding metadata has each data value replaced by the base64 of
// [redacted]; a map without metadata is left alone.
func TestTheWalkRedactsASecretsData(t *testing.T) {
	v := decoded(t, `{"kind":"SecretList","items":[
		{"metadata":{"name":"a"},"type":"Opaque","data":{"password":"aHVudGVyMg==","user":"YWRtaW4="}},
		{"data":{"password":"aHVudGVyMg=="}}
	]}`)

	require.NoError(t, redact(v))

	assert.Equal(t, decoded(t, `{"kind":"SecretList","items":[
		{"metadata":{"name":"a"},"type":"Opaque","data":{"password":"W3JlZGFjdGVkXQ==","user":"W3JlZGFjdGVkXQ=="}},
		{"data":{"password":"aHVudGVyMg=="}}
	]}`), v)
}

// stringData, plain text, reads [redacted]; a null data passes; a data or
// stringData that is neither a map nor null fails closed.
func TestTheWalkRedactsStringDataAndRefusesAnOddShape(t *testing.T) {
	v := decoded(t, `{"metadata":{},"data":null,"stringData":{"password":"hunter2"}}`)
	require.NoError(t, redact(v))
	assert.Equal(t, decoded(t, `{"metadata":{},"data":null,"stringData":{"password":"[redacted]"}}`), v)

	for _, body := range []string{
		`{"metadata":{},"data":"aHVudGVyMg=="}`,
		`{"metadata":{},"stringData":["hunter2"]}`,
	} {
		assert.ErrorIs(t, redact(decoded(t, body)), errUnredactable, body)
	}
}

// Accept keeps only its application/json types, parameters and all, and
// Accept-Encoding goes.
func TestTheRequestAsksForJSON(t *testing.T) {
	for accept, want := range map[string]string{
		"application/json;as=Table;v=v1;g=meta.k8s.io,application/json;as=Table;v=v1beta1;g=meta.k8s.io,application/json": "application/json;as=Table;v=v1;g=meta.k8s.io,application/json;as=Table;v=v1beta1;g=meta.k8s.io,application/json",
		"application/vnd.kubernetes.protobuf, application/json":                                                           "application/json",
		"application/cbor, application/yaml, */*, application/*":                                                          "application/json",
		"Application/JSON; stream=watch":                                                                                  "Application/JSON; stream=watch",
		"":                                                                                                                "application/json",
		"not a type;;":                                                                                                    "application/json",
	} {
		h := http.Header{"Accept-Encoding": {"gzip"}}
		if accept != "" {
			h.Set("Accept", accept)
		}
		askForJSON(h)
		assert.Equal(t, want, h.Get("Accept"), accept)
		assert.Empty(t, h.Values("Accept-Encoding"))
	}
}

// last-applied-configuration reads [redacted] in any map, whatever its kind:
// a table row's PartialObjectMetadata carries a Secret's in the clear.
func TestTheWalkRedactsLastAppliedWherever(t *testing.T) {
	const lastApplied = `{"apiVersion":"v1","kind":"Secret","data":{"password":"aHVudGVyMg=="}}`
	v := decoded(t, `{"kind":"Table","rows":[{"cells":["a"],"object":{
		"kind":"PartialObjectMetadata","metadata":{"name":"a","annotations":{
			"kubectl.kubernetes.io/last-applied-configuration":`+strconv.Quote(lastApplied)+`,"team":"web"}}}}]}`)

	require.NoError(t, redact(v))

	assert.Equal(t, decoded(t, `{"kind":"Table","rows":[{"cells":["a"],"object":{
		"kind":"PartialObjectMetadata","metadata":{"name":"a","annotations":{
			"kubectl.kubernetes.io/last-applied-configuration":"[redacted]","team":"web"}}}}]}`), v)
}

// secretJSON is a Secret as the API server answers it.
const secretJSON = `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"db","namespace":"web"},"type":"Opaque","data":{"password":"aHVudGVyMg=="}}`

// redactedSecretJSON is secretJSON redacted.
const redactedSecretJSON = `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"db","namespace":"web"},"type":"Opaque","data":{"password":"W3JlZGFjdGVkXQ=="}}`

// answering is an API server that answers every request with body as JSON,
// gzipped when the request asks for it.
func answering(t *testing.T, body string) *apiServer {
	t.Helper()
	return newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Accept-Encoding") != "gzip" {
			_, _ = io.WriteString(w, body)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = io.WriteString(zw, body)
		_ = zw.Close()
	})
}

// A get of a Secret reaches the client redacted: the request asks for JSON
// alone, and the transport's own gzip is read plain.
func TestAGetOfASecretIsRedacted(t *testing.T) {
	api := answering(t, secretJSON)
	s := serve(t, api.upstream())
	req := s.request(t, "GET", "/api/v1/namespaces/web/secrets/db", s.g.Token())
	req.Header.Set("Accept", "application/vnd.kubernetes.protobuf, application/json")
	req.Header.Set("Accept-Encoding", "br")

	resp, body := s.do(t, req)

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, redactedSecretJSON, body)
	assert.Empty(t, resp.Header.Get("Content-Length"))
	seen := api.requests()
	require.Len(t, seen, 1)
	assert.Equal(t, "application/json", seen[0].Header.Get("Accept"))
	assert.Equal(t, "gzip", seen[0].Header.Get("Accept-Encoding"), "the transport's own, not the client's")
}

// An answer that is not plain JSON is a 502 in its place: none of its bytes
// reach the client, and its body is closed unread.
func TestANonJSONAnswerFailsClosed(t *testing.T) {
	for name, header := range map[string]http.Header{
		"protobuf": {"Content-Type": {"application/vnd.kubernetes.protobuf"}},
		"encoded":  {"Content-Type": {"application/json"}, "Content-Encoding": {"br"}},
		"untyped":  {},
	} {
		t.Run(name, func(t *testing.T) {
			closed := testutil.NewSignal()
			api := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
				maps.Copy(w.Header(), header)
				_, _ = io.WriteString(w, secretJSON)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				closed.Fire()
			})
			s := serve(t, api.upstream())

			resp, body := s.send(t, "GET", "/api/v1/namespaces/web/secrets/db", s.g.Token())

			assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
			assert.Equal(t, unredactable, statusOf(t, body).Message)
			assert.NotContains(t, body, "aHVudGVyMg==")
			closed.Wait(t, "the upstream body closed")
		})
	}
}

// readUntil reads r until what it has read holds want, and answers it.
func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	var got []byte
	buf := make([]byte, 512)
	for !strings.Contains(string(got), want) {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		require.NoError(t, err, "read %q before %q", got, want)
	}
	return string(got)
}

// A list reaches the client an item at a time: the first item before the
// server has sent the second.
func TestAListIsReadAnItemAtATime(t *testing.T) {
	next := make(chan struct{})
	api := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"SecretList","apiVersion":"v1","metadata":{"resourceVersion":"7"},"items":[`+secretJSON)
		w.(http.Flusher).Flush()
		<-next
		_, _ = io.WriteString(w, `,`+strings.Replace(secretJSON, `"db"`, `"api"`, 1)+`]}`)
	})
	s := serve(t, api.upstream())
	resp, err := s.client.Do(s.request(t, "GET", "/api/v1/namespaces/web/secrets", s.g.Token()))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	first := readUntil(t, resp.Body, `"name":"db"`)
	close(next)
	rest, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.JSONEq(t, `{"kind":"SecretList","apiVersion":"v1","metadata":{"resourceVersion":"7"},"items":[`+
		redactedSecretJSON+`,`+strings.Replace(redactedSecretJSON, `"db"`, `"api"`, 1)+`]}`, first+string(rest))
}

// An empty list, one whose items are null, and one whose items come before
// its metadata read back whole.
func TestAListOfAnyShapeReadsBackWhole(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"empty":                {`{"kind":"SecretList","items":[]}`, `{"kind":"SecretList","items":[]}`},
		"null items":           {`{"kind":"SecretList","items":null}`, `{"kind":"SecretList","items":null}`},
		"items first":          {`{"items":[` + secretJSON + `],"kind":"SecretList","metadata":{}}`, `{"items":[` + redactedSecretJSON + `],"kind":"SecretList","metadata":{}}`},
		"arrays only":          {`{"items":[],"rows":[1]}`, `{"items":[],"rows":[1]}`},
		"top-level":            {`[` + secretJSON + `]`, `[` + redactedSecretJSON + `]`},
		"a scalar":             {`"a"`, `"a"`},
		"nothing":              {``, ``},
		"data before metadata": {`{"items":[],"data":{"a":"aHVudGVyMg=="},"metadata":{}}`, `{"items":[],"metadata":{},"data":{"a":"W3JlZGFjdGVkXQ=="}}`},
	} {
		t.Run(name, func(t *testing.T) {
			s := serve(t, answering(t, c.body).upstream())
			resp, body := s.send(t, "GET", "/api/v1/secrets", s.g.Token())
			require.Equal(t, http.StatusOK, resp.StatusCode, body)
			if c.want == "" {
				assert.Empty(t, body)
				return
			}
			assert.JSONEq(t, c.want, body)
		})
	}
}

// badRelease is a helm release Secret whose release does not decode.
const badRelease = `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"sh.helm.release.v1.web.v1"},"type":"helm.sh/release.v1","data":{"release":"bm90IGJhc2U2NCE="}}`

// A value the rewriter cannot read answers 502 when it comes before the
// status line, and cuts the body after it; none of it reaches the client.
func TestAnUnreadableValueFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"a bad release":       badRelease,
		"data not a map":      `{"metadata":{},"data":"aHVudGVyMg=="}`,
		"data an array":       `{"metadata":{},"data":["hunter2"]}`,
		"stringData an array": `{"metadata":{},"stringData":["hunter2"]}`,
		"a first bad item":    `{"kind":"SecretList","items":[` + badRelease + `]}`,
		"not JSON":            `{"metadata":`,
		"a bad key":           `{1:2}`,
		"a bad value":         `{"a":}`,
		"a bad inner value":   `{"a":{"b":}}`,
		"an object cut":       `{"a":1`,
		"a bad inner key":     `{"metadata":{1:2}}`,
		"an inner cut":        `{"metadata":{"a":1`,
		"a bad deep value":    `{"metadata":{"a":{"b":}}}`,
		"a bad array value":   `{"metadata":{"a":[1,}}}`,
		"a bad array inner":   `{"metadata":{"a":[{"b":}]}}`,
		"an array cut":        `{"metadata":{"a":[1`,
		"a bad item":          `{"items":[{"a":}]}`,
		"items cut":           `{"items":[`,
		"a nested bad list":   `{"object":{"list":[{"metadata":{},"data":"aHVudGVyMg=="}]}}`,
		"a bad table value":   `{"kind":"Table","rows":[{"object":{"metadata":{},"data":"aHVudGVyMg=="}}]}`,
	} {
		t.Run("before: "+name, func(t *testing.T) {
			s := serve(t, answering(t, body).upstream())
			resp, got := s.send(t, "GET", "/api/v1/secrets", s.g.Token())
			assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
			assert.Equal(t, unredactable, statusOf(t, got).Message)
		})
	}
	for name, body := range map[string]string{
		"a bad release":     `{"kind":"SecretList","items":[` + secretJSON + `,` + badRelease + `]}`,
		"a stream cut":      `{"kind":"SecretList","items":[` + secretJSON + `,{"metadata":{"name":"api"},"data":{"password":"aHVu`,
		"a bad second item": `{"kind":"SecretList","items":[` + secretJSON + `,{"metadata":{},"data":"aHVudGVyMg=="}]}`,
	} {
		t.Run("after: "+name, func(t *testing.T) {
			s := serve(t, answering(t, body).upstream())
			resp, err := s.client.Do(s.request(t, "GET", "/api/v1/secrets", s.g.Token()))
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			got, err := io.ReadAll(resp.Body)
			assert.Error(t, err, "the body is cut")
			assert.Contains(t, string(got), `"name":"db"`, "the value before it")
			assert.NotContains(t, string(got), "aHVu")
			assert.NotContains(t, string(got), "bm90IGJhc2U2NCE=")
		})
	}
}

// secretWatch is an API server that answers with a JSON stream, sending each
// line it is handed on next, flushed at once, until the request ends, and
// ended gets a value when it does.
func secretWatch(t *testing.T) (api *apiServer, next chan string, ended chan struct{}) {
	t.Helper()
	next, ended = make(chan string), make(chan struct{}, 8)
	api = newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		defer func() { ended <- struct{}{} }()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case line := <-next:
				_, _ = io.WriteString(w, line+"\n")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	return api, next, ended
}

// event is a watch event of a Secret.
func event(kind, secret string) string { return `{"type":"` + kind + `","object":` + secret + `}` }

// A watch, however it is asked for, answers at once, and each event reaches
// the client redacted before the next is sent.
func TestEachWatchEventArrivesAlone(t *testing.T) {
	for _, path := range []string{
		"/api/v1/namespaces/web/secrets?watch=true",
		"/api/v1/namespaces/web/secrets?watch=1",
		"/api/v1/namespaces/web/secrets?watch=",
		"/api/v1/watch/namespaces/web/secrets",
	} {
		t.Run(path, func(t *testing.T) {
			api, next, _ := secretWatch(t)
			s := serve(t, api.upstream())
			resp, err := s.client.Do(s.request(t, "GET", path, s.g.Token()))
			require.NoError(t, err, "the status line before any event")
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			lines := bufio.NewScanner(resp.Body)

			for _, kind := range []string{"ADDED", "MODIFIED", "DELETED"} {
				next <- event(kind, secretJSON)
				require.True(t, lines.Scan(), lines.Err())
				assert.JSONEq(t, event(kind, redactedSecretJSON), lines.Text())
			}
		})
	}
}

// A request is a watch as the API server reads one: a legacy /watch/, or a
// first watch value that is not 0 or false.
func TestIsWatch(t *testing.T) {
	for query, want := range map[string]bool{
		"watch=true": true, "watch=1": true, "watch=": true, "watch=yes": true,
		"watch=false": false, "watch=FALSE": false, "watch=0": false, "": false,
		"watch=0&watch=1": false,
	} {
		q, err := url.ParseQuery(query)
		require.NoError(t, err)
		assert.Equal(t, want, isWatch(apiPath{}, q), query)
	}
	assert.True(t, isWatch(parsePath("/api/v1/watch/secrets"), nil))
}

// A client leaving a quiet watch ends the rewrite, and the upstream request
// with it.
func TestTheRewriterEndsWithTheClient(t *testing.T) {
	api, next, ended := secretWatch(t)
	s := serve(t, api.upstream())
	resp, err := s.client.Do(s.request(t, "GET", "/api/v1/namespaces/web/secrets?watch=true", s.g.Token()))
	require.NoError(t, err)
	lines := bufio.NewScanner(resp.Body)
	next <- event("ADDED", secretJSON)
	require.True(t, lines.Scan(), lines.Err())

	require.NoError(t, resp.Body.Close())

	testutil.Recv(t, ended, "the upstream request's end")
}

// A table's rows reach the client redacted: a row's whole Secret, and the
// last-applied-configuration a row's PartialObjectMetadata carries.
func TestATableIsRedacted(t *testing.T) {
	const lastApplied = `"kubectl.kubernetes.io/last-applied-configuration":"{\"data\":{\"password\":\"aHVudGVyMg==\"}}"`
	table := `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[{"name":"Name"}],"rows":[
		{"cells":["db","Opaque",1],"object":` + secretJSON + `},
		{"cells":["api","Opaque",1],"object":{"kind":"PartialObjectMetadata","metadata":{"name":"api","annotations":{` + lastApplied + `}}}}]}`
	api := answering(t, table)
	s := serve(t, api.upstream())
	req := s.request(t, "GET", "/api/v1/secrets?includeObject=Object", s.g.Token())
	req.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io,application/json")

	resp, body := s.do(t, req)

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.JSONEq(t, `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[{"name":"Name"}],"rows":[
		{"cells":["db","Opaque",1],"object":`+redactedSecretJSON+`},
		{"cells":["api","Opaque",1],"object":{"kind":"PartialObjectMetadata","metadata":{"name":"api","annotations":{
			"kubectl.kubernetes.io/last-applied-configuration":"[redacted]"}}}}]}`, body)
	assert.Equal(t, "application/json;as=Table;v=v1;g=meta.k8s.io,application/json", api.requests()[0].Header.Get("Accept"))
}

// Only a request on core Secrets is rewritten: a ConfigMap's data, and
// another group's secrets, pass as they came.
func TestOnlySecretsAreRewritten(t *testing.T) {
	const configMap = `{"kind":"ConfigMap","metadata":{"name":"c"},"data":{"password":"hunter2"}}`
	s := serve(t, answering(t, configMap).upstream())
	for _, path := range []string{"/api/v1/namespaces/web/configmaps/c", "/apis/example.com/v1/secrets"} {
		resp, body := s.send(t, "GET", path, s.g.Token())
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, configMap, body, path)
	}
}

// A rewrite whose client is gone ends at the next piece it cannot write, and
// closes the upstream body.
func TestARewriteEndsWhenItsClientIsGone(t *testing.T) {
	for _, body := range []string{`"a" "b"`, `{"items":[1,2]}`} {
		pr, pw := io.Pipe()
		require.NoError(t, pr.Close())
		upstream := &closeRecorder{Reader: strings.NewReader(body)}

		(&redactedBody{pw: pw}).rewrite(upstream)

		assert.True(t, upstream.closed, body)
	}
}

// closeRecorder is a body that records its Close.
type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

// redactLastApplied blanks the last-applied annotation and leaves everything
// else, a ConfigMap's data included.
func TestRedactLastAppliedBlanksTheAnnotationAlone(t *testing.T) {
	var v any
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"ConfigMap","metadata":{"name":"c","annotations":{
		"`+lastApplied+`":"{\"kind\":\"Secret\",\"data\":{\"p\":\"aHVudGVyMg==\"}}","team":"web"}},"data":{"k":"v"}}`), &v))
	redactLastApplied(v)
	b, err := json.Marshal(v)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"ConfigMap","metadata":{"name":"c","annotations":{"`+lastApplied+`":"[redacted]","team":"web"}},"data":{"k":"v"}}`, string(b))
}

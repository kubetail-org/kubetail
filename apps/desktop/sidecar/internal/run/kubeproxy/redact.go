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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// lastApplied is the annotation kubectl apply keeps an object's whole
// manifest in, a Secret's values included.
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration"

// redactedData is a Secret's data value once redacted: the base64 of the
// mark, so a typed client still decodes the object.
var redactedData = base64.StdEncoding.EncodeToString([]byte(safe.Redacted))

// errUnredactable is a value the rewriter cannot read, so none of it passes.
var errUnredactable = errors.New("kubeproxy: a Secret the sandbox cannot redact")

// unredactable is the message of the 502 that answers in place of a response
// the rewriter cannot read.
const unredactable = "kubetail: the cluster answered a Secret in a form the sandbox cannot redact"

// rewriteSecrets makes p ask for plain JSON and redact the answer. A watch's
// status line goes at once; any other waits for the first redacted piece, so a
// failure before it answers 502.
func rewriteSecrets(p *httputil.ReverseProxy, watch bool) {
	rewrite := p.Rewrite
	p.Rewrite = func(pr *httputil.ProxyRequest) {
		rewrite(pr)
		askForJSON(pr.Out.Header)
	}
	p.ModifyResponse = func(resp *http.Response) error { return redactResponse(resp, watch) }
}

// isWatch is whether a request on p with query q is a watch, as the API server
// reads one.
func isWatch(p apiPath, q url.Values) bool {
	var watch bool
	v := q["watch"]
	_ = runtime.Convert_Slice_string_To_bool(&v, &watch, nil)
	return p.watch || watch
}

// redactResponse replaces resp's body with its redacted stream. It answers
// errUnredactable for a body it cannot read, or one that fails before its
// first redacted piece.
func redactResponse(resp *http.Response, watch bool) error {
	if !isJSON(resp.Header.Get("Content-Type")) || resp.Header.Get("Content-Encoding") != "" {
		return fmt.Errorf("%w: a %q answer", errUnredactable, resp.Header.Get("Content-Type"))
	}
	pr, pw := io.Pipe()
	b := &redactedBody{pw: pw}
	var ready chan error
	if !watch {
		ready = make(chan error, 1)
		b.ready = ready
	}
	go b.rewrite(resp.Body)
	if ready != nil {
		if err := <-ready; err != nil {
			return fmt.Errorf("%w: %w", errUnredactable, err)
		}
	}
	resp.Body = pr
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

// redactedBody is the body a response on secrets is rewritten into. What is
// written is held until a redacted piece is whole, then goes down the pipe in
// one write. The first piece also signals ready, since the pipe has no reader
// before ModifyResponse returns.
type redactedBody struct {
	pw   *io.PipeWriter
	held bytes.Buffer
	// ready is sent nil once the first piece is whole, or the error that came
	// before it. It is nil once sent, and for a watch, whose status line goes
	// at once.
	ready chan<- error
}

func (b *redactedBody) write(p []byte) { b.held.Write(p) }

// piece sends what is held down the pipe, a redacted piece whole, and answers
// the pipe's refusal: the client is gone.
func (b *redactedBody) piece() error {
	if b.ready != nil {
		b.ready <- nil
		b.ready = nil
	}
	if b.held.Len() == 0 {
		return nil
	}
	_, err := b.pw.Write(b.held.Bytes())
	b.held.Reset()
	return err
}

// rewrite reads upstream a value at a time, redacts each and writes it, and
// cuts the body at the first failure, so nothing it could not read passes.
func (b *redactedBody) rewrite(upstream io.ReadCloser) {
	defer upstream.Close()
	dec := json.NewDecoder(upstream)
	dec.UseNumber()
	err := b.values(dec)
	if err == nil {
		err = b.piece()
	}
	if err != nil && b.ready != nil {
		b.ready <- err
	}
	_ = b.pw.CloseWithError(err)
}

// values rewrites each top-level value, a newline after each, as a watch
// stream frames them.
func (b *redactedBody) values(dec *json.Decoder) error {
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := b.value(dec, tok); err != nil {
			return err
		}
		b.write([]byte("\n"))
		if err := b.piece(); err != nil {
			return err
		}
	}
}

// value rewrites the top-level value tok opens.
func (b *redactedBody) value(dec *json.Decoder, tok json.Token) error {
	switch tok {
	case json.Delim('{'):
		return b.object(dec)
	case json.Delim('['):
		return b.elements(dec)
	}
	b.write(marshal(tok))
	return nil
}

// collections are the keys whose arrays stream an element at a time: a list's
// items and a table's rows. Any other array is held, so a Secret's data that
// is an array reaches the walk and fails it.
var collections = map[string]bool{"items": true, "rows": true}

// object rewrites an object whose { has been read. A collection's array
// streams its elements, each redacted alone. Every other key is held, and the held keys are redacted together, as
// one map, and written when the object ends, so a key is redacted the same
// wherever it came in the stream.
func (b *redactedBody) object(dec *json.Decoder) error {
	held := map[string]any{}
	streamed := false
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if tok != json.Delim('[') || !collections[key.(string)] {
			if held[key.(string)], err = valueFrom(dec, tok); err != nil {
				return err
			}
			continue
		}
		open := ","
		if !streamed {
			open = "{"
		}
		b.write([]byte(open + string(marshal(key)) + ":"))
		if err := b.elements(dec); err != nil {
			return err
		}
		streamed = true
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if err := redact(held); err != nil {
		return err
	}
	rest := marshal(held)
	switch {
	case !streamed:
	case len(held) == 0:
		rest = []byte("}")
	default:
		rest = append([]byte(","), rest[1:]...)
	}
	b.write(rest)
	return nil
}

// elements rewrites an array whose [ has been read, an element at a time.
func (b *redactedBody) elements(dec *json.Decoder) error {
	b.write([]byte("["))
	for i := 0; dec.More(); i++ {
		var e any
		if err := dec.Decode(&e); err != nil {
			return err
		}
		if err := redact(e); err != nil {
			return err
		}
		if i > 0 {
			b.write([]byte(","))
		}
		b.write(marshal(e))
		if err := b.piece(); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	b.write([]byte("]"))
	return nil
}

// valueFrom reads the value tok opens off dec, as dec.Decode would have.
func valueFrom(dec *json.Decoder, tok json.Token) (any, error) {
	switch tok {
	case json.Delim('{'):
		m := map[string]any{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			t, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if m[key.(string)], err = valueFrom(dec, t); err != nil {
				return nil, err
			}
		}
		_, err := dec.Token()
		return m, err
	case json.Delim('['):
		a := []any{}
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := valueFrom(dec, t)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err := dec.Token()
		return a, err
	}
	return tok, nil
}

// askForJSON rewrites a request on secrets so the answer is one the rewriter
// reads: plain JSON. Accept keeps only its application/json types, since a
// wildcard would leave the form to the server, and Accept-Encoding goes, so
// the transport asks for gzip itself and hands back plain bytes.
func askForJSON(h http.Header) {
	var kept []string
	for _, t := range acceptedTypes(h) {
		if isJSON(t) {
			kept = append(kept, t)
		}
	}
	accept := strings.Join(kept, ",")
	if accept == "" {
		accept = "application/json"
	}
	h.Set("Accept", accept)
	h.Del("Accept-Encoding")
}

// acceptedTypes is every media type h's Accept names, in order.
func acceptedTypes(h http.Header) []string {
	var types []string
	for _, v := range h.Values("Accept") {
		for t := range strings.SplitSeq(v, ",") {
			types = append(types, strings.TrimSpace(t))
		}
	}
	return types
}

// isJSON is whether a media type, its parameters aside, is application/json.
func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

// walk calls fn on every map in v, at any depth, v's own first.
func walk(v any, fn func(map[string]any) error) error {
	var children []any
	switch v := v.(type) {
	case map[string]any:
		if err := fn(v); err != nil {
			return err
		}
		for _, e := range v {
			children = append(children, e)
		}
	case []any:
		children = v
	}
	for _, e := range children {
		if err := walk(e, fn); err != nil {
			return err
		}
	}
	return nil
}

// redactLastApplied blanks every last-applied-configuration in v, which holds
// a whole manifest whatever the kind, and leaves the rest. It never touches
// data, so a ConfigMap's change still shows.
func redactLastApplied(v any) {
	_ = walk(v, func(m map[string]any) error {
		if _, ok := m[lastApplied]; ok {
			m[lastApplied] = safe.Redacted
		}
		return nil
	})
}

// redact redacts v in place, a value decoded with UseNumber: every map holding
// metadata is a Secret's shape, and every last-applied-configuration goes,
// whatever the kind, since a table row's metadata carries a Secret's whole.
func redact(v any) error {
	return walk(v, func(m map[string]any) error {
		if _, ok := m[lastApplied]; ok {
			m[lastApplied] = safe.Redacted
		}
		if _, ok := m["metadata"]; !ok {
			return nil
		}
		return redactSecret(m)
	})
}

// redactSecret redacts obj's data and stringData. A helm release's release is
// redacted inside, since helm reads it to list and show.
func redactSecret(obj map[string]any) error {
	data, _ := obj["data"].(map[string]any)
	release, isRelease := data["release"]
	isRelease = isRelease && release != nil && obj["type"] == helmReleaseType
	if isRelease {
		value, ok := release.(string)
		if !ok {
			return fmt.Errorf("%w: a helm release that is not a string", errUnredactable)
		}
		var err error
		if release, err = redactRelease(value); err != nil {
			return err
		}
	}
	if err := redactValues(obj); err != nil {
		return err
	}
	if isRelease {
		data["release"] = release
	}
	return nil
}

// redactValues sets each value of obj's data to redactedData and each of its
// stringData, plain text, to the mark.
func redactValues(obj map[string]any) error {
	if err := replaceValues(obj, "data", redactedData); err != nil {
		return err
	}
	return replaceValues(obj, "stringData", safe.Redacted)
}

// replaceValues sets every value of obj's key, a map or null, to with.
func replaceValues(obj map[string]any, key, with string) error {
	switch m := obj[key].(type) {
	case nil:
	case map[string]any:
		for k := range m {
			m[k] = with
		}
	default:
		return fmt.Errorf("%w: its %s is not a map", errUnredactable, key)
	}
	return nil
}

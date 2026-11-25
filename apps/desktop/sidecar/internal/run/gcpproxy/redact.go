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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// errUnreadable is an answer the proxy will not pass on unread.
var errUnreadable = errors.New("an answer that cannot be checked")

// pieceSize is how much a rewritten answer holds before it sends it on.
const pieceSize = 32 << 10

// rewrites is whether svc's answers pass through the rewriter.
func (svc service) rewrites() bool { return svc.blank != nil }

// blankCompute blanks instance metadata values, which Google says anyone who
// reads the instance reads, and a VPN tunnel's shared secret. The keys stay,
// so a startup script is seen to be set. A string under metadata is a log
// setting's enum, not a value, and stays.
func blankCompute(m map[string]any) {
	for _, key := range []string{"metadata", "commonInstanceMetadata"} {
		if md, ok := m[key].(map[string]any); ok {
			blankMetadata(md)
		}
	}
	partners, _ := m["partnerMetadata"].(map[string]any)
	for namespace, v := range partners {
		ns, _ := v.(map[string]any)
		if entries, ok := ns["entries"].(map[string]any); ok {
			blankValues(entries)
		} else {
			partners[namespace] = safe.Redacted
		}
	}
	blankField(m, "sharedSecret")
}

// blankMetadata blanks a metadata map's values, in each shape Compute
// answers: a Metadata, whose items list keys and values; instance settings',
// whose items map keys to values; and a plain map of keys to values, as a
// preserved state or an instance group's config holds. Only an items
// collection marks the first two, since a plain map's values are strings; a
// map without one is blanked whole, an empty Metadata's kind and fingerprint
// with it, since in a plain map those are the user's keys.
func blankMetadata(md map[string]any) {
	switch items := md["items"].(type) {
	case []any:
		for _, item := range items {
			if item, ok := item.(map[string]any); ok {
				blankField(item, "value")
			}
		}
	case map[string]any:
		blankValues(items)
	default:
		blankValues(md)
	}
}

// blankContainer blanks a cluster's client key and password, which it
// answers on every read, and each node metadata value, which holds what an
// instance's does. A metadata map under any config is blanked, which hides
// more and never less.
func blankContainer(m map[string]any) {
	if auth, ok := m["masterAuth"].(map[string]any); ok {
		blankField(auth, "clientKey")
		blankField(auth, "password")
	}
	for _, key := range []string{"nodeConfig", "config"} {
		config, _ := m[key].(map[string]any)
		md, _ := config["metadata"].(map[string]any)
		blankValues(md)
	}
}

// blankValues blanks every value of m.
func blankValues(m map[string]any) {
	for k := range m {
		m[k] = safe.Redacted
	}
}

// blankField blanks m's key, when m has it.
func blankField(m map[string]any, key string) {
	if _, ok := m[key]; ok {
		m[key] = safe.Redacted
	}
}

// heldKeys are the keys the rules read under, whose value is read whole,
// so a rule sees it under its key.
var heldKeys = map[string]bool{
	"metadata": true, "commonInstanceMetadata": true, "partnerMetadata": true,
	"masterAuth": true, "nodeConfig": true, "config": true,
}

// rewriteAnswer replaces a 2xx answer's body with its blanked stream. An
// error answer, and one with no body, pass as they are: neither holds a
// resource. It answers errUnreadable for a body that is not plain JSON, or
// that fails before its first piece.
func rewriteAnswer(svc service, resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode > 299 || resp.StatusCode == http.StatusNoContent ||
		resp.Request.Method == http.MethodHead || resp.ContentLength == 0 {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || resp.Header.Get("Content-Encoding") != "" {
		return errUnreadable
	}
	pr, pw := io.Pipe()
	ready := make(chan error, 1)
	rw := &rewriter{blank: svc.blank, pw: pw, ready: ready}
	rw.enc = json.NewEncoder(&rw.held)
	rw.enc.SetEscapeHTML(false)
	go rw.run(resp.Body)
	if err := <-ready; err != nil {
		return fmt.Errorf("%w: %w", errUnreadable, err)
	}
	resp.Body = pr
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return nil
}

// rewriter streams one answer, an object whose objects and arrays go a
// member and an element at a time, each blanked before it is written, but
// under a held key, read whole. What is written is held until pieceSize, then
// goes down the pipe in one write; the first piece also signals ready, since
// the pipe has no reader before ModifyResponse returns. It cuts the body at
// the first failure, so nothing it could not read passes.
type rewriter struct {
	blank func(map[string]any)
	pw    *io.PipeWriter
	held  bytes.Buffer
	enc   *json.Encoder // writes to held, with no HTML escaping
	ready chan<- error
}

func (rw *rewriter) run(body io.ReadCloser) {
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.UseNumber()
	err := rw.object(dec)
	if err == nil {
		if _, end := dec.Token(); end != io.EOF {
			err = errors.New("more than one value")
		}
	}
	if err == nil {
		err = rw.piece()
	}
	// The decoder says io.EOF for a body that ends inside the object, which
	// the pipe would pass on as a whole one.
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && rw.ready != nil {
		rw.ready <- err
	}
	_ = rw.pw.CloseWithError(err)
}

// piece sends what is held down the pipe, and answers the pipe's refusal:
// the client is gone.
func (rw *rewriter) piece() error {
	if rw.ready != nil {
		rw.ready <- nil
		rw.ready = nil
	}
	_, err := rw.pw.Write(rw.held.Bytes())
	rw.held.Reset()
	return err
}

// object rewrites the answer's one object.
func (rw *rewriter) object(dec *json.Decoder) error {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.Join(errors.New("not an object"), err)
	}
	return rw.members(dec)
}

// members rewrites an object whose { has been read, a member at a time.
func (rw *rewriter) members(dec *json.Decoder) error {
	rw.held.WriteString("{")
	for i := 0; dec.More(); i++ {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key := tok.(string)
		if i > 0 {
			rw.held.WriteString(",")
		}
		rw.write(key)
		rw.held.WriteString(":")
		if tok, err = dec.Token(); err != nil {
			return err
		}
		switch {
		case heldKeys[key]:
		case tok == json.Delim('['):
			if err := rw.elements(dec); err != nil {
				return err
			}
			continue
		case tok == json.Delim('{'):
			if err := rw.members(dec); err != nil {
				return err
			}
			continue
		}
		v, err := valueFrom(dec, tok)
		if err != nil {
			return err
		}
		wrapped := map[string]any{key: v}
		blankAll(wrapped, rw.blank)
		rw.write(wrapped[key])
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	rw.held.WriteString("}")
	return nil
}

// elements rewrites an array whose [ has been read, an element at a time.
func (rw *rewriter) elements(dec *json.Decoder) error {
	rw.held.WriteString("[")
	for i := 0; dec.More(); i++ {
		var e any
		if err := dec.Decode(&e); err != nil {
			return err
		}
		blankAll(e, rw.blank)
		if i > 0 {
			rw.held.WriteString(",")
		}
		rw.write(e)
		if rw.held.Len() < pieceSize {
			continue
		}
		if err := rw.piece(); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	rw.held.WriteString("]")
	return nil
}

// write holds v as JSON, its text as Google wrote it.
func (rw *rewriter) write(v any) {
	_ = rw.enc.Encode(v)                // a decoded value always encodes
	rw.held.Truncate(rw.held.Len() - 1) // Encode's newline
}

// blankAll calls blank on every object in v, at any depth.
func blankAll(v any, blank func(map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		blank(v)
		for _, e := range v {
			blankAll(e, blank)
		}
	case []any:
		for _, e := range v {
			blankAll(e, blank)
		}
	}
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

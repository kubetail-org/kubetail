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

package safe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// RedactJSON redacts s by its structure, for JSON that is one line with its newlines
// escaped: Redact over that text would take the rest of the line after a match, which is
// the rest of the document, and miss a line inside an escaped string. Every string, key
// or value, goes through Redact decoded, so it reads as its own lines. The result is s
// compact, in its own order, with numbers as written. ok is false when s is not one JSON
// object or array.
func RedactJSON(s string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	root, err := parseJSON(dec)
	if err != nil || (root.kind != '{' && root.kind != '[') {
		return "", false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", false
	}
	var b bytes.Buffer
	redactNode(root, false)
	writeJSON(&b, root)
	return b.String(), true
}

// jsonNode is one JSON value, keeping an object's members in the order they came.
type jsonNode struct {
	// kind is '{', '[', '"' for a string, '0' for a number, or 'l' for a literal.
	kind byte
	// text is a string's value, a number as written, or true, false or null.
	text string
	keys []string
	kids []*jsonNode
}

func parseJSON(dec *json.Decoder) (*jsonNode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		n := &jsonNode{kind: byte(t)}
		for dec.More() {
			if n.kind == '{' {
				key, err := dec.Token()
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, key.(string))
			}
			kid, err := parseJSON(dec)
			if err != nil {
				return nil, err
			}
			n.kids = append(n.kids, kid)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return n, nil
	case string:
		return &jsonNode{kind: '"', text: t}, nil
	case json.Number:
		return &jsonNode{kind: '0', text: string(t)}, nil
	case bool:
		if t {
			return &jsonNode{kind: 'l', text: "true"}, nil
		}
		return &jsonNode{kind: 'l', text: "false"}, nil
	default:
		return &jsonNode{kind: 'l', text: "null"}, nil
	}
}

// redactNode runs Redact over every string under n, keys included. Under a credential
// key, secret, every string and number is Redacted instead; a key, a boolean and a null
// carry no credential and stay.
func redactNode(n *jsonNode, secret bool) {
	switch n.kind {
	case '"', '0':
		if secret {
			n.kind, n.text = '"', Redacted
		} else if n.kind == '"' {
			n.text = Redact(n.text)
		}
	case '{', '[':
		for i := range n.keys {
			n.keys[i] = Redact(n.keys[i])
		}
		named := namesACredential(n)
		for i, kid := range n.kids {
			under := n.kind == '{' && (isCredentialKey(n.keys[i]) || named && n.keys[i] == "value")
			redactNode(kid, secret || under)
		}
	}
}

// namesACredential reports an object whose name member is a string that is a credential
// key, so its value member holds the credential: a Pod's env var
// ({"name":"DB_PASSWORD","value":…}), a Helm value, an Argo parameter. Its valueFrom
// names a reference, not a credential, and stays.
func namesACredential(n *jsonNode) bool {
	for i, key := range n.keys {
		if key == "name" && n.kids[i].kind == '"' && isCredentialKey(n.kids[i].text) {
			return true
		}
	}
	return false
}

// isCredentialKey reports a key that, lower-cased with - and _ removed, equals or ends
// with one of credentialNames spelled the same way: DB_PASSWORD, clientSecret,
// serviceAccountToken.
func isCredentialKey(key string) bool {
	k := squash(key)
	for _, name := range credentialNames {
		if strings.HasSuffix(k, squash(name)) {
			return true
		}
	}
	return false
}

// squash is s lower-cased with - and _ removed.
func squash(s string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(s))
}

func writeJSON(b *bytes.Buffer, n *jsonNode) {
	switch n.kind {
	case '{', '[':
		b.WriteByte(n.kind)
		for i, kid := range n.kids {
			if i > 0 {
				b.WriteByte(',')
			}
			if n.kind == '{' {
				writeString(b, n.keys[i])
				b.WriteByte(':')
			}
			writeJSON(b, kid)
		}
		if n.kind == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	case '"':
		writeString(b, n.text)
	default:
		b.WriteString(n.text)
	}
}

// writeString writes s as a JSON string, leaving <, > and & as they are.
func writeString(b *bytes.Buffer, s string) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)       // a string always encodes
	b.Truncate(b.Len() - 1) // Encode ends with a newline
}

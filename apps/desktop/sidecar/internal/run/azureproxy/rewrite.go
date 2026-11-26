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

package azureproxy

import (
	"bufio"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// linkHeaders are the headers whose link az follows.
var linkHeaders = []string{"Location", "Azure-AsyncOperation", "Operation-Location"}

// linkRewriter is what makes a link to host name the door on port instead:
// az follows the links an answer gives it.
func linkRewriter(host string, port int) func(string) string {
	from := "https://" + host
	to := "https://127.0.0.1:" + strconv.Itoa(port)
	return func(link string) string {
		if len(link) < len(from) || !strings.EqualFold(link[:len(from)], from) {
			return link
		}
		rest := strings.TrimPrefix(link[len(from):], ":443")
		if rest != "" && rest[0] != '/' && rest[0] != '?' {
			return link
		}
		return to + rest
	}
}

// relay writes resp to w with its links naming the door, and with no
// WWW-Authenticate, which would send az to sign in. A JSON body is rewritten
// as it streams; one that fails partway is cut, never ended as if whole.
func relay(w http.ResponseWriter, resp *http.Response, link func(string) string) {
	rewrite := isJSON(resp.Header.Get("Content-Type"))
	for k, vs := range resp.Header {
		if k == "Www-Authenticate" || rewrite && k == "Content-Length" {
			continue
		}
		w.Header()[k] = vs
	}
	for _, k := range linkHeaders {
		if v := w.Header().Get(k); v != "" {
			w.Header().Set(k, link(v))
		}
	}
	w.WriteHeader(resp.StatusCode)
	var err error
	if rewrite {
		err = rewriteBody(w, resp.Body, link)
	} else {
		_, err = io.Copy(w, resp.Body)
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
}

func isJSON(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// rewriteBody copies a JSON body from src to dst a token at a time,
// rewriting the top-level object's nextLink or @odata.nextLink. One inside
// an item is left alone; so is a body that is not an object.
func rewriteBody(dst io.Writer, src io.Reader, link func(string) string) error {
	in := bufio.NewReader(src)
	first, err := peekNonSpace(in)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	if first != '{' {
		_, err := io.Copy(dst, in)
		return err
	}
	out := bufio.NewWriter(dst)
	dec := json.NewDecoder(in)
	dec.UseNumber()
	var e emitter
	for {
		tok, err := dec.Token()
		if err == io.EOF && len(e.stack) == 0 {
			return out.Flush()
		}
		if err != nil {
			return err
		}
		if s, ok := tok.(string); ok && e.topLevelValue() && (e.key == "nextLink" || e.key == "@odata.nextLink") {
			tok = link(s)
		}
		if err := e.emit(out, tok); err != nil {
			return err
		}
	}
}

// peekNonSpace is the first byte of in past any white space, left unread.
func peekNonSpace(in *bufio.Reader) (byte, error) {
	for {
		b, err := in.Peek(1)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\n', '\r':
			_, _ = in.ReadByte()
		default:
			return b[0], nil
		}
	}
}

// emitter writes json.Decoder's tokens back as JSON, putting back the commas
// and colons the tokens leave out.
type emitter struct {
	stack []frame
	key   string // the last key of the top-level object
}

// frame is one open object or array, and how many keys and values it holds.
type frame struct {
	object bool
	n      int
}

// topLevelValue is whether the next token is a value of the top-level
// object.
func (e *emitter) topLevelValue() bool {
	return len(e.stack) == 1 && e.stack[0].n%2 == 1
}

func (e *emitter) emit(out *bufio.Writer, tok json.Token) error {
	if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
		e.stack = e.stack[:len(e.stack)-1]
		return out.WriteByte(byte(d))
	}
	if len(e.stack) > 0 {
		top := &e.stack[len(e.stack)-1]
		switch {
		case top.object && top.n%2 == 1:
			_ = out.WriteByte(':')
		case top.n > 0:
			_ = out.WriteByte(',')
		}
		if top.object && top.n%2 == 0 && len(e.stack) == 1 {
			e.key, _ = tok.(string)
		}
		top.n++
	}
	if d, ok := tok.(json.Delim); ok {
		e.stack = append(e.stack, frame{object: d == '{'})
		return out.WriteByte(byte(d))
	}
	b, err := json.Marshal(tok)
	if err != nil { // coverage:ignore a decoded token always marshals
		return err
	}
	_, err = out.Write(b)
	return err
}

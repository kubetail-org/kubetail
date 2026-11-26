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

package webfetch

import (
	"bytes"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

const (
	// maxMediaType is the most of a media type a result carries: the token can
	// be as long as the response headers.
	maxMediaType = 100
	// maxTitle is the most of a title a result carries, in characters.
	maxTitle = 200
)

// page is a fetched body as the model reads it.
type page struct {
	mediaType string
	title     string
	text      string
}

// readPage is body as text, by its media type: HTML converted to markdown, the
// other text types as they came, anything else refused. A Content-Type that is
// missing or does not parse is sniffed. base resolves the page's relative
// links.
func readPage(contentType string, body []byte, base *url.URL) (page, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if contentType == "" || err != nil {
		mediaType, params, _ = mime.ParseMediaType(http.DetectContentType(body))
		// The sniffer names UTF-8 for any HTML, and a charset in the type
		// overrides the page's own <meta charset> and BOM.
		contentType = mediaType
	}
	// The type goes into the result's header, which Fit never cuts.
	shown := capped(mediaType, maxMediaType)
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		p, err := readHTML(contentType, body, base)
		p.mediaType = shown
		return p, err
	case isText(mediaType):
		text, err := decodeText(params["charset"], body)
		return page{mediaType: shown, text: text}, err
	}
	return page{}, refusal("WebFetch reads text pages only; this one is " + shown + ".")
}

func isText(mediaType string) bool {
	switch mediaType {
	case "text/markdown", "text/plain", "application/json", "application/yaml", "text/yaml",
		"text/xml", "application/xml":
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

// decodeText is a text body other than HTML in UTF-8, from the charset its
// header names; with none, or one it cannot name, the body is UTF-8 as sent.
// The charset package guesses as HTML does, and would read a body whose first
// kilobyte is ASCII as Windows-1252.
func decodeText(label string, body []byte) (string, error) {
	enc, _ := charset.Lookup(label)
	if enc == nil {
		return string(body), nil
	}
	b, err := enc.NewDecoder().Bytes(body)
	return string(b), err
}

// dropped are the elements that are never the page's text.
var dropped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Iframe: true, atom.Svg: true, atom.Nav: true, atom.Aside: true,
	atom.Input: true, atom.Button: true, atom.Select: true, atom.Textarea: true,
}

// readHTML is an HTML page as markdown, with its title. A form is kept, since
// some sites wrap the whole page in one; a header or footer goes only as the
// body's own child, so an article's header, with its title, stays.
func readHTML(contentType string, body []byte, base *url.URL) (page, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return page{}, err
	}
	doc, err := html.Parse(r)
	if err != nil { // coverage:ignore html.Parse fails only on a read error, and the body is in memory
		return page{}, err
	}
	title := titleOf(doc)
	var drop []*html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		pageChrome := (n.DataAtom == atom.Header || n.DataAtom == atom.Footer) && n.Parent.DataAtom == atom.Body
		if dropped[n.DataAtom] || pageChrome {
			drop = append(drop, n)
		}
	}
	for _, n := range drop {
		n.Parent.RemoveChild(n)
	}
	md, err := htmltomarkdown.ConvertNode(doc, converter.WithDomain(base.String()))
	if err != nil { // coverage:ignore ConvertNode fails only on a plugin's setup, and its default plugins set up cleanly
		return page{}, err
	}
	return page{title: title, text: string(md)}, nil
}

// titleOf is the page's first <title>, on one line of at most maxTitle
// characters, so it cannot write header lines of its own.
func titleOf(doc *html.Node) string {
	for n := range doc.Descendants() {
		if n.DataAtom != atom.Title {
			continue
		}
		var b strings.Builder
		for c := range n.Descendants() {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
		}
		title := strings.Join(strings.Fields(b.String()), " ")
		if utf8.RuneCountInString(title) > maxTitle {
			title = string([]rune(title)[:maxTitle-1]) + "…"
		}
		return title
	}
	return ""
}

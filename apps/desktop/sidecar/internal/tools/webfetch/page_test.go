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
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

var base, _ = url.Parse("https://a.test/docs/page")

func htmlPage(t *testing.T, doc string) page {
	t.Helper()
	p, err := readPage("text/html; charset=utf-8", []byte(doc), base)
	require.NoError(t, err)
	return p
}

func TestWebFetchConvertsHTML(t *testing.T) {
	p := htmlPage(t, `<html><head><title>Release
		notes</title><style>body{}</style><script>alert(1)</script></head>
		<body>
		<header>Site menu</header>
		<nav>Home | Docs</nav>
		<article><header><h1>v1.2.3</h1></header><p>Fixes <a href="/cve">a CVE</a>.</p></article>
		<aside>Ads</aside>
		<form><p>Inside a form</p><input value="typed"><button>Go</button><select><option>opt</option></select><textarea>draft</textarea></form>
		<noscript>enable js</noscript><template>tmpl</template><iframe src="x"></iframe><svg><text>svg</text></svg>
		<footer>Copyright</footer>
		</body></html>`)

	assert.Equal(t, "text/html", p.mediaType)
	assert.Equal(t, "Release notes", p.title)
	assert.Contains(t, p.text, "# v1.2.3")
	assert.Contains(t, p.text, "Fixes [a CVE](https://a.test/cve).")
	assert.Contains(t, p.text, "Inside a form")
	for _, gone := range []string{"alert", "body{}", "Site menu", "Home | Docs", "Ads", "typed", "Go", "opt", "draft",
		"enable js", "tmpl", "svg", "Copyright"} {
		assert.NotContains(t, p.text, gone)
	}
}

// A title is one line of at most 200 characters, so it cannot write header
// lines of its own.
func TestATitleIsOneShortLine(t *testing.T) {
	p := htmlPage(t, "<title>"+strings.Repeat("é\n\tx ", 250)+"</title><p>body</p>")
	assert.NotContains(t, p.title, "\n")
	assert.Equal(t, 200, utf8.RuneCountInString(p.title))
	assert.True(t, strings.HasSuffix(p.title, "…"))

	assert.Empty(t, htmlPage(t, "<p>no title</p>").title)
}

func TestTextTypesPassThrough(t *testing.T) {
	for _, ct := range []string{"text/markdown", "text/plain", "application/json", "application/yaml", "text/yaml",
		"application/vnd.api+json", "application/atom+xml", "text/xml", "application/xml"} {
		p, err := readPage(ct, []byte("# <b>as it came</b>"), base)
		require.NoError(t, err, ct)
		assert.Equal(t, "# <b>as it came</b>", p.text, ct)
		assert.Equal(t, ct, p.mediaType)
	}
}

// With no Content-Type, or one that does not parse, the body is sniffed.
func TestAMissingTypeIsSniffed(t *testing.T) {
	for _, ct := range []string{"", "text/html; charset", ";;"} {
		p, err := readPage(ct, []byte("<!DOCTYPE html><title>T</title><p>sniffed</p>"), base)
		require.NoError(t, err, ct)
		assert.Equal(t, "text/html", p.mediaType, ct)
		assert.Equal(t, "sniffed", p.text, ct)
	}
	_, err := readPage("", []byte("%PDF-1.7\n"), base)
	assert.Equal(t, refusal("WebFetch reads text pages only; this one is application/pdf."), err)
}

// A sniffed page is decoded by its own declaration: the sniffer's UTF-8 is a
// default, not the page's word.
func TestASniffedPageKeepsItsCharset(t *testing.T) {
	body := append([]byte(`<!DOCTYPE html><meta charset="windows-1252"><p>`), "caf\xe9"...)
	p, err := readPage("", body, base)
	require.NoError(t, err)
	assert.Equal(t, "café", p.text)
}

// A type WebFetch does not read is refused, its name capped.
func TestOtherTypesAreRefused(t *testing.T) {
	_, err := readPage("application/pdf", []byte("%PDF"), base)
	assert.Equal(t, refusal("WebFetch reads text pages only; this one is application/pdf."), err)

	long := "application/" + strings.Repeat("x", 1000)
	_, err = readPage(long, []byte("x"), base)
	assert.Equal(t, refusal("WebFetch reads text pages only; this one is "+long[:100]+"…."), err)
}

// Text is decoded to UTF-8 from the header or the page's own declaration.
func TestTextIsDecoded(t *testing.T) {
	latin1 := []byte("caf\xe9")
	p, err := readPage("text/plain; charset=iso-8859-1", latin1, base)
	require.NoError(t, err)
	assert.Equal(t, "café", p.text)

	p, err = readPage("text/html", append([]byte(`<meta charset="iso-8859-1"><p>`), latin1...), base)
	require.NoError(t, err)
	assert.Equal(t, "café", p.text)
}

// A text type other than HTML is UTF-8 unless its header says otherwise:
// HTML's fallback to Windows-1252 would garble a page whose first kilobyte is
// ASCII.
func TestTextKeepsUTF8WithoutACharset(t *testing.T) {
	body := `{"pad":"` + strings.Repeat("x", 2000) + `","name":"café"}`
	for _, ct := range []string{"application/json", "text/markdown", "text/plain", "application/yaml", ""} {
		p, err := readPage(ct, []byte(body), base)
		require.NoError(t, err, ct)
		assert.Equal(t, body, p.text, ct)
	}

	p, err := readPage("application/json; charset=utf-8", []byte(body), base)
	require.NoError(t, err)
	assert.Equal(t, body, p.text)
	p, err = readPage("text/plain; charset=nonsense", []byte("café"), base)
	require.NoError(t, err)
	assert.Equal(t, "café", p.text, "a charset it cannot name leaves the text as sent")
}

// An accepted media type is capped too, so a result's header stays short.
func TestAnAcceptedTypeIsCapped(t *testing.T) {
	long := "application/" + strings.Repeat("x", 1000) + "+json"
	p, err := readPage(long, []byte("{}"), base)
	require.NoError(t, err)
	assert.Equal(t, long[:maxMediaType]+"…", p.mediaType)
}

// Through Run: a page's header names its type, capped, and its title.
func TestTheResultNamesTheTypeAndTitle(t *testing.T) {
	long := "text/plain+" + strings.Repeat("x", 1000)
	s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<title>T</title><p>body</p>")
		case "/long":
			w.Header().Set("Content-Type", long)
			_, _ = io.WriteString(w, "x")
		}
	}))
	tl := s.tool(s.dialing())

	text, isError := run(t, tl, s.url("a.test", "/html"))
	assert.False(t, isError)
	assert.Equal(t, "Fetched "+s.url("a.test", "/html")+" (text/html, 0KB)\nTitle: T\n\nbody", text)

	text, isError = run(t, tl, s.url("a.test", "/long"))
	assert.True(t, isError)
	assert.Equal(t, "WebFetch reads text pages only; this one is "+long[:100]+"….", text)
}

// An empty HTML body is no page: the charset reader finds nothing to read.
func TestAnEmptyHTMLBodyIsNoPage(t *testing.T) {
	_, err := readPage("text/html", nil, base)
	assert.ErrorIs(t, err, io.EOF)
}

// A page that cannot be converted says so, never with the error's text.
func TestAPageThatCannotBeReadSaysSo(t *testing.T) {
	s := newSite(t, plain("hello"))
	tl := s.tool(s.dialing())
	tl.convert = func(string, []byte, *url.URL) (page, error) { return page{}, errors.New("secret detail") }
	text, isError := run(t, tl, s.url("a.test", "/"))
	assert.True(t, isError)
	assert.Equal(t, "WebFetch could not read the page.", text)
}

// The conversion runs where Run can leave it: a cancel answers at once.
func TestAConversionThatOutlastsTheCallIsLeft(t *testing.T) {
	s := newSite(t, plain("hello"))
	tl := s.tool(s.dialing())
	ctx, cancel := context.WithCancel(t.Context())
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	tl.convert = func(string, []byte, *url.URL) (page, error) {
		cancel()
		<-block
		return page{}, nil
	}
	text, isError := tl.Run(ctx, tools.Runtime{}, call(s.url("a.test", "/")))
	assert.True(t, isError)
	assert.Equal(t, `{"error":"cancelled"}`, text)
}

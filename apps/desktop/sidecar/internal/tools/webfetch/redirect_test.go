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
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirects is a site whose paths redirect as each test needs. Location is
// written as given, so a test can hand the server's own port to it.
func redirects(t *testing.T) *site {
	t.Helper()
	var s *site
	s = newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to := func(code int, loc string) {
			if loc != "" {
				w.Header().Set("Location", loc)
			}
			w.WriteHeader(code)
		}
		host := strings.ToLower(r.Host)
		switch p := r.URL.Path; {
		case p == "/page":
			plain("page on "+host)(w, r)
		case p == "/www":
			to(http.StatusMovedPermanently, s.url("www.a.test", "/page"))
		case p == "/bare":
			to(http.StatusFound, s.url("a.test", "/page"))
		case p == "/upper":
			to(http.StatusFound, s.url("A.TEST", "/page"))
		case p == "/other":
			to(http.StatusFound, "//"+net.JoinHostPort("b.test", s.port())+"/page")
		case p == "/port":
			to(http.StatusFound, "https://a.test:1/page")
		case p == "/wwwwww":
			to(http.StatusFound, s.url("www.www.a.test", "/page"))
		case p == "/chain":
			to(http.StatusFound, s.url("www.a.test", "/wwwwww"))
		case p == "/http":
			to(http.StatusFound, "http://"+net.JoinHostPort("a.test", s.port())+"/page")
		case p == "/private":
			to(http.StatusFound, "https://"+net.JoinHostPort("127.0.0.1", s.port())+"/page")
		case strings.HasPrefix(p, "/loop/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(p, "/loop/"))
			to(http.StatusTemporaryRedirect, "/loop/"+strconv.Itoa(n+1))
		case strings.HasPrefix(p, "/count/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(p, "/count/"))
			if n == 5 {
				plain("counted")(w, r)
				return
			}
			to(http.StatusFound, "/count/"+strconv.Itoa(n+1))
		case p == "/nowhere":
			to(http.StatusFound, "")
		case p == "/bad":
			to(http.StatusFound, "%zz://bad")
		case p == "/multiple":
			to(http.StatusMultipleChoices, "/page")
		case p == "/long":
			to(http.StatusFound, "/"+strings.Repeat("x", 20<<10))
		}
	}))
	return s
}

func crossHost(original, redirect, status string) string {
	return "REDIRECT DETECTED: The URL redirects to a different host.\n\nOriginal URL: " + original +
		"\nRedirect URL: " + redirect + "\nStatus: " + status + "\n\nTo read it, call WebFetch again with that URL."
}

func TestWebFetchFollowsWwwAndReturnsOtherRedirects(t *testing.T) {
	s := redirects(t)
	tl := s.tool(s.dialing())
	fetch := func(host, path string) (string, bool) {
		t.Helper()
		return run(t, tl, s.url(host, path))
	}

	// A hop to the approved host or its www. twin is followed, either way round
	// and whatever its case.
	for _, c := range []struct{ host, path, final string }{
		{"a.test", "/www", s.url("www.a.test", "/page")},
		{"www.a.test", "/bare", s.url("a.test", "/page")},
		{"a.test", "/upper", s.url("a.test", "/page")},
	} {
		text, isError := fetch(c.host, c.path)
		require.False(t, isError, text)
		assert.True(t, strings.HasPrefix(text, "Fetched "+c.final+" "), text)
	}

	// Anything else is the model's to ask for again.
	text, isError := fetch("a.test", "/other")
	assert.True(t, isError)
	assert.Equal(t, crossHost(s.url("a.test", "/other"), s.url("b.test", "/page"), "HTTP 302 Found"), text)

	text, _ = fetch("a.test", "/port")
	assert.Equal(t, crossHost(s.url("a.test", "/port"), "https://a.test:1/page", "HTTP 302 Found"), text)

	text, _ = fetch("www.a.test", "/wwwwww")
	assert.Equal(t, crossHost(s.url("www.a.test", "/wwwwww"), s.url("www.www.a.test", "/page"), "HTTP 302 Found"), text)

	// Each hop is compared with the approved host, never the previous hop's.
	text, _ = fetch("a.test", "/chain")
	assert.Equal(t, crossHost(s.url("a.test", "/chain"), s.url("www.www.a.test", "/page"), "HTTP 302 Found"), text)

	text, _ = fetch("a.test", "/http")
	assert.Equal(t, "The page redirects to http://"+net.JoinHostPort("a.test", s.port())+"/page, which is not https. WebFetch does not follow it.", text)

	// target runs on a hop before the host comparison.
	text, _ = fetch("a.test", "/private")
	assert.Equal(t, "The page redirects to https://"+net.JoinHostPort("127.0.0.1", s.port())+"/page. WebFetch does not reach local or private addresses.", text)

	// Five hops are followed; a sixth is not.
	text, isError = fetch("a.test", "/count/0")
	require.False(t, isError, text)
	assert.True(t, strings.HasSuffix(text, "counted"), text)

	text, _ = fetch("a.test", "/loop/0")
	assert.Equal(t, "The page redirected more than 5 times. WebFetch stopped at "+s.url("a.test", "/loop/5")+".", text)

	text, _ = fetch("a.test", "/nowhere")
	assert.Equal(t, "The server answered HTTP 302 Found with nowhere to go.", text)
	text, _ = fetch("a.test", "/bad")
	assert.Equal(t, "The server answered HTTP 302 Found with nowhere to go.", text)

	text, _ = fetch("a.test", "/multiple")
	assert.Equal(t, "The server answered HTTP 300 Multiple Choices. The body was not read.", text)

	text, _ = fetch("a.test", "/long")
	assert.Less(t, len(text), maxURL+200)
	assert.Contains(t, text, "…. WebFetch takes a URL of at most 8,192 bytes.")
}

// The port rule reads a missing port as 443; the test server cannot listen there.
func TestSameSite(t *testing.T) {
	u := func(s string) *url.URL { v, _ := url.Parse(s); return v }
	assert.True(t, sameSite(u("https://a.test/"), u("https://a.test:443/")))
	assert.True(t, sameSite(u("https://www.a.test:443/"), u("https://a.test/")))
	assert.False(t, sameSite(u("https://a.test/"), u("https://a.test:8443/")))
	assert.False(t, sameSite(u("https://a.test/"), u("https://b.test/")))
	assert.False(t, sameSite(u("https://a.test/"), u("https://www.www.a.test/")))
}

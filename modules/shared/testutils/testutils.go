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

package testutils

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

/**
 * WebTestResponse - A container object designed to make it easy to make
 * assertions against an HTTP response from a test server
 */
type WebTestResponse struct {
	StatusCode int
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
}

/**
 * WebTestClient - An HTTP Client designed for making HTTP requests against a
 * test server.
 */
type WebTestClient struct {
	Server     *httptest.Server
	httpclient *http.Client
	baseURL    string
	t          *testing.T
}

// Execute GET request
func (c *WebTestClient) Get(url string) WebTestResponse {
	return c.Do(c.NewRequest("GET", url, nil))
}

// Execute HEAD request
func (c *WebTestClient) Head(url string) WebTestResponse {
	return c.Do(c.NewRequest("HEAD", url, nil))
}

// Execute POST request
func (c *WebTestClient) Post(url, contentType string, body io.Reader) WebTestResponse {
	req := c.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", contentType)
	return c.Do(req)
}

// Execute POST request with form data
func (c *WebTestClient) PostForm(url string, form url.Values) WebTestResponse {
	body := strings.NewReader(form.Encode())
	req := c.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req)
}

// Execute request
func (c *WebTestClient) Do(req *http.Request) WebTestResponse {
	// execute request
	resp, err := c.httpclient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}

	// read body
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}

	// return response
	return WebTestResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Cookies:    resp.Cookies(),
		Body:       respBody,
	}
}

// Close testserver, etc.
func (c *WebTestClient) Teardown() {
	c.Server.Close()
}

// Generate new request object
func (c *WebTestClient) NewRequest(method, target string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, c.baseURL+target, body)
	if err != nil {
		c.t.Fatal(err)
	}
	return req
}

// Create new web test client
func NewWebTestClient(t *testing.T, app http.Handler) *WebTestClient {
	testserver := httptest.NewServer(app)

	// copy test server client
	c := testserver.Client()

	// disable redirect-following
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// add cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Jar = jar

	client := &WebTestClient{
		Server:     testserver,
		httpclient: c,
		baseURL:    testserver.URL,
		t:          t,
	}

	return client
}

// Create new web test client without cookie jar
func NewWebTestClientWithoutCookieJar(t *testing.T, app http.Handler) *WebTestClient {
	c := NewWebTestClient(t, app)
	c.httpclient.Jar = nil
	return c
}

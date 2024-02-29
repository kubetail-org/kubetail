// Copyright 2024 Andres Morey
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

package ginapp

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/suite"
)

type LoggerWriter struct {
	originalWriter io.Writer
}

func (lw LoggerWriter) Write(p []byte) (n int, err error) {
	// Determine the caller of the log function
	_, file, line, ok := runtime.Caller(3) // 3 levels up the stack
	if !ok {
		file = "???"
		line = 0
	}

	// Append the caller info to the log message
	newLogMessage := fmt.Sprintf("%s:%d: %s", file, line, p)

	// Write to the original writer
	return lw.originalWriter.Write([]byte(newLogMessage))
}

func init() {
	gin.SetMode(gin.TestMode)

	// add caller info to logging output
	log.SetOutput(LoggerWriter{originalWriter: os.Stdout})
}

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

/*
 * WebTestSuiteBase - A test suite struct with useful methods for making HTTP
 * requests and testing responses against an instance of the app. This struct
 *  is designed to be emdedded by the test suites.
 */
type WebTestSuiteBase struct {
	suite.Suite
	App           *GinApp
	defaultclient *WebTestClient
}

// Set up entire suite
func (suite *WebTestSuiteBase) SetupSuite() {
	app := NewTestApp(nil)
	suite.App = app
	suite.defaultclient = suite.NewClientWithoutCookieJar()
}

// Tear down entire suite
func (suite *WebTestSuiteBase) TearDownSuite() {
	defer suite.defaultclient.Teardown()
}

// Perform Get() against test server using default client
func (suite *WebTestSuiteBase) Get(url string, fns ...gin.HandlerFunc) WebTestResponse {
	return suite.defaultclient.Get(url, fns...)
}

// Perform Head() against test server using default client
func (suite *WebTestSuiteBase) Head(url string, fns ...gin.HandlerFunc) WebTestResponse {
	return suite.defaultclient.Head(url, fns...)
}

// Perform Post() against test server using default client
func (suite *WebTestSuiteBase) Post(url, contentType string, body io.Reader, fns ...gin.HandlerFunc) WebTestResponse {
	return suite.defaultclient.Post(url, contentType, body, fns...)
}

// Perform PostForm() against test server using default client
func (suite *WebTestSuiteBase) PostForm(url string, form url.Values, fns ...gin.HandlerFunc) WebTestResponse {
	return suite.defaultclient.PostForm(url, form, fns...)
}

// Create new test client
func (suite *WebTestSuiteBase) NewClient() *WebTestClient {
	return NewWebTestClient(suite.T(), suite.App)
}

// Create new test client without cookie jar
func (suite *WebTestSuiteBase) NewClientWithoutCookieJar() *WebTestClient {
	return NewWebTestClientWithoutCookieJar(suite.T(), suite.App)
}

/**
 * WebTestClient - An HTTP Client designed for making HTTP requests against a
 * test server.
 */
type WebTestClient struct {
	testserver *httptest.Server
	httpclient *http.Client
	baseURL    string
	app        *GinApp
	t          *testing.T
}

// Execute GET request
func (c *WebTestClient) Get(url string, fns ...gin.HandlerFunc) WebTestResponse {
	return c.Do(c.NewRequest("GET", url, nil), fns...)
}

// Execute HEAD request
func (c *WebTestClient) Head(url string, fns ...gin.HandlerFunc) WebTestResponse {
	return c.Do(c.NewRequest("HEAD", url, nil), fns...)
}

// Execute POST request
func (c *WebTestClient) Post(url, contentType string, body io.Reader, fns ...gin.HandlerFunc) WebTestResponse {
	req := c.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", contentType)
	return c.Do(req, fns...)
}

// Execute POST request with form data
func (c *WebTestClient) PostForm(url string, form url.Values, fns ...gin.HandlerFunc) WebTestResponse {
	body := strings.NewReader(form.Encode())
	req := c.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req, fns...)
}

// Execute request
func (c *WebTestClient) Do(req *http.Request, fns ...gin.HandlerFunc) WebTestResponse {
	// inject handler into app middleware
	if len(fns) > 0 {
		c.app.wraponce = fns[0]
	}

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
	c.testserver.Close()
}

// Generate new request object
func (c *WebTestClient) NewRequest(method, target string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, c.baseURL+target, body)
	if err != nil {
		c.t.Fatal(err)
	}
	return req
}

// Create new base config for testing
func NewTestConfig() *Config {
	cfg := Config{}
	cfg.AccessLogEnabled = false
	cfg.Session.Secret = "TESTSESSIONSECRET"
	cfg.Session.Cookie.Name = "session"
	cfg.CSRF.Enabled = false
	cfg.CSRF.Secret = "TESTCSRFSECRET"
	return &cfg
}

// Create new app for testing
func NewTestApp(cfg *Config) *GinApp {
	if cfg == nil {
		cfg = NewTestConfig()
	}

	app, err := NewGinApp(*cfg)
	if err != nil {
		panic(err)
	}

	return app
}

// Create new web test client
func NewWebTestClient(t *testing.T, app *GinApp) *WebTestClient {
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

	return &WebTestClient{
		testserver: testserver,
		httpclient: c,
		baseURL:    testserver.URL,
		app:        app,
		t:          t,
	}
}

// Create new web test client without cookie jar
func NewWebTestClientWithoutCookieJar(t *testing.T, app *GinApp) *WebTestClient {
	c := NewWebTestClient(t, app)
	c.httpclient.Jar = nil
	return c
}

// Cookie helper method
func GetCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

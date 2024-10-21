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
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/suite"
)

type WebsiteTestSuite struct {
	WebTestSuiteBase
}

func (suite *WebsiteTestSuite) TestMissing() {
	suite.Run("missing manifest should return 404", func() {
		// empty website directory
		websiteFS := fstest.MapFS{}

		cfg := NewTestConfig()
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusNotFound, w.Code)
		suite.Contains(w.Body.String(), "website not found")
	})
}

func (suite *WebsiteTestSuite) TestTemplate() {
	suite.Run("handles manifest['index.html']['file'] argument", func() {
		websiteFS := suite.createManifest(gin.H{
			"index.html": gin.H{
				"file":    "assets/index-xxx.js",
				"imports": []string{},
				"css":     []string{},
			},
		})

		cfg := NewTestConfig()
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusOK, w.Code)
		suite.Contains(w.Body.String(), "<script type=\"module\" crossorigin src=\"/assets/index-xxx.js\"></script>")
	})

	suite.Run("handles manifest['index.html']['imports'] argument", func() {
		websiteFS := suite.createManifest(gin.H{
			"_vendor-xxx.js": gin.H{
				"file": "assets/vendor-xxx.js",
			},
			"index.html": gin.H{
				"file":    "",
				"imports": []string{"_vendor-xxx.js"},
				"css":     []string{},
			},
		})

		cfg := NewTestConfig()
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusOK, w.Code)
		suite.Contains(w.Body.String(), "<link rel=\"modulepreload\" crossorigin href=\"/assets/vendor-xxx.js\">")
	})

	suite.Run("handles manifest['index.html']['css'] argument", func() {
		websiteFS := suite.createManifest(gin.H{
			"index.html": gin.H{
				"file":    "",
				"imports": []string{},
				"css":     []string{"assets/index-xxx.css"},
			},
		})

		cfg := NewTestConfig()
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusOK, w.Code)
		suite.Contains(w.Body.String(), "<link rel=\"stylesheet\" crossorigin href=\"/assets/index-xxx.css\">")
	})

	suite.Run("prepends asset urls with config.BasePath", func() {
		websiteFS := suite.createManifest(gin.H{
			"index.html": gin.H{
				"file":    "assets/index-xxx.js",
				"imports": []string{},
				"css":     []string{},
			},
		})

		cfg := NewTestConfig()
		cfg.Server.BasePath = "/my-base-path"
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusOK, w.Code)
		suite.Contains(w.Body.String(), "<script type=\"module\" crossorigin src=\"/my-base-path/assets/index-xxx.js\"></script>")
	})

	suite.Run("adds runtimeConfig to html", func() {
		websiteFS := suite.createManifest(gin.H{
			"index.html": gin.H{
				"file":    "",
				"imports": []string{},
				"css":     []string{},
			},
		})

		cfg := NewTestConfig()
		cfg.Server.BasePath = "/my-base-path"
		app := NewTestApp(cfg)

		h := &WebsiteHandlers{app, websiteFS}
		app.GET("/website-test", h.EndpointHandler(cfg))

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/website-test", nil)
		app.ServeHTTP(w, r)
		suite.Equal(http.StatusOK, w.Code)
		suite.Contains(w.Body.String(), "\"basePath\":\"/my-base-path\"")
	})
}

func (suite *WebsiteTestSuite) createManifest(manifest gin.H) fs.FS {
	// encode to json
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}

	return fstest.MapFS{
		".vite/manifest.json": &fstest.MapFile{
			Data: manifestBytes,
		},
	}
}

// test runner
func TestWebsiteHandlers(t *testing.T) {
	suite.Run(t, new(WebsiteTestSuite))
}

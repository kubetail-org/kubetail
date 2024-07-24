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
	"html/template"
	"net/http"
	"os"
	"path"

	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/backend/common/config"
)

type WebsiteHandlers struct {
	*GinApp
	websiteDir string
}

func (app *WebsiteHandlers) InitStaticHandlers(root *gin.RouterGroup) {
	root.StaticFile("/favicon.ico", path.Join(app.websiteDir, "/favicon.ico"))
	root.StaticFile("/graphiql", path.Join(app.websiteDir, "/graphiql.html"))
	root.Static("/assets", path.Join(app.websiteDir, "/assets"))
}

func (app *WebsiteHandlers) EndpointHandler(cfg *config.Config) gin.HandlerFunc {
	// read manifest file
	manifestFile, err := os.Open(path.Join(app.websiteDir, ".vite/manifest.json"))
	if err != nil {
		return func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{
				"status": "website not found",
			})
		}
	}
	defer manifestFile.Close()

	// parse manifest json
	manifest := gin.H{}
	decoder := json.NewDecoder(manifestFile)
	err = decoder.Decode(&manifest)
	if err != nil {
		panic(err)
	}

	// define runtime config for react app
	runtimeConfig := map[string]string{
		"basePath": cfg.Server.BasePath,
	}

	runtimeConfigBytes, err := json.Marshal(runtimeConfig)
	if err != nil {
		panic(err)
	}
	runtimeConfigJS := template.JS(string(runtimeConfigBytes))

	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"config":        cfg,
			"manifest":      manifest,
			"runtimeConfig": template.JS(runtimeConfigJS),
		})
	}
}

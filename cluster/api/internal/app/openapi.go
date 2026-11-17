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

package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// openapiV3IndexHandler advertises the per-group/version OpenAPI v3 specs the
// server publishes. The kube-apiserver fetches this index when aggregating
// OpenAPI v3 across APIServices.
func openapiV3IndexHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"paths": gin.H{
			"apis/api.kubetail.com/v1": gin.H{},
		},
	})
}

// openapiV3GroupVersionHandler returns a minimal OpenAPI v3 document for our
// group/version. We currently advertise no paths; clients tolerate empty
// docs, and aggregation only requires the doc to be valid JSON.
func openapiV3GroupVersionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"openapi": "3.0.0",
		"info": gin.H{
			"title":   "api.kubetail.com",
			"version": "v1",
		},
		"paths":      gin.H{},
		"components": gin.H{},
	})
}

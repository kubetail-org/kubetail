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

package ginhelpers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func IsWebSocketRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}

	upgrade := strings.ToLower(c.GetHeader("Upgrade"))
	if upgrade != "websocket" {
		return false
	}

	connection := strings.ToLower(c.GetHeader("Connection"))
	return strings.Contains(connection, "upgrade")
}

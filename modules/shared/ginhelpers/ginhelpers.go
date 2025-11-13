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

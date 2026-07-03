package api

import (
	"github.com/gin-gonic/gin"
	"toonflow/service"
)

func userMsg(c *gin.Context, err error) string {
	return service.UserMessageWithLogID(err, LogID(c))
}

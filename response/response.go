package response

import (
	"github.com/gin-gonic/gin"
	"github.com/kamu/goapp/errs"
	"github.com/kamu/goapp/reqctx"
)

type Body struct {
	Success   bool   `json:"success"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Success(c *gin.Context, code int, data any) {
	c.JSON(code, Body{Success: true, RequestID: requestID(c), Data: data})
}

func Error(c *gin.Context, code int, err error) {
	// Ensure the error carries a stack (keeps a deeper one if already wrapped at
	// the origin), then attach it to the gin context so the logging middleware
	// can emit a type=error record with stack + error_caller for 5xx.
	err = errs.Wrap(err)
	_ = c.Error(err)
	c.JSON(code, Body{Success: false, RequestID: requestID(c), Error: err.Error()})
}

func requestID(c *gin.Context) string {
	return reqctx.FromContext(c.Request.Context()).RequestID
}

package controller

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

func logError(err interface{}) error {
	e, ok := err.(error)
	if !ok {
		e = fmt.Errorf("%v", err)
	}
	log.Error("%v", e)
	return e
}

func badRequest(field string, err interface{}) error {
	return common.Coded("BAD_REQUEST", logError(err), map[string]interface{}{"field": field})
}

// tooManyRequests is the controller-side counterpart of the router's rate
// limit: it is used when a bounded wait for a shared resource expires.
func tooManyRequests(ctx *gin.Context) {
	ctx.Header("Retry-After", "60")
	ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"code":      common.FAIL,
		"message":   "too many requests; retry shortly",
		"data":      nil,
		"errorCode": "RATE_LIMITED",
	})
}

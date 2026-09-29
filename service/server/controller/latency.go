package controller

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetPingLatency(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()

	var wt []*configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("whiches")), &wt)
	if err != nil {
		common.ResponseError(ctx, badRequest("whiches", "query parameter \"whiches\" must be a JSON array of server items"))
		return
	}
	wt, err = service.Ping(wt, 1*time.Second)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"whiches": wt,
	})
}

func GetHttpLatency(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()

	var wt []*configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("whiches")), &wt)
	if err != nil {
		common.ResponseError(ctx, badRequest("whiches", "query parameter \"whiches\" must be a JSON array of server items"))
		return
	}
	wt, err = service.TestHttpLatency(wt, 8*time.Second, 32, false, ctx.Query("testUrl"))
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"whiches": wt,
	})
}

// GetHttpSpeed is the draft throughput probe: same temporary core as the
// latency test, but it downloads and reports Mbit/s per node without
// persisting anything.
func GetHttpSpeed(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()

	var wt []*configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("whiches")), &wt)
	if err != nil {
		common.ResponseError(ctx, badRequest("whiches", "query parameter \"whiches\" must be a JSON array of server items"))
		return
	}
	maxBytes := int64(service.HttpSpeedMaxBytes)
	if raw := ctx.Query("maxMB"); raw != "" {
		var mb int64
		if _, err := fmt.Sscanf(raw, "%d", &mb); err != nil || mb <= 0 || mb > 100 {
			common.ResponseError(ctx, badRequest("maxMB", "query parameter \"maxMB\" must be 1-100"))
			return
		}
		maxBytes = mb << 20
	}
	wt, err = service.TestHttpSpeed(wt, 30*time.Second, 4, ctx.Query("testUrl"), maxBytes)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"whiches": wt,
	})
}

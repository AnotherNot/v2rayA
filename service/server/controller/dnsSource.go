package controller

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetDnsSource(ctx *gin.Context) {
	src := service.GetDnsSource()
	common.ResponseSuccess(ctx, gin.H{
		"url":     src.URL,
		"version": src.Version,
		"remarks": src.Remarks,
	})
}

func PutDnsSource(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	var data struct {
		URL string `json:"url"`
	}
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, badRequest("url", `request body must be {"url": string}`))
		return
	}
	src, err := service.ImportDnsSource(data.URL)
	if errors.Is(err, service.ErrDnsSourceUnchanged) {
		common.ResponseSuccess(ctx, gin.H{
			"url": src.URL, "version": src.Version, "alreadyUpToDate": true,
		})
		return
	}
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	if err := v2ray.UpdateV2RayConfig(); err != nil {
		common.ResponseError(ctx, logError(fmt.Errorf("rules were imported but the core was not restarted: %w", err)))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"url": src.URL, "version": src.Version, "alreadyUpToDate": false,
	})
}

func DeleteDnsSource(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	if err := service.ClearDnsSource(); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{})
}

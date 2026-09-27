package controller

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetRoutingSource(ctx *gin.Context) {
	src := service.GetRoutingSource()
	resp := gin.H{"url": src.URL, "version": src.Version, "remarks": src.Remarks, "domainStrategy": src.DomainStrategy}
	// The index is fetched on demand so a page can list what a source offers
	// without the stored configuration depending on it being reachable now.
	if src.URL != "" {
		if template, err := service.FetchRoutingTemplate(src.URL); err == nil {
			items := make([]gin.H, 0, len(template.RoutingItems))
			for _, item := range template.RoutingItems {
				items = append(items, gin.H{
					"remarks":        item.Remarks,
					"domainStrategy": item.DomainStrategy,
					"url":            item.URL,
				})
			}
			resp["items"] = items
		}
	}
	common.ResponseSuccess(ctx, resp)
}

func PutRoutingSource(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	var data struct {
		URL     string `json:"url"`
		Remarks string `json:"remarks"`
	}
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, badRequest("url and remarks", `request body must be {"url": string, "remarks": string}`))
		return
	}
	src, err := service.ImportRoutingSource(data.URL, data.Remarks)
	if errors.Is(err, service.ErrRoutingSourceUnchanged) {
		common.ResponseSuccess(ctx, gin.H{
			"url": src.URL, "version": src.Version, "remarks": src.Remarks, "alreadyUpToDate": true,
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
		"url": src.URL, "version": src.Version, "remarks": src.Remarks, "alreadyUpToDate": false,
	})
}

func DeleteRoutingSource(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	if err := service.ClearRoutingSource(); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{})
}

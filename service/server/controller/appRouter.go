package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	v2ray "github.com/v2rayA/v2rayA/kernel/v2ray"
)

// GetAppRouter reports the running per-application engine: whether the mode is
// selected, whether the engine is attached, the plan it attached with and the
// counters it has taken since. The settings page shows it, so a mode that is on
// but routes nothing is not indistinguishable from one that is off.
func GetAppRouter(ctx *gin.Context) {
	common.ResponseSuccess(ctx, v2ray.GetAppRouterStatus())
}

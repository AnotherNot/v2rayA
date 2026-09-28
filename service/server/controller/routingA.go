package controller

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset/dat"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetRoutingA(ctx *gin.Context) {
	common.ResponseSuccess(ctx, gin.H{
		"routingA": configure.GetRoutingA(),
	})
}
func PutRoutingA(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	var data struct {
		RoutingA string `json:"routingA"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, badRequest("routingA", "request body must be {\"routingA\": string}"))
		return
	}
	// remove hardcode replacement and try parsing
	lines := strings.Split(data.RoutingA, "\n")
	hardcodeReplacement := regexp.MustCompile(`\$\$.+?\$\$`)
	for i := range lines {
		hardcodes := hardcodeReplacement.FindAllString(lines[i], -1)
		for _, hardcode := range hardcodes {
			lines[i] = strings.Replace(lines[i], hardcode, "", 1)
		}
	}
	_, err = RoutingA.Parse(strings.Join(lines, "\n"))
	if err != nil {
		common.ResponseError(ctx, logError(fmt.Errorf("invalid RoutingA rules: %w", err)))
		return
	}
	// Name the geo tags the files on this machine do not hold before the
	// core gets its turn: a restart with an unknown tag fails and rolls the
	// setting back, and the core's log line is all the user would get.
	if missing := dat.Missing(dat.RoutingATags(strings.Join(lines, "\n"))); len(missing) > 0 {
		names := make([]string, len(missing))
		for i, r := range missing {
			names[i] = r.String()
		}
		common.ResponseError(ctx, common.Coded("UNKNOWN_GEO_TAGS", logError(fmt.Errorf("unknown geo tags: %v; install the geo files that hold them or fix the rule", strings.Join(names, ", "))), map[string]interface{}{
			"tags": names,
		}))
		return
	}

	// Check for deprecated inbound definitions in RoutingA rules
	hasInboundDef := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "inbound(") || strings.HasPrefix(trimmed, "inbound (") {
			hasInboundDef = true
			break
		}
	}

	err = service.ApplyCoreConfig(func() func() error {
		previous := configure.GetRoutingA()
		return func() error { return configure.SetRoutingA(&previous) }
	}, func() error {
		return configure.SetRoutingA(&data.RoutingA)
	})
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	if hasInboundDef {
		common.ResponseSuccess(ctx, gin.H{
			"warning": "RoutingA 中定义入站(inbound)的功能已弃用，生成的 JSON 配置将不会包含对应的入站端口。请使用自定义入站设置中的 RoutingA 规则功能替代。",
		})
		return
	}
	common.ResponseSuccess(ctx, nil)
}

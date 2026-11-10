package controller

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
	v2ray "github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset/dat"
	"github.com/v2rayA/v2rayA/kernel/v2ray/service"
	"github.com/v2rayA/v2rayA/kernel/v2ray/where"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
	"github.com/v2rayA/v2rayA/pkg/util/privilege"
)

func GetVersion(ctx *gin.Context) {
	lite := 0
	if conf.GetEnvironmentConfig().Lite {
		lite = 1
	}
	// The login page needs to know whether the first account exists. The
	// host's privilege, TUN support, Docker mode and the core probe answer a
	// different question: which exploit applies. They are returned only to a
	// caller that already holds a session.
	payload := gin.H{
		"version":     conf.Version,
		"lite":        lite,
		"hasAccounts": configure.HasAnyAccounts(),
		"variant":     where.V2rayaCore,
		"os":          runtime.GOOS,
	}
	if !requestAuthenticated(ctx) {
		common.ResponseSuccess(ctx, payload)
		return
	}

	isRoot := false
	switch runtime.GOOS {
	case "linux", "darwin":
		isRoot = os.Geteuid() == 0
	case "windows":
		isRoot = privilege.IsRootOrAdmin()
	}

	versionErr := service.CheckCoreVersionMatch()
	// the core's own version, for the dashboard; empty when the binary cannot be asked
	coreVersion := ""
	if _, ver, err := where.GetV2rayServiceVersion(); err == nil {
		coreVersion = ver
	}

	payload["foundNew"] = conf.FoundNew
	payload["remoteVersion"] = conf.RemoteVersion
	payload["serviceValid"] = service.IsV2rayServiceValid()
	payload["v5"] = versionErr == nil
	payload["loadBalanceValid"] = true
	payload["coreVersion"] = coreVersion
	payload["isRoot"] = isRoot
	payload["tunSupported"] = v2ray.TunSupported()
	payload["coreVersionValid"] = versionErr == nil
	payload["coreVersionErr"] = func() string {
		if versionErr != nil {
			return versionErr.Error()
		}
		return ""
	}()
	payload["lastKernelExit"] = configure.GetLastKernelExitStatus()
	payload["docker"] = common.IsDocker()
	common.ResponseSuccess(ctx, payload)
}

// requestAuthenticated reports whether the request carries a live session.
// The version probe does not abort an unauthenticated request: it serves a
// smaller answer instead.
func requestAuthenticated(ctx *gin.Context) bool {
	header := strings.TrimSpace(ctx.GetHeader("Authorization"))
	if header == "" {
		return false
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		header = fields[1]
	}
	return jwt.ValidateToken(header)
}

func GetRemoteGFWListVersion(ctx *gin.Context) {
	g, err := dat.GetRemoteGFWListUpdateTime(&http.Client{Timeout: 10 * time.Second})
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"remoteGFWListVersion":   g.UpdateTime.Local().Format("2006-01-02"),
		"remoteBlocklistVersion": g.UpdateTime.Local().Format("2006-01-02"),
	})
}

// GetParams lists the command-line flags for the documentation page.
func GetParams(ctx *gin.Context) {
	common.ResponseSuccess(ctx, gin.H{"params": conf.Parameters()})
}

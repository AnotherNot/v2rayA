package main

import (
	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common/fileperm"
	_ "github.com/v2rayA/v2rayA/conf/report"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

func main() {
	fileperm.SecureUmask()
	gin.SetMode(gin.ReleaseMode)

	// Try running as a service (implemented on Windows, returns false on other platforms)
	if tryRunAsService() {
		return
	}

	// Normal startup (non-service mode)
	checkEnvironment()
	if err := checkPlatformSpecific(); err != nil {
		log.Fatal("Platform check failed: %v", err)
	}
	initConfigure()
	checkUpdate()
	hello()
	if err := run(); err != nil {
		log.Fatal("main: %v", err)
	}
}

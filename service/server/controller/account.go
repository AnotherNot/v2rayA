package controller

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
	"github.com/v2rayA/v2rayA/server/service"
)

/*检查是否存在账户*/
func GetAccount(ctx *gin.Context) {
	hasAnyAccounts := configure.HasAnyAccounts()
	common.ResponseSuccess(ctx, gin.H{
		"hasAnyAccounts":     hasAnyAccounts,
		"setupTokenRequired": !hasAnyAccounts,
	})
}

var loginSessions = make(chan interface{}, 1)

func PostLogin(ctx *gin.Context) {
	var data struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	// the body is read before the one-at-a-time semaphore, so it is capped
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096)
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		err := badRequest("username and password", "request body must be {\"username\": string, \"password\": string}")
		ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code": common.FAIL, "message": err.Error(), "data": nil, "errorCode": "BAD_REQUEST",
			"params": gin.H{"field": "username and password"},
		})
		return
	}
	loginSessions <- nil
	defer func() {
		time.Sleep(500 * time.Millisecond)
		<-loginSessions
	}()
	if !configure.HasAnyAccounts() {
		common.Response(ctx, common.UNAUTHORIZED, gin.H{
			"first": true,
		})
		return
	}
	jwtToken, refreshToken, err := service.Login(data.Username, data.Password)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"token":        jwtToken,
		"refreshToken": refreshToken,
		"expiresIn":    int(jwt.AccessTTL / time.Second),
	})
}

/*注册*/
var muReg sync.Mutex

func PostAccount(ctx *gin.Context) {
	var data struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		SetupToken string `json:"setupToken"`
	}
	muReg.Lock()
	defer muReg.Unlock()
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, badRequest("username and password", "request body must be {\"username\": string, \"password\": string, \"setupToken\": string}"))
		return
	}
	if ok, err := service.ValidPasswordLength(data.Password); !ok {
		common.ResponseError(ctx, logError(err))
		return
	}
	if configure.HasAnyAccounts() {
		common.ResponseError(ctx, common.Coded("ACCOUNT_EXISTS", logError("an account already exists; sign in instead"), nil))
		return
	}
	// The first account is the administrator. Until it exists, the setup
	// token printed at start proves that the caller can read the service's
	// own console or configuration directory, not merely reach its port.
	if data.SetupToken == "" {
		common.ResponseError(ctx, common.Coded("SETUP_TOKEN_REQUIRED", logError("the first account needs the setup token printed by the service"), nil))
		return
	}
	if !service.VerifySetupToken(data.SetupToken) {
		common.ResponseError(ctx, common.Coded("SETUP_TOKEN_INVALID", logError("the setup token is invalid or has already been used"), nil))
		return
	}
	token, refreshToken, err := service.Register(data.Username, data.Password)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	service.ConsumeSetupToken()
	common.ResponseSuccess(ctx, gin.H{
		"token":        token,
		"refreshToken": refreshToken,
		"expiresIn":    int(jwt.AccessTTL / time.Second),
	})
}

// PostRefresh rotates a refresh token and returns a new short-lived access
// token. The old refresh token stops working immediately.
func PostRefresh(ctx *gin.Context) {
	var data struct {
		RefreshToken string `json:"refreshToken"`
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096)
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.Response(ctx, common.UNAUTHORIZED, common.Coded("SESSION_INVALID", err, nil))
		return
	}
	token, refreshToken, err := service.Refresh(data.RefreshToken)
	if err != nil {
		common.Response(ctx, common.UNAUTHORIZED, common.Coded("SESSION_INVALID", err, nil))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"token":        token,
		"refreshToken": refreshToken,
		"expiresIn":    int(jwt.AccessTTL / time.Second),
	})
}

// PostLogout revokes the current session; the token stops working before
// its short expiry.
func PostLogout(ctx *gin.Context) {
	if id, ok := ctx.Get("SessionID"); ok {
		if sessionID, ok := id.(string); ok {
			_ = service.Logout(sessionID)
		}
	}
	common.ResponseSuccess(ctx, nil)
}

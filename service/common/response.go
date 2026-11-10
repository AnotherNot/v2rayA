package common

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	lru "github.com/hashicorp/golang-lru/v2"
)

type Code string

type Resp struct {
	Status int
	Body   gin.H
}

type codedResponse struct {
	err   error
	coded *CodedError
}

func codedErrorBody(err error, coded *CodedError) gin.H {
	body := gin.H{
		"code":      FAIL,
		"message":   err.Error(),
		"data":      nil,
		"errorCode": coded.Code,
	}
	if coded.Params != nil {
		body["params"] = coded.Params
	}
	return body
}

type cachedResponse struct {
	resp Resp
	at   time.Time
}

const responseCacheTTL = time.Minute

var responseCache, _ = lru.New[string, cachedResponse](64)

const (
	SUCCESS         = "SUCCESS"
	FAIL            = "FAIL"
	UNAUTHORIZED    = "UNAUTHORIZED"
	RequestIdHeader = "X-V2raya-Request-Id"
)

// RequestCacheKey scopes a replay id to the method, URI and authenticated
// user. A request id guessed or reused from another screen must not return
// that screen's cached body.
func RequestCacheKey(ctx *gin.Context) string {
	reqID := ctx.GetHeader(RequestIdHeader)
	if reqID == "" || ctx.Request == nil || ctx.Request.URL == nil {
		return ""
	}
	user := ctx.GetString("Name")
	if user == "" {
		user = ctx.GetString("SessionID")
	}
	return ctx.Request.Method + "\x00" + ctx.Request.URL.RequestURI() + "\x00" + user + "\x00" + reqID
}

// GetCachedResponse returns a response stored under key while it is fresh.
func GetCachedResponse(key string) (Resp, bool) {
	if key == "" {
		return Resp{}, false
	}
	cached, ok := responseCache.Get(key)
	if !ok {
		return Resp{}, false
	}
	if time.Since(cached.at) > responseCacheTTL {
		responseCache.Remove(key)
		return Resp{}, false
	}
	return cached.resp, true
}

// CacheResponse stores a response under key for later replays.
func CacheResponse(key string, resp Resp) {
	if key == "" {
		return
	}
	responseCache.Add(key, cachedResponse{resp: resp, at: time.Now()})
}

// 当code为FAIL时，data为string类型返回给前端的消息
func Response(ctx *gin.Context, code Code, data interface{}) (status int, body gin.H) {
	if key := RequestCacheKey(ctx); key != "" {
		if resp, ok := GetCachedResponse(key); ok {
			ctx.JSON(resp.Status, resp.Body)
			return resp.Status, resp.Body
		}
		defer func() {
			CacheResponse(key, Resp{
				Status: status,
				Body:   body,
			})
		}()
	}
	status = http.StatusOK
	if code == UNAUTHORIZED {
		code = FAIL
		status = http.StatusUnauthorized
	}
	if code == FAIL {
		switch data := data.(type) {
		case string:
			body = gin.H{
				"code":    code,
				"message": data,
				"data":    nil,
			}
		case *CodedError:
			body = codedErrorBody(data, data)
		case codedResponse:
			body = codedErrorBody(data.err, data.coded)
		default:
			body = gin.H{
				"code":    code,
				"message": nil,
				"data":    data,
			}
		}
		ctx.JSON(status, body)
		return status, body
	}
	body = gin.H{
		"code":    code,
		"message": nil,
		"data":    data,
	}
	ctx.JSON(status, body)
	return status, body
}

func ResponseError(ctx *gin.Context, err error) {
	var coded *CodedError
	if errors.As(err, &coded) {
		Response(ctx, FAIL, codedResponse{err: err, coded: coded})
		return
	}
	Response(ctx, FAIL, err.Error())
}
func ResponseSuccess(ctx *gin.Context, data interface{}) {
	Response(ctx, SUCCESS, data)
}

package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
	"github.com/v2rayA/v2rayA/server/service"
)

// wsTokenPrefix marks the access token in the Sec-WebSocket-Protocol request
// header. A browser cannot set Authorization on a WebSocket handshake, and a
// token in the query string is written to every proxy access log on the way;
// the subprotocol header is the place the handshake leaves for it. Both the
// prefix and the JWT's alphabet (base64url and dots) are valid RFC 7230
// tokens, so the header survives proxies unchanged.
const wsTokenPrefix = "v2raya.v1."

// WsMessage upgrades the request to the event socket. The caller supplies the
// browser-origin policy the router already uses for HTTP, so a cross-site
// page cannot open the socket even with a leaked token. The route is not
// behind JWTAuth: that middleware also reads the token from the query string.
func WsMessage(checkOrigin func(*http.Request) bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, subprotocol := wsToken(ctx.Request)
		if token == "" || !jwt.ValidateToken(token) {
			common.Response(ctx, common.UNAUTHORIZED, "unauthorized")
			return
		}
		upgrader := websocket.Upgrader{
			ReadBufferSize:    1024,
			WriteBufferSize:   1024,
			EnableCompression: true,
			CheckOrigin:       checkOrigin,
		}
		// The browser drops the connection unless the server echoes one of
		// the protocols it offered. Only the token-bearing protocol is
		// accepted, so this echoes exactly what carried the credentials.
		if subprotocol != "" {
			upgrader.Subprotocols = []string{subprotocol}
		}
		conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
		if err != nil {
			logError(err)
			return
		}
		h := service.NewMessageHandler(conn)
		go h.Write()
		go h.Read()
	}
}

// wsToken reads the access token from the handshake: the token-bearing
// subprotocol first, then Authorization for clients that can set headers.
// The query string is deliberately not read, so a token placed there cannot
// reach the handler or a request log.
func wsToken(r *http.Request) (token, subprotocol string) {
	for _, offered := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		offered = strings.TrimSpace(offered)
		if strings.HasPrefix(offered, wsTokenPrefix) {
			return strings.TrimPrefix(offered, wsTokenPrefix), offered
		}
	}
	token = strings.TrimSpace(r.Header.Get("Authorization"))
	if len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = token[7:]
	}
	return token, ""
}

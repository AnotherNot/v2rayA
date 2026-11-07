package router

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/conf"
)

// securityConfig holds the browser-facing boundary: which origins may call
// the API, which Host headers may reach it, and the local addresses that are
// accepted as Host without extra configuration.
type securityConfig struct {
	allowedOrigins map[string]struct{}
	allowedHosts   map[string]struct{}
	localHosts     map[string]struct{}
}

func newSecurityConfig(params *conf.Params) securityConfig {
	s := securityConfig{
		allowedOrigins: map[string]struct{}{},
		allowedHosts:   map[string]struct{}{},
		localHosts:     localHosts(params.Address),
	}
	for _, raw := range splitSecurityList(params.CorsOrigins) {
		if origin := normalizeOrigin(raw); origin != "" {
			s.allowedOrigins[origin] = struct{}{}
		}
	}
	for _, raw := range splitSecurityList(params.AllowedHosts) {
		host := hostOnly(raw)
		if host != "" {
			s.allowedHosts[host] = struct{}{}
		}
	}
	return s
}

func splitSecurityList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func hasWildcardOrigin(raw string) bool {
	for _, part := range splitSecurityList(raw) {
		if part == "*" {
			return true
		}
	}
	return false
}

func (s securityConfig) originList() []string {
	out := make([]string, 0, len(s.allowedOrigins))
	for origin := range s.allowedOrigins {
		out = append(out, origin)
	}
	sort.Strings(out)
	return out
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(u.Host)
}

func hostOnly(hostport string) string {
	host := strings.TrimSpace(hostport)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	return host
}

// localHosts lists the Host values a request may carry without an explicit
// --allowed-host: loopback, the configured listening host, and every local
// interface address. A DNS name that rebinds to 127.0.0.1 is not in this
// set, which is what stops DNS rebinding.
func localHosts(address string) map[string]struct{} {
	hosts := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				hosts[ipnet.IP.String()] = struct{}{}
			}
		}
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			if !ip.IsUnspecified() {
				hosts[ip.String()] = struct{}{}
			}
		} else if host = hostOnly(host); host != "" {
			hosts[host] = struct{}{}
		}
	}
	return hosts
}

func (s securityConfig) originAllowed(origin, requestHost string) bool {
	normalized := normalizeOrigin(origin)
	if normalized == "" {
		return false
	}
	if _, ok := s.allowedOrigins[normalized]; ok {
		return true
	}
	// Same host, any scheme: a TLS-terminating reverse proxy is common, and
	// the host is the security boundary here.
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, requestHost)
}

// originAllowedRequest applies the origin policy to a whole request. A
// request without Origin is not a browser cross-origin request (curl, a
// native client) and carries no ambient credentials, so it passes; the
// WebSocket handshake uses this because it cannot set an HTTP header the way
// fetch does.
func (s securityConfig) originAllowedRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return s.originAllowed(origin, r.Host)
}

func (s securityConfig) hostAllowed(hostport string) bool {
	host := hostOnly(hostport)
	if host == "" {
		return false
	}
	// An IP literal cannot be the name a DNS-rebinding attack points at
	// 127.0.0.1. Allowing it covers a container reached through a published
	// port, where the Host is the host's address and not the container's.
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified()
	}
	if _, ok := s.allowedHosts[host]; ok {
		return true
	}
	_, ok := s.localHosts[host]
	return ok
}

// hostGuard rejects requests whose Host is neither local nor explicitly
// allowed. DNS rebinding turns a name the browser trusts into 127.0.0.1;
// the Host header still carries the name, so the request stops here.
func (s securityConfig) hostGuard() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !s.hostAllowed(ctx.Request.Host) {
			abortSecurity(ctx, http.StatusMisdirectedRequest, "MISDIRECTED_REQUEST",
				"this host is not allowed to reach the v2rayA API")
			return
		}
		ctx.Next()
	}
}

// csrfGuard rejects state-changing requests that a browser marks as
// cross-site and that are not from an explicitly allowed origin. Requests
// without Origin (curl, the CLI, a non-browser client) pass: they are not
// subject to the browser's ambient credentials.
func (s securityConfig) csrfGuard() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		switch ctx.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			ctx.Next()
			return
		}
		if origin := ctx.GetHeader("Origin"); origin != "" {
			if !s.originAllowed(origin, ctx.Request.Host) {
				abortSecurity(ctx, http.StatusForbidden, "CROSS_ORIGIN_FORBIDDEN",
					"cross-origin state-changing requests are not allowed")
				return
			}
		} else if strings.EqualFold(ctx.GetHeader("Sec-Fetch-Site"), "cross-site") {
			abortSecurity(ctx, http.StatusForbidden, "CROSS_ORIGIN_FORBIDDEN",
				"cross-site state-changing requests are not allowed")
			return
		}
		ctx.Next()
	}
}

// jsonBodyGuard refuses a request body that claims to be something other
// than JSON. ShouldBindJSON ignores Content-Type, so a "simple" CORS
// request with text/plain would otherwise reach a JSON handler without a
// preflight.
func jsonBodyGuard() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		switch ctx.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			ctx.Next()
			return
		}
		if ctx.Request.ContentLength == 0 {
			ctx.Next()
			return
		}
		mediaType, _, err := mime.ParseMediaType(ctx.GetHeader("Content-Type"))
		if err != nil || !(mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")) {
			abortSecurity(ctx, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE",
				"requests with a body must use Content-Type: application/json")
			return
		}
		ctx.Next()
	}
}

func abortSecurity(ctx *gin.Context, status int, code, message string) {
	ctx.AbortWithStatusJSON(status, gin.H{
		"code":      common.FAIL,
		"message":   message,
		"data":      nil,
		"errorCode": code,
	})
}

// peerCredGuard enforces the unix socket's owner boundary even if a process
// can reach the socket file: SO_PEERCRED reports the connecting UID, and a
// different user is refused. TCP connections carry no peer credentials and
// pass through.
func peerCredGuard() gin.HandlerFunc {
	owner := uint32(os.Geteuid())
	return func(ctx *gin.Context) {
		cred, ok := peerCredFromContext(ctx.Request.Context())
		if !ok {
			ctx.Next()
			return
		}
		if cred.uid != owner {
			abortSecurity(ctx, http.StatusForbidden, "SOCKET_PEER_FORBIDDEN",
				"this unix socket is restricted to the service's own user")
			return
		}
		ctx.Next()
	}
}

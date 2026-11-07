package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxAPIBodyBytes caps every request body the API accepts. The largest
// legitimate payload is a routing or DNS rule document; subscription and
// blocklist data are fetched server-side, not uploaded. 1 MiB leaves ample
// room for a rule set while keeping an unauthenticated handler from buffering
// an arbitrary amount of memory.
const maxAPIBodyBytes int64 = 1 << 20

// limitBody rejects a declared oversized body before the handler reads it
// and caps the rest with http.MaxBytesReader. A chunked body that crosses the
// limit fails while the handler reads it; the reader never buffers more than
// max bytes.
func limitBody(max int64) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.ContentLength > max {
			abortSecurity(ctx, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
				"request body is too large")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, max)
		ctx.Next()
	}
}

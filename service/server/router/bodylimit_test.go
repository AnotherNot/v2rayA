package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLimitBodyRejectsDeclaredOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(limitBody(4))
	r.POST("/", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
	if got := errorCode(t, recorder); got != "PAYLOAD_TOO_LARGE" {
		t.Errorf("errorCode = %q, want PAYLOAD_TOO_LARGE", got)
	}
}

func TestLimitBodyCapsAnUndeclaredBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(limitBody(4))
	r.POST("/", func(ctx *gin.Context) {
		if _, err := io.ReadAll(ctx.Request.Body); err != nil {
			ctx.Status(http.StatusRequestEntityTooLarge)
			return
		}
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	req.ContentLength = -1
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("1234"))
	recorder = httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

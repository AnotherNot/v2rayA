package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
)

// An ext tag names a file inside the asset directory, and the asset lookup
// replaces the link at that path. A rule that points out of the directory must
// be refused before the text is stored.
func TestPutRoutingARejectsAnEscapingGeoFile(t *testing.T) {
	withSetupDatabase(t)
	body := `{"routingA":"domain(ext:\"../../etc/passwd:x\")->proxy"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/routingA", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	PutRoutingA(ctx)

	if code, errorCode := codeOf(t, recorder); code != common.FAIL || errorCode != "INVALID_GEO_FILE" {
		t.Fatalf("code=%v errorCode=%v (%s)", code, errorCode, recorder.Body.String())
	}
	var stored string
	configure.GetRoutingARaw(&stored)
	if stored != "" {
		t.Fatalf("the escaping rule was stored: %q", stored)
	}
}

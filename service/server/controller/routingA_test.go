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

// A stored process(...) rule would take effect the next time the core starts,
// where the generator refuses it — after the text has already been saved. The
// endpoint refuses it before the store, while the user is still looking.
func TestPutRoutingARejectsAProcessCondition(t *testing.T) {
	withSetupDatabase(t)
	body := `{"routingA":"process(firefox) && port(443)->proxy"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/routingA", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	PutRoutingA(ctx)

	if code, _ := codeOf(t, recorder); code != common.FAIL {
		t.Fatalf("code=%v (%s)", code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "process(...)") {
		t.Fatalf("the error does not name the condition: %s", recorder.Body.String())
	}
	var stored string
	configure.GetRoutingARaw(&stored)
	if stored != "" {
		t.Fatalf("the refused rule was stored: %q", stored)
	}
}

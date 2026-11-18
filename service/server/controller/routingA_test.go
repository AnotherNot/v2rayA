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

// process(...) is the app router's condition, so the editor is where it belongs
// and the endpoint stores it; the engine applies it, not the core.
func TestPutRoutingAAcceptsAProcessRule(t *testing.T) {
	withSetupDatabase(t)
	body := `{"routingA":"process(firefox) && port(443)->proxy"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/routingA", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	PutRoutingA(ctx)

	if code, _ := codeOf(t, recorder); code != common.SUCCESS {
		t.Fatalf("code=%v (%s)", code, recorder.Body.String())
	}
	var stored string
	configure.GetRoutingARaw(&stored)
	if !strings.Contains(stored, "process(firefox)") {
		t.Fatalf("the rule was not stored: %q", stored)
	}
}

// A process(...) this build cannot enforce is refused while the user is looking
// at it, not at the next core start.
func TestPutRoutingARejectsABadProcessParameter(t *testing.T) {
	withSetupDatabase(t)
	body := `{"routingA":"process(uid: 1000)->proxy"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/routingA", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	PutRoutingA(ctx)

	if code, _ := codeOf(t, recorder); code != common.FAIL {
		t.Fatalf("code=%v (%s)", code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "name, path and folder") {
		t.Fatalf("the error does not say what a process(...) may name: %s", recorder.Body.String())
	}
	var stored string
	configure.GetRoutingARaw(&stored)
	if stored != "" {
		t.Fatalf("the refused rule was stored: %q", stored)
	}
}

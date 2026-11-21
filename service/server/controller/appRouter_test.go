package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
)

// The settings page asks this before anything is attached: the mode is
// reported as selected and the engine as missing, which is what tells a user
// that pressing Start is still needed.
func TestGetAppRouterReportsTheModeWithoutAnEngine(t *testing.T) {
	resetSetting(t)
	setting := configure.GetSettingNotNil()
	setting.TransparentType = configure.TransparentAppRouter
	if err := configure.SetSetting(setting); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/appRouter", nil)
	GetAppRouter(ctx)

	if code, _ := codeOf(t, recorder); code != common.SUCCESS {
		t.Fatalf("answer: %s", recorder.Body.String())
	}
	var response struct {
		Data struct {
			Mode     bool `json:"mode"`
			Attached bool `json:"attached"`
			Counters any  `json:"counters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.Mode {
		t.Fatal("the selected mode was not reported")
	}
	if response.Data.Attached {
		t.Fatal("an engine was reported without one being attached")
	}
	if response.Data.Counters != nil {
		t.Fatalf("counters without an engine: %v", response.Data.Counters)
	}
}

// Another mode is not this engine's: it reports nothing and the page hides the
// block.
func TestGetAppRouterReportsAnotherModeAsOff(t *testing.T) {
	resetSetting(t)
	setting := configure.GetSettingNotNil()
	setting.TransparentType = configure.TransparentTproxy
	if err := configure.SetSetting(setting); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/appRouter", nil)
	GetAppRouter(ctx)

	var response struct {
		Data struct {
			Mode bool `json:"mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Mode {
		t.Fatal("tproxy was reported as the app router")
	}
}

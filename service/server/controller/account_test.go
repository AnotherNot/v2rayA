package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPostLoginRefusesWhenTheSemaphoreIsBusy(t *testing.T) {
	previousSessions := loginSessions
	previousTimeout := loginWaitTimeout
	loginSessions = make(chan interface{}, 1)
	loginWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		loginSessions = previousSessions
		loginWaitTimeout = previousTimeout
	})
	loginSessions <- nil

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader(`{"username":"someone","password":"something"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostLogin(ctx)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q", got)
	}
	<-loginSessions
}

func TestPostLoginBindsBodyBeforeTakingSemaphore(t *testing.T) {
	previous := loginSessions
	loginSessions = make(chan interface{}, 1)
	t.Cleanup(func() { loginSessions = previous })
	loginSessions <- nil

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.ContentLength = 10
	done := make(chan struct{})
	go func() {
		PostLogin(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		<-loginSessions
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("invalid body waited for the login semaphore")
	}
	if recorder.Code < 400 || recorder.Code >= 500 {
		t.Fatalf("status = %d, want 4xx", recorder.Code)
	}
	select {
	case <-loginSessions:
	default:
		t.Fatal("invalid body acquired the login semaphore")
	}
	select {
	case loginSessions <- nil:
		<-loginSessions
	default:
		t.Fatal("login semaphore is not free")
	}
}

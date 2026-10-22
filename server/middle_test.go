package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAuthenticationStopsUnauthorizedHandler(t *testing.T) {
	called := false
	router := gin.New()
	router.GET("/api/ctrl/kick_session", Authentication([]string{"secret"}, nil), func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/ctrl/kick_session", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if called {
		t.Fatal("unauthorized request reached the handler")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestAuthenticationAllowsMatchingToken(t *testing.T) {
	called := false
	router := gin.New()
	router.GET("/api/ctrl/kick_session", Authentication([]string{"secret"}, nil), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/ctrl/kick_session?token=secret", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if !called {
		t.Fatal("authorized request did not reach the handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

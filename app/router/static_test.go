package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestSecureAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{"main.js": {Data: []byte("ok")}}

	t.Run("serves asset without caching", func(t *testing.T) {
		router := gin.New()
		registerSecureAssets(router, assets)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/secure/assets/main.js", nil)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Body.String() != "ok" {
			t.Fatalf("unexpected asset response: status=%d cache=%q body=%q", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
	})

	t.Run("missing asset returns 404 without caching", func(t *testing.T) {
		router := gin.New()
		registerSecureAssets(router, assets)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/secure/assets/missing.js", nil)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusNotFound || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected missing asset response: status=%d cache=%q", response.Code, response.Header().Get("Cache-Control"))
		}
	})
}

func TestNoRouteReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(noRoute())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", response.Code)
	}
}

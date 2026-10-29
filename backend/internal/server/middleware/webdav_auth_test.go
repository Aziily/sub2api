package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestWebDAVCredentialsMustMatchKeyOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(WebDAVBasicCredentials())
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer valid-key" || c.GetHeader("x-api-key") != "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(ContextKeyAPIKey), &service.APIKey{User: &service.User{Email: "owner@example.com"}})
		c.Next()
	})
	router.Use(WebDAVMatchUser())
	router.GET("/dav", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, tc := range []struct {
		username, password string
		want               int
	}{
		{"owner@example.com", "valid-key", http.StatusNoContent},
		{"other@example.com", "valid-key", http.StatusUnauthorized},
		{"owner@example.com", "wrong-key", http.StatusUnauthorized},
		{"", "", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodGet, "/dav", nil)
		if tc.username != "" {
			req.SetBasicAuth(tc.username, tc.password)
		}
		req.Header.Set("x-api-key", "other-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("username %q: got %d, want %d", tc.username, w.Code, tc.want)
		}
	}
}

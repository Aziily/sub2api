package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const webDAVUsernameKey = "webdav_username"

// WebDAVBasicCredentials adapts Basic credentials to the existing API-key
// middleware. It must be followed by that middleware and WebDAVMatchUser.
func WebDAVBasicCredentials() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, password, ok := c.Request.BasicAuth()
		if !ok || username == "" || password == "" {
			c.Header("WWW-Authenticate", `Basic realm="Sub2API WebDAV", charset="UTF-8"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(webDAVUsernameKey, username)
		// Do not allow alternate credential headers to change the identity.
		c.Request.Header.Del("x-api-key")
		c.Request.Header.Del("x-goog-api-key")
		c.Request.Header.Set("Authorization", "Bearer "+password)
		c.Next()
	}
}

// WebDAVMatchUser verifies that the Basic username owns the authenticated key.
func WebDAVMatchUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.GetString(webDAVUsernameKey)
		key, ok := GetAPIKeyFromContext(c)
		if !ok || !driveKeyUsable(c) || !strings.EqualFold(username, key.User.Email) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

// driveKeyUsable re-checks key state. Simple mode's gateway middleware skips
// billing checks, including expiry, but Drive access must still honor them.
func driveKeyUsable(c *gin.Context) bool {
	key, ok := GetAPIKeyFromContext(c)
	return ok && key != nil && key.User != nil && key.IsActive() && !key.IsExpired() && !key.IsQuotaExhausted()
}

// DriveAPIKeyUsable guards API-key Drive endpoints other than WebDAV.
func DriveAPIKeyUsable() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !driveKeyUsable(c) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

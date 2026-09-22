package handler

import (
	"net/http"
	"strings"

	"be_homework_center/backend/internal/oidc"

	"github.com/gin-gonic/gin"
)

// claimsKey 是 gin.Context 里存 OIDC 声明的键。
const claimsKey = "oidc_claims"

// OIDCAuth 要求 Authorization: Bearer <GitHub Actions OIDC 令牌>；
// 验签通过后把声明存入 context，供 Report 做载荷绑定校验。
// 注意拒绝路径必须 Abort——gin 中间件仅 return 不会阻止后续 handler 执行。
func OIDCAuth(v *oidc.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		const prefix = "Bearer "
		raw := c.GetHeader("Authorization")
		if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "invalid_token", "detail": "缺少 Authorization: Bearer 令牌"})
			return
		}
		cl, err := v.Verify(strings.TrimSpace(raw[len(prefix):]))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "invalid_token", "detail": err.Error()})
			return
		}
		c.Set(claimsKey, cl)
		c.Next()
	}
}

// CORS 公开只读看板 + 学生上报：放开跨域，方便前端分离部署调试。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

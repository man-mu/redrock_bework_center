// Package handler 是 api 层：请求绑定、响应与状态码映射；
// 业务规则一律在 service 包，本包不做 SQL、不做业务判断。
package handler

import (
	"errors"
	"log"
	"net/http"

	"be_homework_center/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func badRequest(c *gin.Context, detail string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_payload", "detail": detail})
}

func dbError(c *gin.Context, err error) {
	log.Printf("[api] %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
}

// respondError 把 service 的错误语义翻译成 HTTP 状态码（§3.2）。
func respondError(c *gin.Context, err error) {
	var ve *service.ValidationError
	if errors.As(err, &ve) {
		badRequest(c, ve.Detail)
		return
	}
	var be *service.BindError
	if errors.As(err, &be) {
		c.JSON(http.StatusForbidden, gin.H{"error": be.Field + "_mismatch", "detail": be.Detail})
		return
	}
	var ue *service.UnknownLessonError
	if errors.As(err, &ue) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "unknown_lesson", "lesson": ue.Lesson})
		return
	}
	dbError(c, err)
}

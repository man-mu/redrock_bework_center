package handler

import (
	"net/http"

	"be_homework_center/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// Overview 处理 GET /api/v1/overview（每课提交学生数 + 题目全集）。
func Overview(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Overview(c.Request.Context())
		if err != nil {
			dbError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// Leaderboard 处理 GET /api/v1/leaderboard?lesson=&q=（按完成题数排行）。
func Leaderboard(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Leaderboard(c.Request.Context(), c.Query("lesson"), c.Query("q"))
		if err != nil {
			dbError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

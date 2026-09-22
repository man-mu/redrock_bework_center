package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"be_homework_center/backend/internal/oidc"
	"be_homework_center/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// reportBody 对应 §3.1 的请求载荷；仅做 JSON 绑定，校验在 service。
type reportBody struct {
	RepoURL string `json:"repo_url"`
	Commit  string `json:"commit"`
	Ref     string `json:"ref"`
	Event   string `json:"event"`
	Config  struct {
		Name   string `json:"name"`
		Lesson string `json:"lesson"`
	} `json:"config"`
	Result struct {
		Lesson string              `json:"lesson"`
		Tests  []service.TestState `json:"tests"`
	} `json:"result"`
}

// Report 处理 POST /api/v1/reports。
func Report(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil || len(raw) == 0 {
			badRequest(c, "JSON 无法解析")
			return
		}
		var body reportBody
		if err := json.Unmarshal(raw, &body); err != nil {
			badRequest(c, "JSON 无法解析")
			return
		}

		// 已验签的令牌声明透传给 service 做绑定校验；未启用鉴权时为 nil。
		var ident *service.Identity
		if v, ok := c.Get(claimsKey); ok {
			if cl, ok := v.(oidc.Claims); ok {
				ident = &service.Identity{Repository: cl.Repository, SHA: cl.SHA}
			}
		}

		res, err := svc.Report(c.Request.Context(), &service.ReportInput{
			RepoURL:  body.RepoURL,
			Commit:   body.Commit,
			Ref:      body.Ref,
			Event:    body.Event,
			Name:     body.Config.Name,
			Lesson:   body.Result.Lesson,
			Tests:    body.Result.Tests,
			Payload:  string(raw),
			Identity: ident,
		})
		if err != nil {
			respondError(c, err)
			return
		}
		if res.Deduplicated {
			c.JSON(http.StatusOK, gin.H{"deduplicated": true})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "completed": res.Completed})
	}
}

// Package service 承载看板业务逻辑：上报写入（§4）、看板查询（§7）、
// 模板仓库同步（§2）。本包不感知 HTTP / gin，输入输出都是 Go 值；
// 错误语义用类型区分，由 handler 层翻译成状态码。
package service

import (
	"database/sql"
)

// UnknownLessonError 表示 result.lesson 不在 lessons 表里（§3.2 → HTTP 422）。
type UnknownLessonError struct {
	Lesson string
}

func (e *UnknownLessonError) Error() string { return "unknown_lesson: " + e.Lesson }

// ValidationError 表示载荷未通过业务校验（§3.2 → HTTP 400 invalid_payload）。
type ValidationError struct {
	Detail string
}

func (e *ValidationError) Error() string { return e.Detail }

// BindError 表示已验证的令牌身份与载荷不一致（→ HTTP 403 <field>_mismatch）。
type BindError struct {
	Field  string // repository | commit
	Detail string
}

func (e *BindError) Error() string { return e.Detail }

// Identity 是已通过验签的 OIDC 令牌声明；nil 表示本站未启用上报鉴权。
type Identity struct {
	Repository string // owner/name
	SHA        string
}

// Service 聚合所有请求作用域的业务操作，由 main 装配一次。
type Service struct {
	DB *sql.DB
}

// TestState 是单题结论（§3.1 result.tests[]）。
type TestState struct {
	Test   string
	Status string // pass | fail
}

// ReportInput 是 handler 解析后的上报载荷（§3.1）。
type ReportInput struct {
	RepoURL string
	Commit  string
	Ref     string
	Event   string
	Name    string      // config.name，可为空
	Lesson  string      // result.lesson（slug）
	Tests   []TestState // nil 表示字段缺失（区别于空数组）
	Payload string      // 原始 JSON，留档

	Identity *Identity // OIDC 令牌声明；nil 表示未启用鉴权
}

// ReportResult 是上报处理结果。
type ReportResult struct {
	Deduplicated bool // 同 (student, commit) 已收过，未重复计数（§3.2）
	Completed    int  // 本次通过题数
}
